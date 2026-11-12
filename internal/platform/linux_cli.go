//go:build linux
// +build linux

package platform

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/version"
	"golang.org/x/term"
)

// LinuxMain is the Linux-only terminal entry point. It returns an exit code.
func LinuxMain(args []string) int {
	if err := linuxMain(args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		// The doctor report already explains blocking checks; only the exit
		// code should signal failure.
		if !errors.Is(err, errDoctorBlocking) {
			fmt.Fprintln(os.Stderr, "错误:", err)
		}
		return 1
	}
	return 0
}
func linuxMain(args []string, in *os.File, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("nyist", flag.ContinueOnError)
	flags.SetOutput(errOut)
	var config, autostart, network string
	var daemon, connect, status, disconnect, system, statusJSON, checkUpdate, showVersion, doctor, trayMode bool
	flags.StringVar(&config, "config", "", "配置文件路径（转换为绝对路径）")
	flags.BoolVar(&daemon, "daemon", false, "前台运行保活守护进程（供 systemd 使用）")
	flags.BoolVar(&connect, "connect", false, "认证连接")
	flags.BoolVar(&status, "status", false, "查询网络状态")
	flags.BoolVar(&disconnect, "disconnect", false, "明确登出校园网（将中断连接）")
	flags.StringVar(&autostart, "autostart", "", "enable|disable|status，自启动但不立即连接/登出")
	flags.BoolVar(&system, "system", false, "显式使用 root system 服务，未登录也可启动")
	flags.StringVar(&network, "network", "", "auto|nyist-tec|nyist-stu；默认只读自动检测，不主动切换 Wi-Fi")
	flags.BoolVar(&statusJSON, "status-json", false, "只读状态 JSON，不需要密码")
	flags.BoolVar(&checkUpdate, "check-update", false, "检查 GitHub 最新稳定 Release（不自动安装）")
	flags.BoolVar(&showVersion, "version", false, "显示构建版本")
	flags.BoolVar(&doctor, "doctor", false, "只读诊断配置、网络、DNS 与保活状态（不需要密码）")
	flags.BoolVar(&trayMode, "tray", false, "显示托盘图标并保活（需要托盘宿主）")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("不支持的位置参数")
	}
	count := 0
	for _, v := range []bool{daemon, connect, status, disconnect, autostart != "", statusJSON, checkUpdate, showVersion, doctor, trayMode} {
		if v {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("操作参数只能选择一个（--daemon/--connect/--status/--status-json/--disconnect/--autostart/--check-update/--version/--doctor/--tray）")
	}
	if autostart != "" && autostart != "enable" && autostart != "disable" && autostart != "status" {
		return fmt.Errorf("--autostart 必须是 enable、disable 或 status")
	}
	if showVersion {
		fmt.Fprintln(out, version.Version)
		return nil
	}
	if checkUpdate {
		return printUpdate(context.Background(), out)
	}
	if network != "" && network != "auto" && network != "nyist-tec" && network != "nyist-stu" {
		return fmt.Errorf("--network 必须是 auto、nyist-tec 或 nyist-stu")
	}
	var err error
	if config == "" {
		config, err = app.ConfigPath()
		if err != nil {
			return err
		}
	}
	config, err = filepath.Abs(config)
	if err != nil {
		return err
	}
	if doctor {
		opts := newDoctorOptions(config)
		opts.keepalive = func(ctx context.Context) (string, error) {
			return linuxDoctorKeepalive(ctx, config, system)
		}
		return runDoctor(context.Background(), out, opts)
	}
	cfg, err := app.Load(config)
	if err != nil {
		if !os.IsNotExist(err) {
			if !errors.Is(err, app.ErrPasswordUnavailable) {
				return err
			}
			// The rest of the configuration is still valid: keep the account and
			// network, leave the password empty, and let the user re-enter it.
			fmt.Fprintln(out, "已保存的密码无法恢复，请在账号设置中重新输入。")
		} else {
			cfg = app.DefaultConfig()
		}
	}
	if network != "" {
		cfg.Network = network
	} else {
		cfg.Network = "auto"
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	service, err := NewLinuxService(exe, config, system)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	report := func(s app.Status) {
		fmt.Fprintf(out, "网络: %s  在线: %t  账号: %s  IP: %s  %s\n", s.Network, s.Online, s.Username, s.IP, s.Message)
	}
	save := func(ctx context.Context, c app.Config) error {
		enabled, e := service.Enabled(ctx)
		if e != nil {
			if errors.Is(e, exec.ErrNotFound) {
				fmt.Fprintln(out, "警告：找不到 systemctl，无法使用系统自启动；配置仍会保存。", e)
				enabled = false
			} else {
				return e
			}
		}
		c.AutoStart = enabled
		if e = app.Save(config, c); e != nil {
			return e
		}
		cfg = c
		return nil
	}
	setAuto := func(ctx context.Context, enable bool) error {
		return service.SetEnabled(ctx, enable, func(actual bool) error {
			next := cfg
			next.AutoStart = actual
			if e := app.Save(config, next); e != nil {
				return e
			}
			cfg = next
			return nil
		})
	}
	if autostart != "" {
		linuxServiceNotice(out, system)
		if autostart == "status" {
			enabled, e := service.Enabled(ctx)
			if e != nil {
				return e
			}
			fmt.Fprintf(out, "自启动已启用: %t\n", enabled)
			return nil
		}
		if autostart == "enable" {
			if err = linuxCredentials(cfg); err != nil {
				return fmt.Errorf("请先在菜单配置账号: %w", err)
			}
		}
		return setAuto(ctx, autostart == "enable")
	}
	if status || statusJSON {
		s, e := app.InspectStatus(ctx, cfg)
		if e == nil && statusJSON {
			return json.NewEncoder(out).Encode(s)
		}
		if e == nil {
			report(s)
		}
		return e
	}
	if disconnect {
		fmt.Fprintln(out, "正在执行明确请求的校园网登出；连接将中断。若有保活守护进程，请先停止，否则可能重新认证。")
		return app.Disconnect(ctx, cfg)
	}
	if connect {
		s, e := app.Connect(ctx, cfg)
		if e == nil {
			report(s)
		}
		return e
	}
	if daemon {
		fmt.Fprintln(out, "保活守护中；SIGINT/SIGTERM 停止进程，不主动登出。")
		err = app.Run(ctx, cfg, report, linuxNotifyOptions())
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if trayMode {
		if e := linuxCredentials(cfg); e != nil {
			return e
		}
		return runLinuxTray(ctx, cfg, config, out)
	}
	if !term.IsTerminal(int(in.Fd())) {
		return fmt.Errorf("交互菜单需要终端；脚本请使用 --connect/--status/--daemon 等参数")
	}
	// Interactive input blocks independently of ctx. Do not swallow SIGINT and
	// leave the menu alive with a permanently cancelled operation context.
	// Restore the default signal action: Ctrl+C exits without logging out.
	stop()
	ctx = context.Background()
	return runLinuxUI(in, out, config, exe, system, &cfg, service, save, setAuto, report)
}

func linuxCredentials(cfg app.Config) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Username) == "" || cfg.Password == "" {
		return fmt.Errorf("请先配置非空账号和密码")
	}
	return nil
}

func linuxServiceNotice(out io.Writer, system bool) {
	if system {
		fmt.Fprintln(out, "使用 root system 服务：系统开机即可运行，不依赖用户登录。")
	} else {
		fmt.Fprintln(out, "默认 systemd --user 自启动依赖用户登录。未登录启动可由管理员配置 loginctl enable-linger 用户名，或显式使用 root --system；本程序绝不自动修改 linger。")
	}
}
func linuxStartBackground(executable, config string) (int, error) {
	if !filepath.IsAbs(executable) || !filepath.IsAbs(config) {
		return 0, fmt.Errorf("后台进程要求绝对路径")
	}
	log, err := openDaemonLog(filepath.Join(filepath.Dir(config), "nyist-daemon.log"))
	if err != nil {
		return 0, err
	}
	defer log.Close()
	cmd := exec.Command(executable, "--config", config, "--daemon")
	cmd.Stdin = nil
	cmd.Stdout = log
	cmd.Stderr = log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err = cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	return pid, cmd.Process.Release()
}
