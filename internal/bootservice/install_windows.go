//go:build windows

package bootservice

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

type controller interface {
	Query() (svc.Status, error)
	Control(svc.Cmd) (svc.Status, error)
	Start(...string) error
}

func waitState(s controller, want svc.State) error {
	deadline := time.Now().Add(40 * time.Second)
	for {
		state, e := s.Query()
		if e != nil {
			return e
		}
		if state.State == want {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for service state")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
func stop(s controller) error {
	st, e := s.Query()
	if e != nil {
		return e
	}
	if st.State == svc.Stopped {
		return nil
	}
	if st.State != svc.StopPending {
		if _, e = s.Control(svc.Stop); e != nil {
			return e
		}
	}
	return waitState(s, svc.Stopped)
}
func start(s controller) error {
	if e := s.Start(); e != nil {
		return e
	}
	return waitState(s, svc.Running)
}
func move(from, to string) error {
	a, e := windows.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	b, e := windows.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	return windows.MoveFileEx(a, b, windows.MOVEFILE_WRITE_THROUGH)
}

// transaction retains original files until the new service has reached Running.
// A failed rollback is surfaced, never silently replaced with an install success.
type fileTransaction struct {
	paths     []string
	existed   map[string]bool
	installed map[string]bool
}

func newTransaction(paths ...string) *fileTransaction {
	return &fileTransaction{paths: paths, existed: map[string]bool{}, installed: map[string]bool{}}
}
func (t *fileTransaction) backup() error {
	for _, p := range t.paths {
		_, e := os.Lstat(p + ".previous")
		if e == nil {
			return errors.New("previous recovery files exist; administrator recovery required")
		}
		if !os.IsNotExist(e) {
			return e
		}
		f, e := openLocked(p, windows.READ_CONTROL, windows.OPEN_EXISTING, "")
		if errors.Is(e, windows.ERROR_FILE_NOT_FOUND) {
			continue
		}
		if e != nil {
			return e
		}
		e = trustedOwner(windows.Handle(f.Fd()))
		f.Close()
		if e != nil {
			return e
		}
		if e = move(p, p+".previous"); e != nil {
			return e
		}
		t.existed[p] = true
	}
	return nil
}
func (t *fileTransaction) rollback() error {
	var errs []error
	for _, p := range t.paths {
		if t.installed[p] {
			if e := os.Remove(p); e != nil && !os.IsNotExist(e) {
				errs = append(errs, e)
			}
		}
		if t.existed[p] {
			if e := move(p+".previous", p); e != nil {
				errs = append(errs, e)
			}
		}
	}
	return errors.Join(errs...)
}
func (t *fileTransaction) commit() {
	for _, p := range t.paths {
		if t.existed[p] {
			os.Remove(p + ".previous")
		}
	}
}
func copyExecutable(to string) error {
	src, e := os.Executable()
	if e != nil {
		return e
	}
	f, e := openLocked(src, windows.GENERIC_READ, windows.OPEN_EXISTING, "")
	if e != nil {
		return e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() < 2 || info.Size() > 256*1024*1024 {
		return errors.New("invalid source executable")
	}
	out, e := openLocked(to, windows.GENERIC_WRITE, windows.CREATE_NEW, privateACL)
	if e != nil {
		return e
	}
	n, e := io.Copy(out, io.LimitReader(f, 256*1024*1024+1))
	if e == nil && n != info.Size() {
		e = errors.New("executable changed during copy")
	}
	if e == nil {
		e = out.Sync()
	}
	ce := out.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		os.Remove(to)
	}
	return e
}
func install(v envelope) (err error) {
	release, e := acquireManagement()
	if e != nil {
		return e
	}
	defer release()
	exe, cfg, e := paths()
	if e != nil {
		return e
	}
	held, e := lockPrivateDir(filepath.Dir(exe))
	if e != nil {
		return e
	}
	defer closeFiles(held)
	held2, e := lockPrivateDir(filepath.Dir(cfg))
	if e != nil {
		return e
	}
	defer closeFiles(held2)
	b, e := encodeEnvelope(v)
	if e != nil {
		return e
	}
	m, e := mgr.Connect()
	if e != nil {
		return e
	}
	defer m.Disconnect()
	s, e := m.OpenService(ServiceName)
	existing := e == nil
	defer func() {
		if s != nil {
			s.Close()
		}
	}()
	var old mgr.Config
	var wasRunning bool
	if e != nil && !errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return e
	}
	if existing {
		old, e = s.Config()
		if e != nil {
			return e
		}
		if !strings.EqualFold(old.BinaryPathName, windows.EscapeArg(exe)+" --boot-service") || (old.ServiceStartName != "LocalSystem" && old.ServiceStartName != "NT AUTHORITY\\SYSTEM") {
			return errors.New("refusing to replace an unexpected service configuration")
		}
		st, e := s.Query()
		if e != nil {
			return e
		}
		wasRunning = st.State != svc.Stopped
		if e = stop(s); e != nil {
			return e
		}
	}
	t := newTransaction(exe, cfg)
	created := false
	defer func() {
		if err == nil {
			t.commit()
			return
		}
		if s != nil {
			_ = stop(s)
		}
		var restore []error
		restore = append(restore, t.rollback())
		if created {
			restore = append(restore, s.Delete())
		} else if existing {
			restore = append(restore, s.UpdateConfig(old))
			if wasRunning {
				restore = append(restore, start(s))
			}
		}
		if r := errors.Join(restore...); r != nil {
			err = errors.Join(err, fmt.Errorf("rollback incomplete; recovery files retained: %w", r))
		}
	}()
	if e = t.backup(); e != nil {
		return e
	}
	if e = copyExecutable(exe); e != nil {
		return e
	}
	t.installed[exe] = true
	if e = createPrivate(cfg, b); e != nil {
		return e
	}
	t.installed[cfg] = true
	sc := mgr.Config{ServiceType: windows.SERVICE_WIN32_OWN_PROCESS, StartType: mgr.StartAutomatic, ErrorControl: mgr.ErrorNormal, BinaryPathName: windows.EscapeArg(exe) + " --boot-service", ServiceStartName: "LocalSystem", DisplayName: "AuthingNYIST boot authentication", Description: "Campus authentication before Windows sign-in", DelayedAutoStart: false}
	if existing {
		e = s.UpdateConfig(sc)
	} else {
		s, e = m.CreateService(ServiceName, exe, sc, "--boot-service")
		if e == nil {
			created = true
		}
	}
	if e != nil {
		return e
	}
	return start(s)
}
func remove() error {
	release, e := acquireManagement()
	if e != nil {
		return e
	}
	defer release()
	exe, cfg, e := paths()
	if e != nil {
		return e
	}
	m, e := mgr.Connect()
	if e != nil {
		return e
	}
	defer m.Disconnect()
	s, e := m.OpenService(ServiceName)
	if errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return nil
	}
	if e != nil {
		return e
	}
	defer s.Close()
	if e = stop(s); e != nil {
		return e
	}
	if e = s.Delete(); e != nil {
		return e
	} // Keep private files as recoverable data; they cannot execute after removal.
	_ = exe
	_ = cfg
	return nil
}
