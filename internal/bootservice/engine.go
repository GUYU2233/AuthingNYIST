// Package bootservice owns the machine-wide boot authentication service.
package bootservice

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"sync"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
)

const ServiceName = "AuthingNYIST"
const maxMessage = 64 * 1024

type envelope struct {
	Owner  string     `json:"owner"`
	Config app.Config `json:"config"`
}
type reply struct {
	Status app.Status `json:"status"`
	Error  string     `json:"error,omitempty"`
}
type command struct {
	Action string `json:"action"`
}

func decode(r io.Reader, out any) error {
	b, err := io.ReadAll(io.LimitReader(r, maxMessage+1))
	if err != nil {
		return err
	}
	if len(b) > maxMessage {
		return errors.New("service message too large")
	}
	d := json.NewDecoder(bytesReader(b))
	d.DisallowUnknownFields()
	if err = d.Decode(out); err != nil {
		return err
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return errors.New("trailing service data")
	}
	return nil
}

// A byte reader avoids allowing a decoder to consume an unbounded stream.
type byteReader struct{ b []byte }

func bytesReader(b []byte) *byteReader { return &byteReader{b: b} }
func (r *byteReader) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

type authOps struct {
	connect    func(context.Context, app.Config) (app.Status, error)
	check      func(context.Context, app.Config) (app.Status, error)
	disconnect func(context.Context, app.Config) error
}
type engine struct {
	mu       sync.Mutex
	cfg      app.Config
	ops      authOps
	paused   bool
	failures uint
	last     string
}

func newEngine(cfg app.Config) *engine {
	return &engine{cfg: cfg, ops: authOps{app.Connect, app.Check, app.Disconnect}}
}
func terminal(err error) bool {
	return errors.Is(err, app.ErrCredentials) || errors.Is(err, app.ErrOtherAccount)
}
func (e *engine) attempt(ctx context.Context) (app.Status, error) {
	s, err := e.ops.connect(ctx, e.cfg)
	if terminal(err) {
		e.paused = true
	}
	if err != nil {
		e.last = err.Error()
		if e.failures < 8 {
			e.failures++
		}
	} else {
		e.last = ""
		e.failures = 0
	}
	return s, err
}
func (e *engine) execute(ctx context.Context, action string) reply {
	e.mu.Lock()
	defer e.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return reply{Error: err.Error()}
	}
	var s app.Status
	var err error
	switch action {
	case "status":
		s, err = e.ops.check(ctx, e.cfg)
		if err == nil && e.paused {
			s.Message += "; automatic authentication paused"
		}
	case "connect":
		e.paused = false
		e.failures = 0
		s, err = e.attempt(ctx)
	case "disconnect":
		e.paused = true
		err = e.ops.disconnect(ctx, e.cfg)
		if err == nil {
			s, err = e.ops.check(ctx, e.cfg)
		}
	default:
		err = errors.New("unsupported service action")
	}
	r := reply{Status: s}
	if err != nil {
		r.Error = err.Error()
	}
	return r
}
func (e *engine) tick(ctx context.Context) time.Duration {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.paused || ctx.Err() != nil {
		return time.Second
	}
	_, _ = e.attempt(ctx)
	if e.failures == 0 {
		return 30 * time.Second
	}
	d := time.Duration(1<<e.failures) * time.Second
	if d > 5*time.Minute {
		d = 5 * time.Minute
	}
	return d
}
func (e *engine) run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		d := e.tick(ctx)
		t := time.NewTimer(d)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
