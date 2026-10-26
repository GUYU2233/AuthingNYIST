//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacOperationLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	release, err := macAcquireLock(path)
	if err != nil {
		t.Fatal(err)
	}
	if r, err := macAcquireLock(path); err == nil {
		r()
		release()
		t.Fatal("parallel operation admitted")
	}
	info, err := os.Stat(path + ".operation.lock")
	if err != nil || info.Mode().Perm() != 0600 {
		release()
		t.Fatal("lock permissions", err)
	}
	release()
	r, err := macAcquireLock(path)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	r()
}
