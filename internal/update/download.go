package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	WindowsAssetName        = "nyist-connect.exe"
	ChecksumAssetName       = "SHA256SUMS.txt"
	MaxWindowsBytes   int64 = 256 << 20
	MaxChecksumBytes  int64 = 1 << 20
	DownloadTimeout         = 5 * time.Minute
)

// StageResult is a verified download, NOT an installed update. The user must
// exit the running application before manually replacing it. SHA256 establishes
// consistency with the same release's checksum file, not an independent signature.
type StageResult struct {
	Path      string
	Directory string
	Version   string
	SHA256    string
	Size      int64
}

// Downloader supports a custom transport (for testing/proxies); URL validation
// and redirect validation cannot be disabled. CacheDir defaults to the user's
// cache directory. Each call creates a private, unique staging directory.
type Downloader struct {
	HTTPClient *http.Client
	CacheDir   string
	Timeout    time.Duration
}

func StageWindows(ctx context.Context, release Release) (*StageResult, error) {
	return (Downloader{}).StageWindows(ctx, release)
}

func (d Downloader) StageWindows(ctx context.Context, release Release) (_ *StageResult, err error) {
	if release.Draft || release.Prerelease {
		return nil, fmt.Errorf("cannot stage a draft or prerelease")
	}
	version, err := parseVersion(release.TagName)
	if err != nil {
		return nil, fmt.Errorf("invalid release version: %w", err)
	}
	if len(version.pre) != 0 {
		return nil, fmt.Errorf("cannot stage a prerelease version")
	}
	var exe, sums *Asset
	for i := range release.Assets {
		a := &release.Assets[i]
		switch a.Name {
		case WindowsAssetName:
			if exe != nil {
				return nil, fmt.Errorf("duplicate Windows asset")
			}
			exe = a
		case ChecksumAssetName:
			if sums != nil {
				return nil, fmt.Errorf("duplicate checksum asset")
			}
			sums = a
		}
	}
	if exe == nil || sums == nil {
		return nil, fmt.Errorf("release must contain %s and %s", WindowsAssetName, ChecksumAssetName)
	}
	for _, a := range []*Asset{exe, sums} {
		if err := validateAssetURL(a.BrowserDownloadURL); err != nil {
			return nil, err
		}
		u, _ := url.Parse(a.BrowserDownloadURL)
		expectedPath := "/GUYU2233/AuthingNYIST/releases/download/" + url.PathEscape(release.TagName) + "/" + a.Name
		if u.Host != "github.com" || u.EscapedPath() != expectedPath || u.RawQuery != "" {
			return nil, fmt.Errorf("asset URL must identify this repository, release tag and asset name")
		}
	}
	if exe.Size <= 0 || exe.Size > MaxWindowsBytes || sums.Size <= 0 || sums.Size > MaxChecksumBytes {
		return nil, fmt.Errorf("release asset size outside allowed bounds")
	}
	timeout := d.Timeout
	if timeout <= 0 || timeout > DownloadTimeout {
		timeout = DownloadTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	hc := http.Client{}
	if d.HTTPClient != nil {
		hc = *d.HTTPClient
	}
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("too many asset redirects")
		}
		return validateAssetURL(req.URL.String())
	}
	body, err := fetchAsset(ctx, &hc, *sums, MaxChecksumBytes)
	if err != nil {
		return nil, err
	}
	checksumData, err := io.ReadAll(io.LimitReader(body, MaxChecksumBytes+1))
	body.Close()
	if err != nil {
		return nil, fmt.Errorf("read checksum: %w", err)
	}
	if int64(len(checksumData)) != sums.Size {
		return nil, fmt.Errorf("checksum size does not match release metadata")
	}
	expected, err := windowsChecksum(string(checksumData))
	if err != nil {
		return nil, err
	}
	root := d.CacheDir
	if root == "" {
		root, err = os.UserCacheDir()
		if err != nil {
			return nil, fmt.Errorf("find user cache: %w", err)
		}
		root = filepath.Join(root, "nyist-connect", "updates")
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve staging directory: %w", err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(root, "windows-")
	if err != nil {
		return nil, err
	}
	// Only this newly created private directory is removed on failure.
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dir)
		}
	}()
	path := filepath.Join(dir, WindowsAssetName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	body, err = fetchAsset(ctx, &hc, *exe, MaxWindowsBytes)
	if err != nil {
		f.Close()
		return nil, err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(body, exe.Size+1))
	body.Close()
	syncErr := f.Sync()
	closeErr := f.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("download executable: %w", copyErr)
	}
	if syncErr != nil {
		return nil, syncErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if n != exe.Size {
		return nil, fmt.Errorf("executable size does not match release metadata")
	}
	actual := hex.EncodeToString(hash.Sum(nil))
	if actual != expected {
		return nil, fmt.Errorf("executable SHA256 mismatch")
	}
	return &StageResult{Path: path, Directory: dir, Version: release.TagName, SHA256: actual, Size: n}, nil
}

func validateAssetURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid asset URL")
	}
	if u.Scheme != "https" || u.User != nil || u.Opaque != "" || u.Fragment != "" || (u.Port() != "" && u.Port() != "443") {
		return fmt.Errorf("asset URL must use HTTPS without credentials or nonstandard ports")
	}
	switch u.Hostname() {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return nil
	default:
		return fmt.Errorf("asset URL host is not an allowed GitHub asset host")
	}
}

func fetchAsset(ctx context.Context, hc *http.Client, a Asset, max int64) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.BrowserDownloadURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "AuthingNYIST-update-downloader")
	req.Header.Set("Accept-Encoding", "identity")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", a.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: HTTP %d", a.Name, resp.StatusCode)
	}
	if resp.ContentLength > max || (resp.ContentLength >= 0 && resp.ContentLength != a.Size) {
		resp.Body.Close()
		return nil, fmt.Errorf("download %s: unexpected content length", a.Name)
	}
	return resp.Body, nil
}

func windowsChecksum(text string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 4096), int(MaxChecksumBytes))
	found := ""
	names := make(map[string]bool)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		// Standard sha256sum format: exactly 64 hexadecimal digits, space,
		// text/binary marker, then a plain basename (no escaped/path names).
		if len(line) < 67 || line[64] != ' ' || (line[65] != ' ' && line[65] != '*') {
			return "", fmt.Errorf("malformed SHA256SUMS entry")
		}
		hash, name := line[:64], line[66:]
		if _, err := hex.DecodeString(hash); err != nil {
			return "", fmt.Errorf("invalid SHA256 digest")
		}
		if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\:\x00\r\n\t") || strings.TrimSpace(name) != name {
			return "", fmt.Errorf("unsafe checksum filename")
		}
		if names[name] {
			return "", fmt.Errorf("duplicate checksum filename")
		}
		names[name] = true
		if name == WindowsAssetName {
			found = strings.ToLower(hash)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("read checksum entries: %w", err)
	}
	if found == "" {
		return "", fmt.Errorf("missing Windows executable checksum")
	}
	return found, nil
}
