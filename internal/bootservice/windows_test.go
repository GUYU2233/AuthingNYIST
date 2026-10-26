//go:build windows

package bootservice

import (
	"bytes"
	"errors"
	"github.com/Palvef/AuthingNYIST/internal/app"
	"golang.org/x/sys/windows/svc"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineDPAPIStaging(t *testing.T) {
	sid, e := userSID()
	if e != nil {
		t.Fatal(e)
	}
	cfg := app.DefaultConfig()
	cfg.Username = "test-user"
	cfg.Password = "secret-for-test"
	v := envelope{Owner: sid, Config: cfg}
	b, e := encodeEnvelope(v)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(b, []byte(cfg.Password)) {
		t.Fatal("plaintext password")
	}
	got, e := decodeEnvelope(b)
	if e != nil || got != v {
		t.Fatalf("roundtrip: %v", e)
	}
	path, e := staging(v)
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(filepath.Dir(path))
	got, e = loadStaging(path)
	if e != nil || got != v {
		t.Fatalf("staging: %v", e)
	}
	b[0] ^= 255
	if _, e = decodeEnvelope(b); e == nil {
		t.Fatal("accepted corrupt DPAPI")
	}
}
func TestFrameLimits(t *testing.T) {
	var c command
	if e := readFrame(strings.NewReader("{\"action\":\"status\"}\n"), &c); e != nil {
		t.Fatal(e)
	}
	if e := readFrame(strings.NewReader(strings.Repeat("x", maxMessage+2)+"\n"), &c); e == nil {
		t.Fatal("unbounded frame")
	}
}

type mockController struct {
	state         svc.State
	fail          error
	starts, stops int
}

func (m *mockController) Query() (svc.Status, error) { return svc.Status{State: m.state}, m.fail }
func (m *mockController) Control(svc.Cmd) (svc.Status, error) {
	m.stops++
	m.state = svc.Stopped
	return svc.Status{State: m.state}, m.fail
}
func (m *mockController) Start(...string) error { m.starts++; m.state = svc.Running; return m.fail }
func TestMockController(t *testing.T) {
	m := &mockController{state: svc.Running}
	if e := stop(m); e != nil {
		t.Fatal(e)
	}
	if m.stops != 1 {
		t.Fatal(m.stops)
	}
	if e := stop(m); e != nil || m.stops != 1 {
		t.Fatal("stop idempotence")
	}
	if e := start(m); e != nil || m.starts != 1 {
		t.Fatal("start")
	}
	m.fail = errors.New("injected")
	if stop(m) == nil {
		t.Fatal("lost controller error")
	}
}
func TestLease(t *testing.T) {
	// One private event per test, never touch the running user's service lease.
	name := `Local\AuthingNYIST.TestLease.` + strings.ReplaceAll(t.TempDir(), `\`, "_")
	acquire := func() (func(), error) { return acquireAuthenticationNamed(name) }
	release, e := acquire()
	if e != nil {
		t.Fatal(e)
	}
	if r, e := acquire(); e == nil {
		r()
		release()
		t.Fatal("duplicate lease")
	}
	release()
	release, e = acquire()
	if e != nil {
		t.Fatal(e)
	}
	release()
}
