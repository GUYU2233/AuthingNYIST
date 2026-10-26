//go:build windows

package bootservice

import (
	"os"
	"path/filepath"
	"testing"
)

// Only temporary files are touched; no SCM, registry, or Program Files access.
func TestFileRollbackRestoresPrevious(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "service.json")
	if e := os.WriteFile(p+".previous", []byte("old encrypted config"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := os.WriteFile(p, []byte("new encrypted config"), 0600); e != nil {
		t.Fatal(e)
	}
	tx := newTransaction(p)
	tx.existed[p] = true
	tx.installed[p] = true
	if e := tx.rollback(); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil || string(b) != "old encrypted config" {
		t.Fatalf("old config lost: %q %v", b, e)
	}
}
func TestCreateNeverOverwrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "file")
	if e := os.WriteFile(p, []byte("original"), 0600); e != nil {
		t.Fatal(e)
	}
	if e := createPrivate(p, []byte("replace")); e == nil {
		t.Fatal("overwrote existing file")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "original" {
		t.Fatal("existing data changed")
	}
}
