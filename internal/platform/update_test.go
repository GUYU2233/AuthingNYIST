package platform

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/update"
	"github.com/Palvef/AuthingNYIST/internal/version"
)

func TestPrintUpdateDecisions(t *testing.T) {
	original := checkRelease
	t.Cleanup(func() { checkRelease = original })
	for _, tc := range []struct {
		name             string
		known, available bool
		message          string
	}{
		{"development", false, false, "开发/自定义构建"},
		{"new release", true, true, "不会自动安装"},
		{"up to date", true, false, "无需更新"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type key struct{}
			ctx := context.WithValue(context.Background(), key{}, "sentinel")
			checkRelease = func(got context.Context, current string) (*update.Result, error) {
				if current != version.Version || got.Value(key{}) != "sentinel" {
					t.Error("version/context not forwarded")
				}
				return &update.Result{CurrentKnown: tc.known, UpdateAvailable: tc.available, Latest: update.Release{TagName: "v9.0.0", HTMLURL: "https://example.test/release"}}, nil
			}
			var out bytes.Buffer
			if err := printUpdate(ctx, &out); err != nil {
				t.Fatal(err)
			}
			for _, s := range []string{version.Version, "v9.0.0", "https://example.test/release", tc.message} {
				if !strings.Contains(out.String(), s) {
					t.Errorf("missing %q in %q", s, out.String())
				}
			}
		})
	}
}

func TestPrintUpdateFailure(t *testing.T) {
	original := checkRelease
	t.Cleanup(func() { checkRelease = original })
	want := errors.New("test unavailable")
	checkRelease = func(context.Context, string) (*update.Result, error) { return nil, want }
	var out bytes.Buffer
	err := printUpdate(context.Background(), &out)
	if !errors.Is(err, want) || !strings.Contains(err.Error(), "检查更新失败") {
		t.Fatalf("error=%v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected success output: %q", out.String())
	}
}
