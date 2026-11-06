// Package update checks GitHub release metadata and can stage verified Windows
// downloads for manual replacement. It never installs or executes assets.
// Development versions are reported without an upgrade decision.
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	LatestReleaseURL = "https://api.github.com/repos/GUYU2233/AuthingNYIST/releases/latest"
	DefaultTimeout   = 10 * time.Second
	MaxResponseBytes = 1 << 20
)

// Asset describes an optional download, without making assumptions about filename
// conventions or attempting to execute it. Callers can display its name/URL.
type Asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type Release struct {
	TagName    string  `json:"tag_name"`
	Name       string  `json:"name"`
	HTMLURL    string  `json:"html_url"`
	Prerelease bool    `json:"prerelease"`
	Draft      bool    `json:"draft"`
	Assets     []Asset `json:"assets"`
}

type Result struct {
	CurrentVersion string
	Latest         Release
	// CurrentKnown is false for development or otherwise non-semantic versions.
	CurrentKnown    bool
	UpdateAvailable bool
}

// Client allows transport and endpoint injection (for example for tests).
// Zero values use the public GitHub endpoint, http.DefaultClient and a bounded
// timeout. Timeout <= 0 uses DefaultTimeout; an earlier context deadline wins.
type Client struct {
	HTTPClient *http.Client
	URL        string
	Timeout    time.Duration
}

// Check checks the latest stable release of GUYU2233/AuthingNYIST.
func Check(ctx context.Context, currentVersion string) (*Result, error) {
	return (Client{}).Check(ctx, currentVersion)
}

func (c Client) Check(ctx context.Context, currentVersion string) (*Result, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	endpoint := c.URL
	if endpoint == "" {
		endpoint = LatestReleaseURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "AuthingNYIST-update-checker")
	hc := c.HTTPClient
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("check latest release: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		// Status alone is intentional: do not echo arbitrary server text or tokens.
		return nil, fmt.Errorf("check latest release: HTTP %d (%s)", resp.StatusCode, http.StatusText(resp.StatusCode))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read release response: %w", err)
	}
	if len(data) > MaxResponseBytes {
		return nil, fmt.Errorf("release response exceeds %d bytes", MaxResponseBytes)
	}
	var release Release
	if err := json.Unmarshal(data, &release); err != nil {
		return nil, fmt.Errorf("decode release response: %w", err)
	}
	latest, err := parseVersion(release.TagName)
	if err != nil {
		return nil, fmt.Errorf("invalid release tag %q: %w", release.TagName, err)
	}
	if release.Draft {
		return nil, fmt.Errorf("latest release is a draft")
	}
	result := &Result{CurrentVersion: currentVersion, Latest: release}
	current, err := parseVersion(currentVersion)
	if err != nil || isDevelopmentVersion(currentVersion) {
		return result, nil
	}
	result.CurrentKnown = true
	// The stable endpoint must never recommend an unexpectedly returned preview.
	result.UpdateAvailable = !release.Prerelease && len(latest.pre) == 0 && compare(latest, current) > 0
	return result, nil
}

func isDevelopmentVersion(s string) bool {
	s = strings.ToLower(s)
	return s == "dev" || strings.HasPrefix(s, "devel") || strings.Contains(s, "-dev")
}
