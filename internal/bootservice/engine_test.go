package bootservice

import (
	"context"
	"errors"
	"github.com/Palvef/AuthingNYIST/internal/app"
	"strings"
	"testing"
	"time"
)

func TestSchedulerRetryPauseAndExplicitConnect(t *testing.T) {
	calls := 0
	e := newEngine(app.DefaultConfig())
	e.ops = authOps{connect: func(context.Context, app.Config) (app.Status, error) {
		calls++
		return app.Status{}, errors.New("network unavailable")
	}, check: func(context.Context, app.Config) (app.Status, error) { return app.Status{Online: true}, nil }, disconnect: func(context.Context, app.Config) error { return nil }}
	ctx := context.Background()
	for i := 0; i < 50; i++ {
		d := e.tick(ctx)
		if d > 5*time.Minute {
			t.Fatal(d)
		}
	}
	if calls != 50 {
		t.Fatal(calls)
	}
	e.execute(ctx, "disconnect")
	e.tick(ctx)
	if calls != 50 {
		t.Fatal("disconnect did not pause")
	}
	e.execute(ctx, "connect")
	if calls != 51 {
		t.Fatal("explicit connect did not resume")
	}
	e.ops.connect = func(context.Context, app.Config) (app.Status, error) {
		calls++
		return app.Status{}, app.ErrCredentials
	}
	e.tick(ctx)
	n := calls
	e.tick(ctx)
	if calls != n {
		t.Fatal("credential rejection retried")
	}
	r := e.execute(ctx, "status")
	if !r.Status.Online {
		t.Fatal("status must query live portal")
	}
}
func TestOtherAccountStops(t *testing.T) {
	e := newEngine(app.DefaultConfig())
	e.ops.connect = func(context.Context, app.Config) (app.Status, error) { return app.Status{}, app.ErrOtherAccount }
	e.tick(context.Background())
	if !e.paused {
		t.Fatal("other account must pause")
	}
}
func TestBoundedStrictJSON(t *testing.T) {
	for _, s := range []string{`{"action":"status","config":{}}`, `{"action":"status"} {}`, strings.Repeat("a", maxMessage+1)} {
		var c command
		if decode(strings.NewReader(s), &c) == nil {
			t.Fatalf("accepted invalid input %q", s[:min(len(s), 80)])
		}
	}
	var c command
	if err := decode(strings.NewReader(`{"action":"status"}`), &c); err != nil {
		t.Fatal(err)
	}
}
