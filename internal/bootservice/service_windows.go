//go:build windows

package bootservice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/Microsoft/go-winio"
	"github.com/Palvef/AuthingNYIST/internal/app"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
)

const pipeName = `\\.\pipe\AuthingNYIST.boot.v1`

func Installed() (bool, error) {
	m, e := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if e != nil {
		return false, e
	}
	defer windows.CloseServiceHandle(m)
	name, _ := windows.UTF16PtrFromString(ServiceName)
	s, e := windows.OpenService(m, name, windows.SERVICE_QUERY_STATUS)
	if errors.Is(e, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	windows.CloseServiceHandle(s)
	return true, nil
}
func Request(action string) (app.Status, error) {
	if action != "status" && action != "connect" && action != "disconnect" {
		return app.Status{}, errors.New("unsupported service action")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Second)
	defer cancel()
	c, e := winio.DialPipeContext(ctx, pipeName)
	if e != nil {
		return app.Status{}, errors.New("boot service unavailable; no local authentication was attempted")
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(45 * time.Second))
	if e = json.NewEncoder(c).Encode(command{Action: action}); e != nil {
		return app.Status{}, e
	}
	var r reply
	if e = readFrame(c, &r); e != nil {
		return app.Status{}, e
	}
	if r.Error != "" {
		return r.Status, errors.New(r.Error)
	}
	return r.Status, nil
}

// IPC uses a single bounded newline-delimited JSON object per connection.
func readFrame(r io.Reader, out any) error {
	b := make([]byte, 0, 512)
	one := make([]byte, 1)
	for len(b) <= maxMessage {
		n, e := r.Read(one)
		if n > 0 {
			if one[0] == '\n' {
				return decode(bytesReader(b), out)
			}
			b = append(b, one[0])
		}
		if e != nil {
			return e
		}
	}
	return errors.New("service message too large")
}

type handler struct{ value envelope }

func (h handler) Execute(args []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	release, leaseErr := AcquireAuthentication()
	if leaseErr != nil {
		return false, 2
	}
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, e := winio.ListenPipe(pipeName, &winio.PipeConfig{SecurityDescriptor: "D:P(A;;GA;;;SY)(A;;GA;;;BA)(A;;GA;;;" + h.value.Owner + ")", MessageMode: false, InputBufferSize: maxMessage, OutputBufferSize: maxMessage})
	if e != nil {
		return false, 1
	}
	defer l.Close()
	eng := newEngine(h.value.Config)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); eng.run(ctx) }()
	go func() { defer wg.Done(); serve(ctx, l, eng) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		r := <-requests
		switch r.Cmd {
		case svc.Interrogate:
			status <- r.CurrentStatus
		case svc.Stop, svc.Shutdown:
			status <- svc.Status{State: svc.StopPending}
			cancel()
			l.Close()
			wg.Wait()
			return false, 0
		}
	}
}
func serve(ctx context.Context, l net.Listener, e *engine) {
	slots := make(chan struct{}, 8)
	var clients sync.WaitGroup
	defer clients.Wait()
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		select {
		case slots <- struct{}{}:
			clients.Add(1)
			go func() {
				defer clients.Done()
				closed := make(chan struct{})
				defer close(closed)
				go func() {
					select {
					case <-ctx.Done():
						c.Close()
					case <-closed:
					}
				}()
				defer func() { <-slots; c.Close() }()
				c.SetDeadline(time.Now().Add(45 * time.Second))
				var cmd command
				var r reply
				if err := readFrame(c, &cmd); err != nil {
					r.Error = "invalid service request"
				} else {
					op, cancel := context.WithTimeout(ctx, 35*time.Second)
					r = e.execute(op, cmd.Action)
					cancel()
				}
				json.NewEncoder(c).Encode(r)
			}()
		default:
			c.Close()
		}
	}
}
func HandleCommandLine() (bool, error) {
	if len(os.Args) < 2 {
		return false, nil
	}
	switch os.Args[1] {
	case "--boot-service":
		if len(os.Args) != 2 {
			return true, errors.New("invalid service arguments")
		}
		is, e := svc.IsWindowsService()
		if e != nil || !is {
			return true, errors.New("service mode requires Windows Service Control Manager")
		}
		exe, cfg, e := paths()
		if e != nil {
			return true, e
		}
		actual, e := os.Executable()
		if e != nil || !strings.EqualFold(filepath.Clean(actual), exe) {
			return true, errors.New("service executable must be installed in Program Files")
		}
		held, e := lockPrivateDir(filepath.Dir(exe))
		if e != nil {
			return true, e
		}
		defer closeFiles(held)
		held2, e := lockPrivateDir(filepath.Dir(cfg))
		if e != nil {
			return true, e
		}
		defer closeFiles(held2)
		b, e := readLocked(cfg, true)
		if e != nil {
			return true, e
		}
		v, e := decodeEnvelope(b)
		if e != nil {
			return true, e
		}
		return true, svc.Run(ServiceName, handler{v})
	case "--boot-install":
		if len(os.Args) != 3 {
			return true, errors.New("invalid install arguments")
		}
		if !windows.GetCurrentProcessToken().IsElevated() {
			return true, errors.New("administrator elevation required")
		}
		v, e := loadStaging(os.Args[2])
		if e != nil {
			return true, e
		}
		return true, install(v)
	case "--boot-remove":
		if len(os.Args) != 2 {
			return true, errors.New("invalid remove arguments")
		}
		if !windows.GetCurrentProcessToken().IsElevated() {
			return true, errors.New("administrator elevation required")
		}
		return true, remove()
	default:
		return false, nil
	}
}

// ShellExecuteEx is used rather than a shell: no credentials appear in argv.
type shellExecuteInfo struct {
	Size       uint32
	Mask       uint32
	HWND       uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	Instance   uintptr
	IDList     uintptr
	Class      *uint16
	ClassKey   uintptr
	HotKey     uint32
	Icon       uintptr
	Process    windows.Handle
}

var shellExecuteEx = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

func elevate(owner uintptr, args string) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	verb, _ := windows.UTF16PtrFromString("runas")
	file, e := windows.UTF16PtrFromString(exe)
	if e != nil {
		return e
	}
	params, e := windows.UTF16PtrFromString(args)
	if e != nil {
		return e
	}
	info := shellExecuteInfo{Mask: 0x40 | 0x100, HWND: owner, Verb: verb, File: file, Parameters: params, Show: 0}
	info.Size = uint32(unsafe.Sizeof(info))
	r, _, callErr := shellExecuteEx.Call(uintptr(unsafe.Pointer(&info)))
	if r == 0 {
		if errors.Is(callErr, windows.ERROR_CANCELLED) {
			return errors.New("administrator authorization cancelled")
		}
		return fmt.Errorf("cannot start administrator helper: %w", callErr)
	}
	if info.Process == 0 {
		return errors.New("administrator helper returned no process handle")
	}
	defer windows.CloseHandle(info.Process)
	w, e := windows.WaitForSingleObject(info.Process, 180000)
	if e != nil || w != windows.WAIT_OBJECT_0 {
		return errors.New("cannot wait for administrator helper")
	}
	var code uint32
	if e = windows.GetExitCodeProcess(info.Process, &code); e != nil {
		return e
	}
	if code != 0 {
		return fmt.Errorf("administrator helper failed (exit %d); previous usable configuration was retained where possible", code)
	}
	return nil
}
func Manage(ownerHWND uintptr, enable bool, cfg app.Config) error {
	args := "--boot-remove"
	var stage string
	if enable {
		sid, e := userSID()
		if e != nil {
			return e
		}
		stage, e = staging(envelope{Owner: sid, Config: cfg})
		if e != nil {
			return e
		}
		defer os.RemoveAll(filepath.Dir(stage))
		args = "--boot-install " + windows.EscapeArg(stage)
	}
	if e := elevate(ownerHWND, args); e != nil {
		return e
	}
	ok, e := Installed()
	if e != nil {
		return e
	}
	if ok != enable {
		return errors.New("service operation did not reach requested installed state")
	}
	return nil
}
