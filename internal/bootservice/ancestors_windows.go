//go:build windows

package bootservice

import (
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

// Ancestors may allow creating unrelated children (ProgramData does), but may
// never let ordinary users replace/delete protected descendants or alter ACLs.
func trustedAncestor(h windows.Handle) error {
	sd, e := windows.GetSecurityInfo(h, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if e != nil {
		return e
	}
	owner, _, e := sd.Owner()
	if e != nil {
		return e
	}
	const installer = "S-1-5-80-956008885-3418522649-1831038044-1853292631-2271478464"
	if !owner.IsWellKnown(windows.WinLocalSystemSid) && !owner.IsWellKnown(windows.WinBuiltinAdministratorsSid) && owner.String() != installer {
		return errors.New("untrusted owner of service path ancestor")
	}
	acl, _, e := sd.DACL()
	if e != nil || acl == nil {
		return errors.New("unrestricted service ancestor")
	}
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var a *windows.ACCESS_ALLOWED_ACE
		if e = windows.GetAce(acl, i, &a); e != nil {
			return e
		}
		if a.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || a.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		s := (*windows.SID)(unsafe.Pointer(&a.SidStart))
		if s.IsWellKnown(windows.WinLocalSystemSid) || s.IsWellKnown(windows.WinBuiltinAdministratorsSid) || s.String() == installer {
			continue
		}
		if a.Mask&(windows.GENERIC_ALL|windows.GENERIC_WRITE|windows.WRITE_DAC|windows.WRITE_OWNER|windows.DELETE|0x40) != 0 {
			return errors.New("service ancestor permits untrusted replacement")
		}
	}
	return nil
}
