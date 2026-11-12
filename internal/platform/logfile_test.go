package platform

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenDaemonLogBelowLimitDoesNotRotate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	if err := os.WriteFile(path, []byte("existing line\n"), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openDaemonLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("next line\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatalf("unexpected rotation: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "existing line\nnext line\n" {
		t.Fatalf("append changed content: %q", b)
	}
}

func TestOpenDaemonLogAboveLimitRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	old := strings.Repeat("x", int(daemonLogMaxSize)+1)
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openDaemonLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Size() != int64(len(old)) {
		t.Fatalf("rotated size = %d, want %d", rotated.Size(), len(old))
	}
	if rotated.Mode().Perm() != 0600 {
		t.Fatalf("rotated mode = %04o, want 0600", rotated.Mode().Perm())
	}
	fresh, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Size() != 0 {
		t.Fatalf("fresh log size = %d, want 0", fresh.Size())
	}
	if fresh.Mode().Perm() != 0600 {
		t.Fatalf("fresh log mode = %04o, want 0600", fresh.Mode().Perm())
	}
}

func TestOpenDaemonLogReplacesOlderRotatedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	if err := os.WriteFile(path+".1", []byte("older rotation"), 0600); err != nil {
		t.Fatal(err)
	}
	current := strings.Repeat("y", int(daemonLogMaxSize)+1)
	if err := os.WriteFile(path, []byte(current), 0600); err != nil {
		t.Fatal(err)
	}
	f, err := openDaemonLog(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	b, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != current {
		t.Fatalf(".1 was not replaced (len=%d)", len(b))
	}
}

func TestOpenDaemonLogEnforcesMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	if err := os.WriteFile(path, []byte("line\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	f, err := openDaemonLog(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("mode = %04o, want 0600", info.Mode().Perm())
	}
}

func TestOpenDaemonLogTwiceKeepsAppending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	for i := 0; i < 2; i++ {
		f, err := openDaemonLog(path)
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		if _, err := f.WriteString("start\n"); err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("second start should not rotate a small log")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "start\nstart\n" {
		t.Fatalf("content = %q", b)
	}
}

func TestOpenDaemonLogRejectsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation requires privileges on Windows")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "target.log")
	if err := os.WriteFile(target, []byte("do not touch"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "nyist-daemon.log")
	if err := os.Symlink(target, path); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if _, err := openDaemonLog(path); err == nil {
		t.Fatal("accepted a symlinked log")
	}
	b, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "do not touch" {
		t.Fatalf("symlink target was modified: %q", b)
	}
}

func TestRotateLogFileBelowLimitIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	if err := os.WriteFile(path, []byte("short\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := rotateLogFile(path, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".1"); !os.IsNotExist(err) {
		t.Fatal("small log was rotated")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "short\n" {
		t.Fatalf("content changed: %q", b)
	}
}

func TestRotateLogFileAboveLimitReplacesRotated(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	if err := os.WriteFile(path+".1", []byte("older"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("0123456789"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := rotateLogFile(path, 9); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("live file should have been renamed")
	}
	b, err := os.ReadFile(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "0123456789" {
		t.Fatalf(".1 = %q, want new rotation", b)
	}
}

func TestOpenDaemonLogConcurrentStartsRotateOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nyist-daemon.log")
	old := strings.Repeat("z", int(daemonLogMaxSize)+1)
	if err := os.WriteFile(path, []byte(old), 0600); err != nil {
		t.Fatal(err)
	}
	const starts = 4
	errs := make(chan error, starts)
	for i := 0; i < starts; i++ {
		go func() {
			f, err := openDaemonLog(path)
			if err == nil {
				err = f.Close()
			}
			errs <- err
		}()
	}
	for i := 0; i < starts; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	rotated, err := os.Stat(path + ".1")
	if err != nil {
		t.Fatal(err)
	}
	if rotated.Size() != int64(len(old)) {
		t.Fatalf("rotated size = %d, want the original %d bytes", rotated.Size(), len(old))
	}
}

func TestRotateLogFileRejectsBadLimit(t *testing.T) {
	if err := rotateLogFile(filepath.Join(t.TempDir(), "x.log"), 0); err == nil {
		t.Fatal("accepted non-positive limit")
	}
}
