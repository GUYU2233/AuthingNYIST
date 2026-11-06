package update

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCompareVersions(t *testing.T) {
	order := []string{"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta", "1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0", "1.0.1", "1.1.0", "2.0.0", "999999999999999999999999.0.0"}
	for i, a := range order {
		for j, b := range order {
			got, err := CompareVersions(a, b)
			want := 0
			if i < j {
				want = -1
			}
			if i > j {
				want = 1
			}
			if err != nil || got != want {
				t.Errorf("compare(%s,%s)=%d,%v want %d", a, b, got, err, want)
			}
		}
	}
	if n, err := CompareVersions("v1.2.3+build.5", "1.2.3+other"); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	for _, bad := range []string{"dev", "", "1.2", "01.2.3", "1.02.3", "1.2.3-01", "1.2.3-", "1.2.3+", "1.2.3+a+b", "1.2.3-a_1"} {
		if _, err := CompareVersions(bad, "1.0.0"); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}
func TestCheck(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.Header.Get("Accept") == "" || r.Header.Get("User-Agent") == "" {
			t.Error("missing request metadata")
		}
		fmt.Fprint(w, `{"tag_name":"v2.0.0","html_url":"https://example.test/release","assets":[{"name":"linux-amd64","browser_download_url":"https://example.test/asset","size":12}]}`)
	}))
	defer server.Close()
	c := Client{URL: server.URL, HTTPClient: server.Client()}
	for _, tc := range []struct {
		current       string
		known, update bool
	}{
		{"1.0.0", true, true}, {"2.0.0", true, false}, {"3.0.0", true, false}, {"2.0.0-rc.1", true, true}, {"dev", false, false}, {"v2.0.0-dev.1", false, false}, {"custom", false, false},
	} {
		result, err := c.Check(context.Background(), tc.current)
		if err != nil {
			t.Fatal(err)
		}
		if result.CurrentKnown != tc.known || result.UpdateAvailable != tc.update || len(result.Latest.Assets) != 1 {
			t.Errorf("%s: %+v", tc.current, result)
		}
	}
}
func TestResponseErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"not found", 404, `{}`}, {"rate limit", 403, `{}`}, {"server", 500, `{}`}, {"malformed", 200, `{`}, {"trailing", 200, `{"tag_name":"1.0.0"}{}`}, {"missing tag", 200, `{}`}, {"bad tag", 200, `{"tag_name":"nope"}`}, {"draft", 200, `{"tag_name":"1.0.0","draft":true}`}, {"oversize", 200, strings.Repeat(" ", MaxResponseBytes+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer s.Close()
			if _, err := (Client{URL: s.URL}).Check(context.Background(), "1.0.0"); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
func TestNoPreviewRecommendation(t *testing.T) {
	for _, body := range []string{`{"tag_name":"2.0.0-rc.1"}`, `{"tag_name":"2.0.0","prerelease":true}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) }))
		result, err := (Client{URL: s.URL}).Check(context.Background(), "1.0.0")
		s.Close()
		if err != nil || result.UpdateAvailable {
			t.Fatalf("result=%+v err=%v", result, err)
		}
	}
}
func TestCancellationAndTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	c := Client{URL: s.URL, Timeout: 20 * time.Millisecond}
	_, err := c.Check(context.Background(), "1.0.0")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = c.Check(ctx, "1.0.0")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
