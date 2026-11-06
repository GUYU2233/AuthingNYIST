package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	campus "github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/update"
	"github.com/Palvef/AuthingNYIST/internal/version"
	"github.com/Palvef/AuthingNYIST/libauth"
	"github.com/howeyc/gopass"
	"github.com/juju/loggo"
	"gopkg.in/urfave/cli.v1"
)

type Settings struct {
	Network  string `json:"network"`
	Username string `json:"username"`
	Password string `json:"password"`
	Ip       string `json:"ip"`
	Host     string `json:"host"`
	HookSucc string `json:"hook-success"`
	NoCheck  bool   `json:"noCheck"`
	KeepOn   bool   `json:"keepOnline"`
	V6       bool   `json:"useV6"`
	Insecure bool   `json:"insecure"`
	Daemon   bool   `json:"daemonize"`
	Debug    bool   `json:"debug"`
	AcID     string `json:"acId"`
}

var logger = loggo.GetLogger("auth-nyist")
var settings Settings

// Injectable boundaries keep CLI tests read-only and independent of real networks.
var detectNetwork = campus.DetectNetwork
var inspectStatus = campus.InspectStatus
var checkUpdate = update.Check
var isOnline = libauth.IsOnline
var loginLogout = libauth.LoginLogout

func optionString(c *cli.Context, name string) string {
	if v := c.String(name); v != "" {
		return v
	}
	return c.GlobalString(name)
}
func optionBool(c *cli.Context, name string) bool { return c.Bool(name) || c.GlobalBool(name) }

func parseSettingsFile(path string) error {
	sf, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("read config file failed (%s)", err)
	}
	defer sf.Close()
	bv, _ := ioutil.ReadAll(sf)
	err = json.Unmarshal(bv, &settings)
	if err != nil {
		return fmt.Errorf("parse config file \"%s\" failed (%s)", path, err)
	}
	logger.Debugf("Read config file \"%s\" succeeded\n", path)
	return nil
}

func mergeCliSettings(c *cli.Context) {
	var merged Settings
	merged.Network = optionString(c, "network")
	if merged.Network == "" {
		merged.Network = settings.Network
	}
	if merged.Network == "" {
		merged.Network = "auto"
	}
	merged.Username = c.GlobalString("username")
	if len(merged.Username) == 0 {
		merged.Username = settings.Username
	}
	merged.Password = c.GlobalString("password")
	if len(merged.Password) == 0 {
		merged.Password = settings.Password
	}
	merged.Ip = optionString(c, "ip")
	if len(merged.Ip) == 0 {
		merged.Ip = settings.Ip
	}
	merged.Host = optionString(c, "host")
	if len(merged.Host) == 0 {
		merged.Host = settings.Host
	}
	merged.HookSucc = c.GlobalString("hook-success")
	if len(merged.HookSucc) == 0 {
		merged.HookSucc = settings.HookSucc
	}
	merged.NoCheck = settings.NoCheck || optionBool(c, "no-check")
	merged.V6 = settings.V6 || optionBool(c, "ipv6")
	merged.KeepOn = settings.KeepOn || optionBool(c, "keep-online")
	merged.Insecure = settings.Insecure || optionBool(c, "insecure")
	merged.Daemon = settings.Daemon || c.GlobalBool("daemonize")
	merged.Debug = settings.Debug || c.GlobalBool("debug")
	merged.AcID = optionString(c, "ac-id")
	if len(merged.AcID) == 0 {
		merged.AcID = settings.AcID
	}
	settings = merged
	logger.Debugf("Settings Username: \"%s\"\n", settings.Username)
	logger.Debugf("Settings Ip: \"%s\"\n", settings.Ip)
	logger.Debugf("Settings Host: \"%s\"\n", settings.Host)
	logger.Debugf("Settings HookSucc: \"%s\"\n", settings.HookSucc)
	logger.Debugf("Settings NoCheck: %t\n", settings.NoCheck)
	logger.Debugf("Settings V6: %t\n", settings.V6)
	logger.Debugf("Settings KeepOn: %t\n", settings.KeepOn)
	logger.Debugf("Settings Insecure: %t\n", settings.Insecure)
	logger.Debugf("Settings Daemon: %t\n", settings.Daemon)
	logger.Debugf("Settings Debug: %t\n", settings.Debug)
	logger.Debugf("Settings AcID: \"%s\"\n", settings.AcID)
}

func requestUser() (err error) {
	if len(settings.Username) == 0 {
		if settings.Daemon {
			err = fmt.Errorf("username can't be empty (daemon mode requires a config file with credentials)")
			return
		}
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("Username: ")
		settings.Username, _ = reader.ReadString('\n')
		settings.Username = strings.TrimSpace(settings.Username)
	}
	if len(settings.Username) == 0 {
		err = fmt.Errorf("username can't be empty")
	}
	return
}

func requestPasswd() (err error) {
	if len(settings.Password) == 0 {
		if settings.Daemon {
			err = fmt.Errorf("password can't be empty (daemon mode requires a config file with credentials)")
			return
		}
		var b []byte
		fmt.Printf("Password: ")
		b, err = gopass.GetPasswdMasked()
		if err != nil {
			err = fmt.Errorf("interrupted")
			return
		}
		settings.Password = string(b)
	}
	if len(settings.Password) == 0 {
		err = fmt.Errorf("password can't be empty")
	}
	return
}

func setLoggerLevel(debug bool, daemon bool) {
	if daemon {
		_ = loggo.ConfigureLoggers("auth-nyist=ERROR;libauth=ERROR")
	} else if debug {
		_ = loggo.ConfigureLoggers("auth-nyist=DEBUG;libauth=DEBUG")
	} else {
		_ = loggo.ConfigureLoggers("auth-nyist=INFO;libauth=INFO")
	}
}

func locateConfigFile(c *cli.Context) (cf string) {
	cf = c.GlobalString("config-file")
	if len(cf) != 0 {
		return
	}

	xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")
	homedir, _ := os.UserHomeDir()
	if len(xdgConfigHome) == 0 {
		xdgConfigHome = path.Join(homedir, ".config")
	}
	cf = path.Join(xdgConfigHome, "auth-nyist")
	_, err := os.Stat(cf)
	if !os.IsNotExist(err) {
		return
	}

	cf = path.Join(homedir, ".auth-nyist")
	_, err = os.Stat(cf)
	if !os.IsNotExist(err) {
		return
	}

	return ""
}

func parseSettings(c *cli.Context) (err error) {

	settings = Settings{}
	setLoggerLevel(c.GlobalBool("debug"), c.GlobalBool("daemonize"))

	cf := locateConfigFile(c)
	if len(cf) == 0 && c.GlobalBool("daemonize") {
		return fmt.Errorf("cannot find config file (it is necessary in daemon mode)")
	}
	if len(cf) != 0 {
		err = parseSettingsFile(cf)
		if err != nil {
			return err
		}
	}
	mergeCliSettings(c)
	setLoggerLevel(settings.Debug, settings.Daemon)
	return
}

func runHook() {
	if settings.HookSucc != "" {
		logger.Debugf("Run hook \"%s\"\n", settings.HookSucc)
		cmd := exec.Command(settings.HookSucc)
		if err := cmd.Run(); err != nil {
			logger.Errorf("Hook execution failed: %v\n", err)
		}
	}
}
func cmdAuth(c *cli.Context) error {
	if c.Bool("help") || c.GlobalBool("help") {
		return cli.ShowAppHelp(c)
	}
	return authUtil(c, optionBool(c, "logout"))
}
func cmdDeauth(c *cli.Context) error { return authUtil(c, true) }

// resolveNetwork only inspects the current link; classic CLI never switches Wi-Fi.
func resolveNetwork(ctx context.Context) error {
	switch settings.Network {
	case "", "auto":
		network, err := detectNetwork(ctx)
		if err != nil {
			return fmt.Errorf("network detection: %w (use --network nyist-tec or --network nyist-stu to select explicitly)", err)
		}
		settings.Network = network
		if network == "" {
			settings.Network = "auto"
		}
	case "nyist-tec", "nyist-stu":
	default:
		return fmt.Errorf("network must be auto, nyist-tec or nyist-stu")
	}
	logger.Infof("Network: %s (current connection; Wi-Fi is not changed)", settings.Network)
	return nil
}
func statusConfig() campus.Config {
	cfg := campus.DefaultConfig()
	cfg.Network = settings.Network
	if cfg.Network == "" {
		cfg.Network = "auto"
	}
	// Status describes the current session, regardless of configured credentials.
	if settings.Host != "" {
		cfg.Host = settings.Host
	}
	if settings.Insecure && !strings.Contains(cfg.Host, "://") {
		cfg.Host = "http://" + cfg.Host
	}
	if settings.Insecure {
		cfg.Host = strings.Replace(cfg.Host, "https://", "http://", 1)
	}
	if settings.AcID != "" {
		cfg.AcID = settings.AcID
	}
	return cfg
}
func cmdStatus(c *cli.Context) error {
	if err := parseSettings(c); err != nil {
		return err
	}
	if err := resolveNetwork(context.Background()); err != nil {
		return err
	}
	s, err := inspectStatus(context.Background(), statusConfig())
	if err != nil {
		return fmt.Errorf("status unavailable: %w", err)
	}
	state := "offline"
	if s.Online {
		state = "online"
	}
	fmt.Fprintf(c.App.Writer, "Status: %s\nNetwork: %s\n", state, s.Network)
	if s.Message != "" {
		fmt.Fprintln(c.App.Writer, s.Message)
	}
	if s.Username != "" {
		fmt.Fprintf(c.App.Writer, "Account: %s\n", s.Username)
	}
	if s.IP != "" {
		fmt.Fprintf(c.App.Writer, "IP: %s\n", s.IP)
	}
	return nil
}
func cmdCheckUpdate(c *cli.Context) error {
	result, err := checkUpdate(context.Background(), version.Version)
	if err != nil {
		return err
	}
	fmt.Fprintf(c.App.Writer, "Current: %s\nLatest: %s\n", result.CurrentVersion, result.Latest.TagName)
	switch {
	case !result.CurrentKnown:
		fmt.Fprintln(c.App.Writer, "Development/unknown version: cannot determine whether an update is needed.")
	case result.UpdateAvailable:
		fmt.Fprintln(c.App.Writer, "Update available.")
	default:
		fmt.Fprintln(c.App.Writer, "Already up to date.")
	}
	fmt.Fprintln(c.App.Writer, result.Latest.HTMLURL)
	return nil
}
func cmdKeepalive(c *cli.Context) {
	err := parseSettings(c)
	if err != nil {
		logger.Errorf("Parse setting error: %s\n", err)
		os.Exit(1)
	}
	if err = resolveNetwork(context.Background()); err != nil {
		logger.Errorf("Network error: %s", err)
		os.Exit(1)
	}
	err = keepAliveLoop(c)
	if err != nil {
		logger.Errorf("Keepalive error: %s\n", err)
		os.Exit(1)
	}
}
func authUtil(c *cli.Context, logout bool) error {
	if err := parseSettings(c); err != nil {
		return err
	}
	if err := resolveNetwork(context.Background()); err != nil {
		return err
	}
	return authenticateAndKeepAlive(logout, authOnce, func() error { return keepAliveLoop(c) })
}

// Keep loop ownership at the command boundary, including the already-online case.
// Re-authentication from the loop calls authOnce directly and cannot nest loops.
func authenticateAndKeepAlive(logout bool, authenticate func(bool) error, keepAlive func() error) error {
	if err := authenticate(logout); err != nil {
		return err
	}
	if settings.KeepOn && !logout {
		if len(settings.Ip) != 0 {
			logger.Errorf("Cannot keep another IP online\n")
		} else {
			return keepAlive()
		}
	}
	return nil
}

// Bare accounts accept a portal-assigned realm; explicitly qualified accounts must match.
func samePortalAccount(want, got string) bool {
	w := strings.SplitN(want, "@", 2)
	g := strings.SplitN(got, "@", 2)
	return w[0] != "" && w[0] == g[0] && (len(w) == 1 || (len(g) == 2 && strings.EqualFold(w[1], g[1])))
}

func authOnce(logout bool) error {
	acID := "1"
	if len(settings.AcID) != 0 {
		acID = settings.AcID
	}
	domain := settings.Host
	if len(settings.Host) == 0 {
		if settings.V6 {
			domain = "auth.nyist.edu.cn"
		} else {
			domain = "auth.nyist.edu.cn"
		}
	}

	// Accept both the historical hostname and an explicit HTTP(S) origin.
	insecure := settings.Insecure
	if strings.HasPrefix(domain, "http://") {
		insecure = true
		domain = strings.TrimPrefix(domain, "http://")
	}
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimRight(domain, "/")
	host := libauth.NewUrlProvider(domain, insecure)
	if len(settings.Ip) == 0 && !settings.NoCheck {
		online, statusErr, username := isOnline(host, acID)
		if statusErr != nil {
			return fmt.Errorf("cannot determine portal status: %w", statusErr)
		}
		if online {
			if logout && settings.Username == "" {
				if err := requestUser(); err != nil {
					return err
				}
			}
			if settings.Username != "" && !samePortalAccount(settings.Username, username) {
				return campus.ErrOtherAccount
			}
			if logout {
				settings.Username = username
			}
		}
		if online && !logout {
			logger.Infof("Currently online!")
			return nil
		} else if !online && logout {
			logger.Infof("Currently offline!")
			return nil
		}
	}
	err := requestUser()
	if err != nil {
		return err
	}
	if !logout {
		err = requestPasswd()
		if err != nil {
			return err
		}
	}

	err = loginLogout(settings.Username, settings.Password, host, logout, settings.Ip, acID)
	action := "Login"
	if logout {
		action = "Logout"
	}
	if err == nil {
		logger.Infof("%s Successfully!\n", action)
		runHook()
	} else {
		err = fmt.Errorf("%s Failed: %w", action, err)
	}
	return err
}

func keepAliveLoop(_ *cli.Context) error {
	logger.Infof("Checking connectivity to NYIST Library...")
	probe := newLibraryProbe(libraryProbeURL, 2*time.Second)
	defer probe.close()
	return runKeepAlive(context.Background(), 5*time.Second, probe.check, func() error {
		return authOnce(false)
	})
}

// Dependencies and cancellation are explicit so retry behavior can be tested
// without contacting the portal, waiting five seconds, or logging in for real.
func runKeepAlive(ctx context.Context, interval time.Duration, check, authenticate func() error) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := check(); err != nil {
			logger.Warningf("Library connectivity check failed: %v", err)
			if authErr := authenticate(); authErr != nil {
				logger.Errorf("auth error: %v", authErr)
			}
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func connectionFlags() []cli.Flag {
	return []cli.Flag{
		&cli.StringFlag{Name: "host", Usage: "portal hostname"},
		&cli.StringFlag{Name: "ac-id", Usage: "portal access controller ID"},
		&cli.StringFlag{Name: "network", Usage: "network: auto|nyist-tec|nyist-stu (never switches Wi-Fi)"},
		&cli.StringFlag{Name: "ip", Usage: "authenticate specified IP"},
		&cli.BoolFlag{Name: "no-check, n", Usage: "skip online check"},
		&cli.BoolFlag{Name: "ipv6, v", Usage: "IPv6-compatible portal"},
		&cli.BoolFlag{Name: "insecure", Usage: "use HTTP for portal"},
		&cli.BoolFlag{Name: "keep-online", Usage: "keep authenticated connection online"},
		&cli.BoolFlag{Name: "logout", Usage: "log out instead of logging in"},
	}
}
func newCLI() *cli.App {
	application := &cli.App{
		Name: "auth-nyist",
		UsageText: `auth-nyist [options]
	 auth-nyist [options] auth [auth_options]
	 auth-nyist [options] deauth [auth_options]
	 auth-nyist [options] online [online_options]`,
		Usage:    "Authenticating utility for NYIST",
		Version:  version.Version,
		Action:   cmdAuth,
		HideHelp: true,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "username, u", Usage: "your portal account `name`"},
			&cli.StringFlag{Name: "password, p", Usage: "your portal `password`"},
			&cli.StringFlag{Name: "config-file, c", Usage: "`path` to your config file, default ~/.auth-nyist"},
			&cli.StringFlag{Name: "hook-success", Usage: "command line to be executed in shell after successful login/out"},
			&cli.BoolFlag{Name: "daemonize, D", Usage: "run without reading username/password from standard input; less log"},
			&cli.BoolFlag{Name: "debug", Usage: "print debug messages"},
			&cli.BoolFlag{Name: "help, h", Usage: "print the help"},
		},
		Commands: []cli.Command{
			{Name: "status", Aliases: []string{"online"}, Usage: "inspect current session without credentials or changing Wi-Fi", Flags: connectionFlags(), Action: cmdStatus},
			{Name: "check-update", Usage: "check latest release (no installation)", Action: cmdCheckUpdate},
			{
				Name:   "auth",
				Usage:  "(default) Auth via auth.nyist.edu.cn",
				Flags:  connectionFlags(),
				Action: cmdAuth,
			},
			{
				Name:   "deauth",
				Flags:  connectionFlags(),
				Usage:  "De-authenticate via auth.nyist.edu.cn",
				Action: cmdDeauth,
			},
			{
				Name:   "keepalive",
				Flags:  connectionFlags(),
				Usage:  "Keep the connection alive by pinging a server",
				Action: cmdKeepalive,
			},
		},
	}

	globalConnectionFlags := connectionFlags()
	// -v remains the conventional global version flag; auth -v keeps IPv6 compatibility.
	for i, flag := range globalConnectionFlags {
		if flag.GetName() == "ipv6, v" {
			globalConnectionFlags[i] = &cli.BoolFlag{Name: "ipv6", Usage: "IPv6-compatible portal"}
		}
	}
	application.Flags = append(application.Flags, globalConnectionFlags...)
	return application
}

func main() {
	err := newCLI().Run(os.Args)
	if err != nil {
		logger.Errorf("Run error: %s\n", err)
		os.Exit(1)
	}
}
