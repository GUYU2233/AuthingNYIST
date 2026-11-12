//go:build !linux

package tray

import (
	"context"
	"errors"
	"testing"
)

func TestStubsReportUnsupported(t *testing.T) {
	if err := Run(context.Background(), func() State { return State{} }, Actions{}, nil); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Run = %v, want ErrUnsupported", err)
	}
	if err := Available(); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Available = %v, want ErrUnsupported", err)
	}
	if err := Notify("t", "b", true); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Notify = %v, want ErrUnsupported", err)
	}
	Refresh()
}
