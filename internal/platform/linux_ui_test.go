//go:build linux

package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/huh"
)

func TestLinuxUIAbortClassification(t *testing.T) {
	for _, err := range []error{huh.ErrUserAborted, context.Canceled, fmt.Errorf("wrapped: %w", huh.ErrUserAborted)} {
		if !linuxUIAborted(err) {
			t.Fatalf("not cancellation: %v", err)
		}
	}
	if linuxUIAborted(errors.New("failure")) || linuxUIAborted(nil) {
		t.Fatal("ordinary result treated as cancellation")
	}
}

func TestLinuxUIOperationFreshBoundedContext(t *testing.T) {
	var out bytes.Buffer
	ui := linuxUI{out: &out}
	var previous context.Context
	for i := 0; i < 2; i++ {
		err := ui.operation("test", func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Fatal("operation began canceled")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 45*time.Second {
				t.Fatal("missing operation deadline")
			}
			previous = ctx
			return errors.New("sample failure")
		})
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(previous.Err(), context.Canceled) {
			t.Fatal("operation context not released")
		}
	}
	if strings.Count(out.String(), "sample failure") != 2 {
		t.Fatal(out.String())
	}
}

func TestLinuxNoActionRejectsNonTerminal(t *testing.T) {
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	var out bytes.Buffer
	err = linuxMain([]string{"--config", filepath.Join(t.TempDir(), "config.json")}, in, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "交互菜单需要终端") {
		t.Fatalf("expected non-terminal guidance, got %v", err)
	}
}
