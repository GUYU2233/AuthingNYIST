//go:build linux
// +build linux

package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
)

const (
	keyringService = "nyist-connect"
	// maxKeyringOutput caps what secret-tool may write to stdout (1 MiB).
	maxKeyringOutput = 1 << 20
)

// keyringExec runs secret-tool. Tests replace it to avoid needing a live
// session bus; production uses runSecretTool. The stdin argument is never
// exposed through argv.
var keyringExec = runSecretTool

func keyringStore(id, secret string) error {
	ctx, cancel := keyringTimeoutContext()
	defer cancel()
	// Feed the secret on stdin so it never appears in the process list.
	if _, err := keyringExec(ctx, []byte(secret), "store", "--label=AuthingNYIST campus password", "service", keyringService, "account", id); err != nil {
		return errors.New("cannot store password in the system keyring")
	}
	return nil
}

func keyringLookup(id string) (string, error) {
	ctx, cancel := keyringTimeoutContext()
	defer cancel()
	out, err := keyringExec(ctx, nil, "lookup", "service", keyringService, "account", id)
	if err != nil {
		return "", errors.New("cannot read password from the system keyring")
	}
	// secret-tool terminates its stdout with a newline; strip only line
	// endings so passwords with meaningful leading/trailing spaces survive.
	out = bytes.TrimRight(out, "\r\n")
	if len(out) == 0 {
		return "", errors.New("system keyring returned an empty password")
	}
	return string(out), nil
}

func runSecretTool(ctx context.Context, stdin []byte, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "secret-tool", args...)
	configureCommand(cmd)
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	out := &boundedBuffer{limit: maxKeyringOutput}
	cmd.Stdout = out
	// Discard stderr: it can echo the account label or other environment
	// details and is never safe to surface in a user-facing error.
	cmd.Stderr = io.Discard
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	if out.overflow {
		return nil, errors.New("system keyring output too large")
	}
	return out.Bytes(), nil
}

type boundedBuffer struct {
	buf      bytes.Buffer
	limit    int
	overflow bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.buf.Len()+len(p) > b.limit {
		b.overflow = true
		return 0, errors.New("system keyring output exceeds limit")
	}
	return b.buf.Write(p)
}

func (b *boundedBuffer) Bytes() []byte { return b.buf.Bytes() }
