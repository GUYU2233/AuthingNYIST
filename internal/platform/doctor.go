package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/version"
)

// errDoctorBlocking is deliberately not printed by the CLI: the doctor report
// itself explains which checks block authentication. The non-zero exit code is
// still propagated to the shell.
var errDoctorBlocking = errors.New("诊断发现阻断认证的问题")

type doctorRoute struct {
	Interface    string
	HardwareAddr string
}

type doctorDNS struct {
	Path            string
	Exists          bool
	Nameserver      string
	SystemdResolved bool
}

// doctorOptions keeps every side effect injectable so tests never touch the
// real network, systemd, or the user's configuration.
type doctorOptions struct {
	configPath string

	stat         func(string) (os.FileMode, error)
	readFile     func(string) ([]byte, error)
	loadConfig   func(string) (app.Config, error)
	detectNet    func(context.Context) (string, error)
	getenv       func(string) string
	dialContext  func(context.Context, string, string) (net.Conn, error)
	defaultRoute func(context.Context) (doctorRoute, error)
	resolver     func(context.Context) (doctorDNS, error)
	keepalive    func(context.Context) (string, error)
	version      string

	timeout time.Duration
}

type doctorCheck struct {
	key      string
	value    string
	blocking bool
	warn     bool
}

func newDoctorOptions(configPath string) doctorOptions {
	return doctorOptions{
		configPath: configPath,
		stat: func(path string) (os.FileMode, error) {
			info, err := os.Stat(path)
			if err != nil {
				return 0, err
			}
			return info.Mode(), nil
		},
		readFile: func(path string) ([]byte, error) {
			f, err := os.Open(path)
			if err != nil {
				return nil, err
			}
			defer f.Close()
			return io.ReadAll(io.LimitReader(f, 1024*1024+1))
		},
		loadConfig:   app.Load,
		detectNet:    app.DetectNetwork,
		getenv:       os.Getenv,
		dialContext:  (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		defaultRoute: probeDefaultRoute,
		resolver:     probeResolver,
		version:      version.Version,
		timeout:      5 * time.Second,
	}
}

// runDoctor prints one "key: value" line per check (never a secret) and
// returns errDoctorBlocking only when a check that prevents authentication
// fails. Network and command work is bounded by opts.timeout.
func runDoctor(ctx context.Context, out io.Writer, opts doctorOptions) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if opts.timeout <= 0 {
		opts.timeout = 5 * time.Second
	}
	var checks []doctorCheck
	add := func(key, value string) {
		checks = append(checks, doctorCheck{key: key, value: value})
	}
	addCheck := func(c doctorCheck) {
		checks = append(checks, c)
	}

	cfg := app.DefaultConfig()
	loaded := false
	exists := false
	readable := false

	mode, statErr := opts.stat(opts.configPath)
	switch {
	case statErr == nil:
		exists = true
		add("配置文件", opts.configPath)
		add("配置存在", "是")
		if _, err := opts.readFile(opts.configPath); err != nil {
			addCheck(doctorCheck{key: "配置可读", value: "否（" + err.Error() + "）", blocking: true})
		} else {
			readable = true
			add("配置可读", "是")
		}
		perm := mode.Perm()
		value := fmt.Sprintf("%04o", perm)
		warn := false
		if perm&0077 != 0 {
			value += "（警告：组或其他用户有权限）"
			warn = true
		}
		addCheck(doctorCheck{key: "配置权限", value: value, warn: warn})
	case os.IsNotExist(statErr):
		add("配置文件", opts.configPath)
		addCheck(doctorCheck{key: "配置存在", value: "否（尚未配置账号）", blocking: true})
		addCheck(doctorCheck{key: "配置可读", value: "否（文件不存在）", blocking: true})
	default:
		add("配置文件", opts.configPath)
		addCheck(doctorCheck{key: "配置存在", value: "无法检查（" + statErr.Error() + "）", blocking: true})
		addCheck(doctorCheck{key: "配置可读", value: "无法检查", blocking: true})
	}

	passwordMissing := false
	if exists && readable {
		c, err := opts.loadConfig(opts.configPath)
		switch {
		case err == nil:
			cfg = c
			loaded = true
		case errors.Is(err, app.ErrPasswordUnavailable):
			// The JSON and settings are fine; only the stored secret is gone.
			cfg = c
			loaded = true
			passwordMissing = true
			add("配置内容", "有效（密码引用无法解析）")
		default:
			addCheck(doctorCheck{key: "配置内容", value: "无效（" + err.Error() + "）", blocking: true})
		}
	}
	if passwordMissing {
		addCheck(doctorCheck{key: "密码", value: "需重新输入（密钥环条目或跨平台凭据不可用）", blocking: true})
	} else if !loaded {
		add("密码", "无法确认（配置不可用）")
	} else if cfg.Password == "" {
		addCheck(doctorCheck{key: "密码", value: "未设置", blocking: true})
	} else {
		add("密码", "已设置")
	}

	selected := "未知"
	if loaded {
		selected = cfg.Network
	}
	add("网络选择", selected)

	netCtx, cancel := context.WithTimeout(ctx, opts.timeout)
	defer cancel()

	detected := ""
	var detectErr error
	if opts.detectNet != nil {
		detected, detectErr = opts.detectNet(netCtx)
	}
	switch {
	case detectErr != nil:
		addCheck(doctorCheck{key: "当前校园网络", value: "检测失败（" + detectErr.Error() + "）"})
	case detected == "":
		add("当前校园网络", "未检测到校园 Wi-Fi（可能使用有线网络）")
	default:
		add("当前校园网络", detected)
	}
	switch {
	case selected == "未知":
		add("网络一致性", "无法确认（配置不可用）")
	case detectErr != nil:
		add("网络一致性", "无法确认（检测失败）")
	case selected == "auto":
		if detected == "" {
			add("网络一致性", "自动（未检测到校园 Wi-Fi）")
		} else {
			add("网络一致性", "自动（当前 "+detected+"）")
		}
	case detected == "":
		add("网络一致性", "无法确认（未检测到校园 Wi-Fi，可能使用有线网络）")
	case detected == selected:
		add("网络一致性", "一致")
	default:
		addCheck(doctorCheck{key: "网络一致性", value: "不一致（当前 " + detected + "，配置为 " + selected + "）", warn: true})
	}

	var route doctorRoute
	var routeErr error
	if opts.defaultRoute != nil {
		route, routeErr = opts.defaultRoute(netCtx)
	}
	switch {
	case routeErr != nil:
		add("默认路由接口", "未找到（"+routeErr.Error()+"）")
	case route.HardwareAddr == "":
		add("默认路由接口", route.Interface+"（无硬件地址，可能是 TUN/虚拟网卡）")
	default:
		add("默认路由接口", route.Interface+"（硬件地址 "+route.HardwareAddr+"）")
	}

	var proxyNames []string
	for _, name := range []string{"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "NO_PROXY"} {
		if opts.getenv != nil && (strings.TrimSpace(opts.getenv(name)) != "" || strings.TrimSpace(opts.getenv(strings.ToLower(name))) != "") {
			// Only names are printed. Values may embed credentials.
			proxyNames = append(proxyNames, name)
		}
	}
	if len(proxyNames) == 0 {
		add("代理环境变量", "未设置（按设计忽略）")
	} else {
		add("代理环境变量", strings.Join(proxyNames, ", ")+" 已设置（按设计忽略，程序不使用）")
	}

	if opts.resolver != nil {
		dns, err := opts.resolver(netCtx)
		switch {
		case err != nil:
			add("DNS 配置", "无法读取（"+err.Error()+"）")
		case dns.Path == "":
			add("DNS 配置", "未发现解析器配置文件")
		case !dns.Exists:
			value := "未发现 " + dns.Path
			if dns.SystemdResolved {
				value += "（检测到 systemd-resolved）"
			}
			add("DNS 配置", value)
		case dns.Nameserver != "":
			value := dns.Path + " 存在（nameserver " + dns.Nameserver + "）"
			if dns.SystemdResolved {
				value += "；检测到 systemd-resolved"
			}
			add("DNS 配置", value)
		default:
			value := dns.Path + " 存在但未发现 nameserver"
			if dns.SystemdResolved {
				value += "；检测到 systemd-resolved"
			}
			add("DNS 配置", value)
		}
	} else {
		add("DNS 配置", "未检查（当前平台不支持）")
	}

	if addr, err := portalAddress(cfg.Host); err != nil {
		add("门户 TCP 可达性", "无法解析门户地址（仅提示）")
	} else if opts.dialContext != nil {
		conn, dialErr := opts.dialContext(netCtx, "tcp", addr)
		if dialErr != nil {
			add("门户 TCP 可达性", "不可达（"+dialErr.Error()+"；仅提示，TUN 可能本地接受连接）")
		} else {
			if conn != nil {
				_ = conn.Close()
			}
			add("门户 TCP 可达性", "可达（仅提示，不代表已联网或认证成功）")
		}
	} else {
		add("门户 TCP 可达性", "未检查")
	}

	add("版本", opts.version)

	if opts.keepalive == nil {
		add("保活服务", "未检查（当前平台不支持）")
	} else {
		keepCtx, keepCancel := context.WithTimeout(ctx, opts.timeout)
		state, err := opts.keepalive(keepCtx)
		keepCancel()
		if err != nil {
			add("保活服务", "无法查询（"+err.Error()+"）")
		} else {
			add("保活服务", state)
		}
	}

	blocking, warnings := 0, 0
	for _, c := range checks {
		if c.blocking {
			blocking++
		}
		if c.warn {
			warnings++
		}
	}
	switch {
	case blocking > 0:
		add("结论", fmt.Sprintf("未通过（%d 个阻断项，%d 个警告）", blocking, warnings))
	case warnings > 0:
		add("结论", fmt.Sprintf("通过（%d 个警告）", warnings))
	default:
		add("结论", "通过")
	}

	for _, c := range checks {
		fmt.Fprintf(out, "%s: %s\n", c.key, c.value)
	}
	if blocking > 0 {
		return errDoctorBlocking
	}
	return nil
}

// portalAddress accepts only an HTTP(S) origin without embedded credentials.
func portalAddress(host string) (string, error) {
	h := strings.TrimSpace(host)
	if h == "" {
		h = "https://auth.nyist.edu.cn"
	}
	if !strings.Contains(h, "://") {
		h = "https://" + h
	}
	u, err := url.Parse(h)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid portal host")
	}
	port := u.Port()
	if port == "" {
		if u.Scheme == "http" {
			port = "80"
		} else {
			port = "443"
		}
	}
	return net.JoinHostPort(u.Hostname(), port), nil
}
