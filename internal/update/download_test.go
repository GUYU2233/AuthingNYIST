package update

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type assetTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (tr assetTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clone := r.Clone(r.Context())
	u := *r.URL
	u.Scheme = tr.target.Scheme
	u.Host = tr.target.Host
	clone.URL = &u
	return tr.base.RoundTrip(clone)
}
func fixture(t *testing.T, handler http.HandlerFunc) (Downloader, Release) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	target, _ := url.Parse(server.URL)
	client := &http.Client{Transport: assetTransport{target, server.Client().Transport}}
	data := "test Windows executable"
	sums := fmt.Sprintf("%x  nyist-connect.exe\n", sha256.Sum256([]byte(data)))
	return Downloader{HTTPClient: client, CacheDir: t.TempDir()}, Release{TagName: "v1.2.3", Assets: []Asset{
		{Name: WindowsAssetName, BrowserDownloadURL: "https://github.com/GUYU2233/AuthingNYIST/releases/download/v1.2.3/nyist-connect.exe", Size: int64(len(data))},
		{Name: ChecksumAssetName, BrowserDownloadURL: "https://github.com/GUYU2233/AuthingNYIST/releases/download/v1.2.3/SHA256SUMS.txt", Size: int64(len(sums))},
	}}
}
func TestStageWindows(t *testing.T) {
	data := "test Windows executable"
	sums := fmt.Sprintf("%x  nyist-connect.exe\n", sha256.Sum256([]byte(data)))
	d, r := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/SHA256SUMS.txt") {
			fmt.Fprint(w, sums)
		} else {
			fmt.Fprint(w, data)
		}
	})
	result, err := d.StageWindows(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(result.Path)
	if err != nil || string(got) != data {
		t.Fatalf("staged data %q: %v", got, err)
	}
	if filepath.Base(result.Path) != WindowsAssetName || result.Size != int64(len(data)) || result.Version != r.TagName {
		t.Fatalf("unexpected result: %+v", result)
	}
	again, err := d.StageWindows(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	if again.Directory == result.Directory {
		t.Fatal("staging directory reused")
	}
}
func TestStageRejectsCorruptDownloadAndCleans(t *testing.T) {
	sums := fmt.Sprintf("%x  nyist-connect.exe\n", sha256.Sum256([]byte("test Windows executable")))
	for _, payload := range []string{"evil Windows executable", "short", strings.Repeat("x", 30)} {
		t.Run(payload, func(t *testing.T) {
			d, r := fixture(t, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/SHA256SUMS.txt") {
					fmt.Fprint(w, sums)
				} else {
					fmt.Fprint(w, payload)
				}
			})
			if _, err := d.StageWindows(context.Background(), r); err == nil {
				t.Fatal("accepted corrupt executable")
			}
			entries, err := os.ReadDir(d.CacheDir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed staging left files: %v %v", entries, err)
			}
		})
	}
}
func TestAssetURLValidation(t *testing.T) {
	for _, raw := range []string{"http://github.com/a", "https://github.com.evil/a", "https://evil/a", "https://github.com:8443/a", "https://u:p@github.com/a", "https://github.com/a#fragment", "https://127.0.0.1/a"} {
		if validateAssetURL(raw) == nil {
			t.Errorf("accepted %s", raw)
		}
	}
	for _, host := range []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com"} {
		if err := validateAssetURL("https://" + host + "/a?token=test"); err != nil {
			t.Error(err)
		}
	}
}
func TestChecksumValidation(t *testing.T) {
	hash := strings.Repeat("a", 64)
	for _, text := range []string{
		hash + "  ../nyist-connect.exe\n", hash + "  dir\\nyist-connect.exe\n", hash + "  nyist-connect.exe\n" + hash + "  nyist-connect.exe\n",
		hash + "  other.exe\n", strings.Repeat("z", 64) + "  nyist-connect.exe\n", hash + "  C:evil.exe\n", hash + "  nyist-connect.exe \n",
	} {
		if _, err := windowsChecksum(text); err == nil {
			t.Errorf("accepted checksum %q", text)
		}
	}
	if got, err := windowsChecksum(strings.ToUpper(hash) + " *nyist-connect.exe\n"); err != nil || got != hash {
		t.Fatalf("binary marker: %q %v", got, err)
	}
}
func TestStageRejectsUnsafeRedirect(t *testing.T) {
	d, r := fixture(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/payload", http.StatusFound)
	})
	if _, err := d.StageWindows(context.Background(), r); err == nil || !strings.Contains(err.Error(), "allowed GitHub") {
		t.Fatalf("redirect: %v", err)
	}
}
func TestStageMetadataValidation(t *testing.T) {
	d, r := fixture(t, func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") })
	for _, modify := range []func(*Release){
		func(r *Release) { r.Assets = r.Assets[:1] },
		func(r *Release) { r.Assets = append(r.Assets, r.Assets[0]) },
		func(r *Release) { r.Assets[0].Size = MaxWindowsBytes + 1 },
		func(r *Release) { r.Assets[0].Size = 0 },
		func(r *Release) { r.Assets[0].BrowserDownloadURL = "http://github.com/exe" },
		func(r *Release) { r.Draft = true },
		func(r *Release) { r.Prerelease = true },
		func(r *Release) { r.TagName = "v1.2.3-beta.1" },
		func(r *Release) {
			r.Assets[0].BrowserDownloadURL = "https://github.com/other/repo/releases/download/v1.2.3/nyist-connect.exe"
		},
		func(r *Release) {
			r.Assets[0].BrowserDownloadURL = "https://github.com/GUYU2233/AuthingNYIST/releases/download/v9.9.9/nyist-connect.exe"
		},
		func(r *Release) { r.Assets[0].BrowserDownloadURL += "?redirect=evil" },
	} {
		clone := r
		clone.Assets = append([]Asset(nil), r.Assets...)
		modify(&clone)
		if _, err := d.StageWindows(context.Background(), clone); err == nil {
			t.Fatal("accepted invalid release")
		}
	}
}
func TestStageCancellation(t *testing.T) {
	d, r := fixture(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := d.StageWindows(ctx, r); err == nil {
		t.Fatal("ignored cancellation")
	}
}
