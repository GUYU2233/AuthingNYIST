// Package app is the shared, UI-independent campus-network application core.
package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	Network        string `json:"network"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	AutoStart      bool   `json:"autoStart"`
	StartMinimized bool   `json:"startMinimized"`
	Background     bool   `json:"background"`
	AutoConnect    bool   `json:"autoConnect"`
	Host           string `json:"host"`
	AcID           string `json:"acId"`
}

func DefaultConfig() Config {
	return Config{Network: "nyist-tec", Host: "https://auth.nyist.edu.cn", AcID: "1"}
}
func ConfigPath() (string, error) {
	d, e := os.UserConfigDir()
	if e != nil {
		return "", e
	}
	return filepath.Join(d, "AuthingNYIST", "config.json"), nil
}

// Validate validates settings, permitting empty credentials for first-run setup.
// Connect and Run additionally require a username and password.
func (c Config) Validate() error {
	if c.Network != "nyist-tec" && c.Network != "nyist-stu" {
		return errors.New("network must be nyist-tec or nyist-stu")
	}
	if strings.TrimSpace(c.Username) != c.Username || strings.ContainsAny(c.Username, "\r\n\t ") {
		return errors.New("username contains whitespace")
	}
	if strings.Count(c.Username, "@") > 1 || strings.HasSuffix(c.Username, "@") || strings.HasPrefix(c.Username, "@") {
		return errors.New("invalid username realm")
	}
	if strings.ContainsAny(c.Password, "\r\n\x00") {
		return errors.New("password contains invalid control characters")
	}
	u, e := url.Parse(hostURL(c.Host))
	if e != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" {
		return errors.New("host must be an HTTP(S) origin without credentials, path, or query")
	}
	id, e := strconv.Atoi(c.AcID)
	if e != nil || id <= 0 {
		return errors.New("acId must be a positive integer")
	}
	return nil
}
func hostURL(h string) string {
	if !strings.Contains(h, "://") {
		return "https://" + h
	}
	return strings.TrimRight(h, "/")
}
func Load(path string) (Config, error) {
	c := DefaultConfig()
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		return c, nil
	}
	if e != nil {
		return c, errors.New("cannot read configuration")
	}
	if len(b) > 1024*1024 {
		return c, errors.New("configuration too large")
	}
	if e = json.Unmarshal(b, &c); e != nil {
		return DefaultConfig(), errors.New("invalid configuration JSON")
	}
	c.Password, e = unprotectPassword(c.Password)
	if e != nil {
		return DefaultConfig(), e
	}
	if e = c.Validate(); e != nil {
		return DefaultConfig(), e
	}
	return c, nil
}
func Save(path string, cfg Config) error {
	if e := cfg.Validate(); e != nil {
		return e
	}
	p, e := protectPassword(cfg.Password)
	if e != nil {
		return e
	}
	cfg.Password = p
	b, e := json.MarshalIndent(cfg, "", "  ")
	if e != nil {
		return errors.New("cannot encode configuration")
	}
	b = append(b, '\n')
	dir := filepath.Dir(path)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return errors.New("cannot create configuration directory")
	}
	if e = secureDirectory(dir); e != nil {
		return e
	}
	f, e := os.CreateTemp(dir, ".config-*")
	if e != nil {
		return errors.New("cannot create configuration temporary file")
	}
	name := f.Name()
	defer os.Remove(name)
	if e = f.Chmod(0600); e == nil {
		_, e = f.Write(b)
	}
	if e == nil {
		e = f.Sync()
	}
	ce := f.Close()
	if e == nil {
		e = ce
	}
	if e != nil {
		return errors.New("cannot write configuration")
	}
	if e = replaceFile(name, path); e != nil {
		return fmt.Errorf("cannot replace configuration: %w", e)
	}
	return nil
}
