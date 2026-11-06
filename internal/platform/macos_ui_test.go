package platform

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/charmbracelet/huh"
)

func TestMacUIAbortClassification(t *testing.T) {
	for _, err := range []error{context.Canceled, huh.ErrUserAborted} {
		if !macUIAborted(err) {
			t.Fatalf("abort not recognized: %v", err)
		}
	}
	if macUIAborted(nil) || macUIAborted(errors.New("request failed")) {
		t.Fatal("ordinary result treated as abort")
	}
}

func TestMacUIOperationFreshContextAndDeadline(t *testing.T) {
	var out bytes.Buffer
	ui := macUI{out: &out}
	var previous context.Context
	for i := 0; i < 2; i++ {
		err := ui.operation("状态", false, func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Fatal("operation started canceled")
			}
			if _, ok := ctx.Deadline(); !ok {
				t.Fatal("request has no deadline")
			}
			if ctx == previous {
				t.Fatal("operation reused context")
			}
			previous = ctx
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		if !errors.Is(previous.Err(), context.Canceled) {
			t.Fatal("completed operation context not released")
		}
	}
}

func TestMacUIKeepaliveHasNoDeadlineAndReturnsErrors(t *testing.T) {
	var out bytes.Buffer
	want := errors.New("lock unavailable")
	err := (macUI{out: &out}).operation("前台保活", true, func(ctx context.Context) error {
		if _, ok := ctx.Deadline(); ok {
			t.Fatal("keepalive must not expire")
		}
		return want
	})
	if !errors.Is(err, want) {
		t.Fatalf("operation error lost: %v", err)
	}
}
