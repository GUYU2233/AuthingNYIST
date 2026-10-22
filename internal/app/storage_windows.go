//go:build windows
// +build windows

package app

import (
	"encoding/base64"
	"errors"
	"golang.org/x/sys/windows"
	"strings"
	"unsafe"
)

const protectedPrefix = "dpapi:"

func protectPassword(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	b := []byte(s)
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	if e := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); e != nil {
		return "", errors.New("cannot protect password with Windows DPAPI")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return protectedPrefix + base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, int(out.Size))), nil
}
func unprotectPassword(s string) (string, error) {
	if s == "" {
		return "", nil
	}
	if !strings.HasPrefix(s, protectedPrefix) {
		return "", errors.New("unprotected Windows password rejected; enter it again")
	}
	b, e := base64.StdEncoding.DecodeString(strings.TrimPrefix(s, protectedPrefix))
	if e != nil || len(b) == 0 {
		return "", errors.New("invalid protected password")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	if e = windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); e != nil {
		return "", errors.New("cannot decrypt password for this Windows user")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, int(out.Size))), nil
}
func secureDirectory(string) error { return nil }
func replaceFile(from, to string) error {
	f, e := windows.UTF16PtrFromString(from)
	if e != nil {
		return e
	}
	t, e := windows.UTF16PtrFromString(to)
	if e != nil {
		return e
	}
	return windows.MoveFileEx(f, t, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}
