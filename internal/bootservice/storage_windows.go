//go:build windows

package bootservice

import (
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"
)

const privateACL = "D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)"

func userSID() (string, error) {
	u, e := windows.GetCurrentProcessToken().GetTokenUser()
	if e != nil {
		return "", e
	}
	return u.User.Sid.String(), nil
}
func validOwner(s string) error {
	sid, e := windows.StringToSid(s)
	if e != nil || sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return errors.New("invalid service owner SID")
	}
	return nil
}
func machineProtect(b []byte, decrypt bool) ([]byte, error) {
	if len(b) == 0 || len(b) > maxMessage {
		return nil, errors.New("invalid protected configuration size")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	var e error
	if decrypt {
		e = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	} else {
		e = windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN|windows.CRYPTPROTECT_LOCAL_MACHINE, &out)
	}
	if e != nil {
		return nil, errors.New("machine DPAPI operation failed")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	if out.Size > maxMessage {
		return nil, errors.New("protected configuration too large")
	}
	return append([]byte(nil), unsafe.Slice(out.Data, int(out.Size))...), nil
}
func encodeEnvelope(v envelope) ([]byte, error) {
	if e := validateEnvelope(v); e != nil {
		return nil, e
	}
	b, e := json.Marshal(v)
	if e != nil {
		return nil, e
	}
	return machineProtect(b, false)
}
func decodeEnvelope(b []byte) (envelope, error) {
	var v envelope
	p, e := machineProtect(b, true)
	if e != nil {
		return v, e
	}
	if e = decode(bytesReader(p), &v); e != nil {
		return v, e
	}
	return v, validateEnvelope(v)
}
func validateEnvelope(v envelope) error {
	if e := validOwner(v.Owner); e != nil {
		return e
	}
	if e := v.Config.Validate(); e != nil {
		return e
	}
	if v.Config.Username == "" || v.Config.Password == "" {
		return errors.New("service requires credentials")
	}
	return nil
}
func security(s string) (*windows.SecurityAttributes, error) {
	sd, e := windows.SecurityDescriptorFromString(s)
	if e != nil {
		return nil, e
	}
	return &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}, nil
}
func paths() (exe, config string, err error) {
	p, e := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, 0)
	if e != nil {
		return "", "", e
	}
	d, e := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if e != nil {
		return "", "", e
	}
	return filepath.Join(p, "AuthingNYIST", "nyist-service.exe"), filepath.Join(d, "AuthingNYIST", "service.json"), nil
}

// openLocked rejects reparse points and keeps the object non-renamable for the
// lifetime of the handle. Writers use CREATE_NEW, never truncate a link target.
func openLocked(path string, access, creation uint32, acl string) (*os.File, error) {
	p, e := windows.UTF16PtrFromString(path)
	if e != nil {
		return nil, e
	}
	var sa *windows.SecurityAttributes
	if acl != "" {
		sa, e = security(acl)
		if e != nil {
			return nil, e
		}
	}
	h, e := windows.CreateFile(p, access, windows.FILE_SHARE_READ, sa, creation, windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if e != nil {
		return nil, e
	}
	var info windows.ByHandleFileInformation
	if e = windows.GetFileInformationByHandle(h, &info); e != nil || info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		windows.CloseHandle(h)
		return nil, errors.New("reparse point rejected")
	}
	return os.NewFile(uintptr(h), path), nil
}
func trustedOwner(h windows.Handle) error {
	sd, e := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	sid, _, e := sd.Owner()
	if e != nil {
		return e
	}
	if !sid.IsWellKnown(windows.WinLocalSystemSid) && !sid.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
		return errors.New("service path is not owned by SYSTEM or Administrators")
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil {
		return errors.New("unrestricted service path ACL")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var a *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &a); e != nil {
			return e
		}
		if a.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		s := (*windows.SID)(unsafe.Pointer(&a.SidStart))
		if s.IsWellKnown(windows.WinLocalSystemSid) || s.IsWellKnown(windows.WinBuiltinAdministratorsSid) {
			continue
		}
		if a.Mask != 0 {
			return errors.New("service path permits untrusted writes")
		}
	}
	return nil
}

// lockPrivateDir locks each ancestor, rejecting junctions. Private leaves are
// created atomically with a protected DACL; existing leaves must already be safe.
func lockPrivateDir(dir string) ([]*os.File, error) {
	var held []*os.File
	closeAll := func() {
		for _, f := range held {
			f.Close()
		}
	}
	root := filepath.VolumeName(dir) + `\`
	rel, e := filepath.Rel(root, dir)
	if e != nil {
		return nil, e
	}
	rootHandle, e := openLocked(root, windows.READ_CONTROL, windows.OPEN_EXISTING, "")
	if e != nil {
		return nil, e
	}
	held = append(held, rootHandle)
	if e = trustedAncestor(windows.Handle(rootHandle.Fd())); e != nil {
		closeAll()
		return nil, e
	}
	cur := root
	parts := strings.Split(rel, `\`)
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		if i == len(parts)-1 {
			p, _ := windows.UTF16PtrFromString(cur)
			sa, _ := security(privateACL)
			e = windows.CreateDirectory(p, sa)
			if e != nil && !errors.Is(e, windows.ERROR_ALREADY_EXISTS) {
				closeAll()
				return nil, e
			}
		}
		f, e := openLocked(cur, windows.READ_CONTROL, windows.OPEN_EXISTING, "")
		if e != nil {
			closeAll()
			return nil, e
		}
		held = append(held, f)
		if i == len(parts)-1 {
			e = trustedOwner(windows.Handle(f.Fd()))
		} else {
			e = trustedAncestor(windows.Handle(f.Fd()))
		}
		if e != nil {
			closeAll()
			return nil, e
		}
	}
	return held, nil
}
func closeFiles(fs []*os.File) {
	for _, f := range fs {
		f.Close()
	}
}
func readLocked(path string, private bool) ([]byte, error) {
	f, e := openLocked(path, windows.GENERIC_READ|windows.READ_CONTROL, windows.OPEN_EXISTING, "")
	if e != nil {
		return nil, e
	}
	defer f.Close()
	if private {
		if e = trustedOwner(windows.Handle(f.Fd())); e != nil {
			return nil, e
		}
	}
	s, e := f.Stat()
	if e != nil || !s.Mode().IsRegular() || s.Size() > maxMessage {
		return nil, errors.New("invalid configuration file")
	}
	return io.ReadAll(io.LimitReader(f, maxMessage+1))
}
func createPrivate(path string, b []byte) error {
	f, e := openLocked(path, windows.GENERIC_WRITE, windows.CREATE_NEW, privateACL)
	if e != nil {
		return e
	}
	_, e = f.Write(b)
	if e == nil {
		e = f.Sync()
	}
	c := f.Close()
	if e == nil {
		e = c
	}
	if e != nil {
		os.Remove(path)
	}
	return e
}
func staging(cfg envelope) (string, error) {
	b, e := encodeEnvelope(cfg)
	if e != nil {
		return "", e
	}
	d, e := os.MkdirTemp("", "AuthingNYIST-stage-")
	if e != nil {
		return "", e
	}
	sd, e := windows.SecurityDescriptorFromString("D:P(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)(A;OICI;FA;;;" + cfg.Owner + ")")
	if e != nil {
		return "", e
	}
	acl, _, _ := sd.DACL()
	p, e := windows.UTF16PtrFromString(d)
	if e != nil {
		return "", e
	}
	_ = p
	e = windows.SetNamedSecurityInfo(d, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, nil, nil, acl, nil)
	if e != nil {
		os.Remove(d)
		return "", e
	}
	path := filepath.Join(d, "envelope.bin")
	f, e := openLocked(path, windows.GENERIC_WRITE, windows.CREATE_NEW, "O:"+cfg.Owner+"D:P(A;;FA;;;SY)(A;;FA;;;BA)(A;;FA;;;"+cfg.Owner+")")
	if e == nil {
		_, e = f.Write(b)
		f.Close()
	}
	if e != nil {
		os.RemoveAll(d)
		return "", e
	}
	return path, nil
}
func loadStaging(path string) (envelope, error) {
	var v envelope
	if !filepath.IsAbs(path) || filepath.Base(path) != "envelope.bin" {
		return v, errors.New("invalid staging path")
	}
	f, e := openLocked(path, windows.GENERIC_READ|windows.READ_CONTROL, windows.OPEN_EXISTING, "")
	if e != nil {
		return v, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, maxMessage+1))
	if e != nil {
		return v, e
	}
	v, e = decodeEnvelope(b)
	if e != nil {
		return v, e
	}
	sd, e := windows.GetSecurityInfo(windows.Handle(f.Fd()), windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if e != nil {
		return v, e
	}
	sid, _, e := sd.Owner()
	if e != nil || sid.String() != v.Owner {
		return v, fmt.Errorf("staging owner does not match requesting user SID")
	}
	return v, nil
}
