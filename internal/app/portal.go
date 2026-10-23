package app

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Palvef/AuthingNYIST/libauth"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Status struct {
	Online   bool
	Username string
	Domain   string
	IP       string
	Message  string
}

var ErrCredentials = errors.New("authentication rejected; verify account and password before retrying")
var ErrOtherAccount = errors.New("another account is online; refusing to replace or disconnect its session")
var portalHTTP = &http.Client{Timeout: 12 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}

type portalReply struct {
	Error     string `json:"error"`
	Res       string `json:"res"`
	Challenge string `json:"challenge"`
	ClientIP  string `json:"client_ip"`
	Username  string `json:"user_name"`
	Domain    string `json:"domain"`
	IP        string `json:"online_ip"`
	IP6       string `json:"online_ip6"`
}

// portalGet deliberately discards transport URLs and raw server error messages:
// net/http errors and authentication query strings may otherwise disclose secrets.
func portalGet(ctx context.Context, cfg Config, path string, q url.Values) (portalReply, error) {
	var out portalReply
	q.Set("callback", "authing_callback")
	req, e := http.NewRequestWithContext(ctx, http.MethodGet, hostURL(cfg.Host)+path+"?"+q.Encode(), nil)
	if e != nil {
		return out, errors.New("cannot build portal request")
	}
	req.Header.Set("User-Agent", "AuthingNYIST")
	resp, e := portalHTTP.Do(req)
	if e != nil {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, errors.New("portal unavailable (network or TLS error)")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("portal HTTP status %d", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if e != nil || len(b) > 1024*1024 {
		return out, errors.New("cannot read portal response")
	}
	s := strings.TrimSpace(string(b))
	// Some portal versions return this exact plain-text sentinel instead of JSONP.
	// Only accept it for the read-only status endpoint, never as a login reply.
	if path == "/cgi-bin/rad_user_info" && s == "not_online_error" {
		out.Error = s
		return out, nil
	}
	if strings.HasPrefix(s, "authing_callback(") {
		s = strings.TrimPrefix(s, "authing_callback(")
		s = strings.TrimSuffix(strings.TrimSuffix(s, ";"), ")")
	}
	if json.Unmarshal([]byte(s), &out) != nil {
		return out, errors.New("invalid portal response")
	}
	return out, nil
}

// Account matching is case-sensitive. Explicit realms must match. A bare account
// accepts the realm assigned by the official portal, not another account's name.
func sameAccount(want, got, domain string) bool {
	wb, wr := splitAccount(want)
	gb, gr := splitAccount(got)
	if gr == "" {
		gr = strings.TrimPrefix(domain, "@")
	}
	return wb != "" && wb == gb && (wr == "" || strings.EqualFold(wr, gr))
}
func splitAccount(s string) (string, string) {
	p := strings.SplitN(s, "@", 2)
	if len(p) == 2 {
		return p[0], p[1]
	}
	return s, ""
}

// InspectStatus adds local link diagnostics for user-facing status checks.
// Authentication uses Check directly: a missing link must never be mistaken
// for a confirmed offline portal session or permit an unsafe logout/login.
func InspectStatus(ctx context.Context, cfg Config) (Status, error) {
	if err := cfg.Validate(); err != nil {
		return Status{}, err
	}
	if err := ctx.Err(); err != nil {
		return Status{}, err
	}
	message, err := localNetworkStatus(ctx, cfg)
	if err != nil {
		return Status{}, err
	}
	if message != "" {
		return Status{Message: message}, nil
	}
	s, err := Check(ctx, cfg)
	if err != nil {
		if ctx.Err() != nil {
			return Status{}, ctx.Err()
		}
		return Status{}, fmt.Errorf("local network connected, but campus authentication status is unavailable: %w", err)
	}
	return s, nil
}

func Check(ctx context.Context, cfg Config) (Status, error) {
	var s Status
	if e := cfg.Validate(); e != nil {
		return s, e
	}
	r, e := portalGet(ctx, cfg, "/cgi-bin/rad_user_info", url.Values{})
	if e != nil {
		return s, e
	}
	s.Username = r.Username
	s.Domain = r.Domain
	s.IP = r.IP
	if s.IP == "" {
		s.IP = r.IP6
	}
	if r.Error == "ok" && r.Username != "" {
		s.Online = sameAccount(cfg.Username, r.Username, r.Domain)
		if s.Online {
			s.Message = "Connected"
		} else {
			s.Message = "Another account is online"
		}
		return s, nil
	}
	if r.Error == "not_online_error" || r.Error == "not_online" || r.Error == "offline" {
		s.Message = "Disconnected"
		return s, nil
	}
	return s, errors.New("portal did not provide a valid online status")
}
func requireCredentials(cfg Config) error {
	if e := cfg.Validate(); e != nil {
		return e
	}
	if cfg.Username == "" || cfg.Password == "" {
		return errors.New("username and password are required")
	}
	return nil
}
func Connect(ctx context.Context, cfg Config) (Status, error) {
	var s Status
	if e := requireCredentials(cfg); e != nil {
		return s, e
	}
	// Protect another account before switching, but always honor an explicit
	// network selection even when this account is online on a different SSID.
	s, e := Check(ctx, cfg)
	if e == nil && !s.Online && s.Username != "" {
		return s, ErrOtherAccount
	}
	if e = EnsureNetwork(ctx, cfg); e != nil {
		return s, e
	}
	s, e = Check(ctx, cfg)
	if e != nil {
		return s, e
	}
	if s.Online {
		return s, nil
	}
	if s.Username != "" {
		return s, ErrOtherAccount
	}
	if e = authenticate(ctx, cfg, false); e != nil {
		return s, e
	}
	s, e = Check(ctx, cfg)
	if e != nil {
		return s, e
	}
	if !s.Online {
		return s, errors.New("authentication response succeeded but requested account is not online")
	}
	return s, nil
}
func Disconnect(ctx context.Context, cfg Config) error {
	if e := cfg.Validate(); e != nil {
		return e
	}
	if cfg.Username == "" {
		return errors.New("username is required")
	}
	s, e := Check(ctx, cfg)
	if e != nil {
		return e
	}
	if s.Username == "" {
		return nil
	}
	if !s.Online {
		return ErrOtherAccount
	}
	// Match the official portal: logout uses the online session's full account
	// and IP, not a fresh challenge or the possibly unqualified configured name.
	username := s.Username
	if _, realm := splitAccount(username); realm == "" && s.Domain != "" {
		username += "@" + strings.TrimPrefix(s.Domain, "@")
	}
	r, e := portalGet(ctx, cfg, "/cgi-bin/srun_portal", url.Values{
		"action": {"logout"}, "username": {username}, "ip": {s.IP}, "ac_id": {cfg.AcID},
	})
	if e != nil {
		return e
	}
	if r.Error != "ok" {
		return errors.New("portal rejected logout; session may still be online")
	}
	// Do not report success until the server confirms this account is offline.
	for attempt := 0; attempt < 4; attempt++ {
		status, err := Check(ctx, cfg)
		if err != nil {
			return fmt.Errorf("cannot confirm logout: %w", err)
		}
		if status.Username == "" {
			return nil
		}
		if !status.Online {
			return ErrOtherAccount
		}
		if attempt < 3 {
			timer := time.NewTimer(500 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	return errors.New("logout requested but account is still online")
}
func authenticate(ctx context.Context, cfg Config, logout bool) error {
	r, e := portalGet(ctx, cfg, "/cgi-bin/get_challenge", url.Values{"username": {cfg.Username}, "ip": {""}, "double_stack": {"1"}})
	if e != nil {
		return e
	}
	if r.Res != "ok" || len(r.Challenge) < 16 || r.ClientIP == "" {
		return errors.New("portal challenge unavailable")
	}
	token, ip := r.Challenge, r.ClientIP
	mac := hmac.New(md5.New, []byte(token))
	mac.Write([]byte(cfg.Password))
	hm := fmt.Sprintf("%x", mac.Sum(nil))
	info := map[string]string{"username": cfg.Username, "password": cfg.Password, "ip": ip, "acid": cfg.AcID, "enc_ver": "srun_bx1"}
	action := "login"
	if logout {
		action = "logout"
		delete(info, "password")
	}
	b, _ := json.Marshal(info)
	encoded := libauth.XEncode(string(b), token)
	if encoded == nil {
		return errors.New("cannot encode authentication request")
	}
	q := url.Values{"action": {action}, "ac_id": {cfg.AcID}, "n": {"200"}, "type": {"1"}, "ip": {ip}, "double_stack": {"1"}, "username": {cfg.Username}, "info": {"{SRBX1}" + libauth.QuirkBase64Encode(*encoded)}}
	sum := token + cfg.Username
	if !logout {
		q.Set("password", "{MD5}"+hm)
		sum += token + hm
	}
	sum += token + cfg.AcID + token + ip + token + "200" + token + "1" + token + q.Get("info")
	q.Set("chksum", fmt.Sprintf("%x", sha1.Sum([]byte(sum))))
	r, e = portalGet(ctx, cfg, "/cgi-bin/srun_portal", q)
	if e != nil {
		return e
	}
	if r.Error == "ok" {
		return nil
	}
	// All portal rejections are terminal. This intentionally errs on the safe side:
	// no automatic retries can lock an account or hot-loop on a bad password.
	return ErrCredentials
}

// Run monitors every 30 seconds, reconnecting when necessary. At most five
// consecutive transient failures are retried with 2/4/8/16 second backoff.
// Authentication rejections and conflicting accounts stop immediately.
func Run(ctx context.Context, cfg Config, report func(Status)) error {
	if e := requireCredentials(cfg); e != nil {
		return e
	}
	failures := 0
	for {
		if e := ctx.Err(); e != nil {
			return e
		}
		s, e := Connect(ctx, cfg)
		if e == nil {
			failures = 0
			if report != nil {
				report(s)
			}
		} else {
			if report != nil {
				report(Status{Message: e.Error()})
			}
			if errors.Is(e, ErrCredentials) || errors.Is(e, ErrOtherAccount) {
				return e
			}
			failures++
			if failures >= 5 {
				return e
			}
		}
		delay := 30 * time.Second
		if failures > 0 {
			delay = time.Duration(1<<uint(failures)) * time.Second
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
