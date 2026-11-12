package platform

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/version"
	"golang.org/x/term"
)

// DarwinMain implements the macOS CLI. JSON operations reserve stdout solely
// for one JSON value; failures use stderr and exit 1. Credentials never use argv.
func DarwinMain(args []string) int {
	if err := macMain(context.Background(), args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		if !errors.Is(err, errDoctorBlocking) {
			fmt.Fprintln(os.Stderr, "错误:", err)
		}
		return 1
	}
	return 0
}
func macMain(ctx context.Context, args []string, in *os.File, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("nyist", flag.ContinueOnError)
	flags.SetOutput(errOut)
	var path, autostart, network string
	var connect, status, disconnect, daemon, statusJSON, configJSON, saveJSON, configure, system, checkUpdate, showVersion, doctor bool
	flags.StringVar(&path, "config", "", "配置文件路径（密码以0600权限明文保存，未使用Keychain）")
	flags.BoolVar(&connect, "connect", false, "认证连接")
	flags.BoolVar(&status, "status", false, "只读网络状态")
	flags.BoolVar(&disconnect, "disconnect", false, "明确登出；先停止保活进程以免重新认证")
	flags.BoolVar(&daemon, "daemon", false, "前台保活；SIGINT/SIGTERM停止但不登出，不安装launchd服务")
	flags.BoolVar(&configure, "configure", false, "交互配置账号（密码隐藏输入，0600明文保存）")
	flags.BoolVar(&statusJSON, "status-json", false, "stdout仅app.Status JSON，PascalCase字段")
	flags.BoolVar(&configJSON, "config-json", false, "stdout仅app.Config JSON，含明文密码；勿记录日志")
	flags.BoolVar(&saveJSON, "save-config-stdin", false, "从stdin读取一个app.Config JSON，0600原子保存；stdout为空")
	flags.StringVar(&autostart, "autostart", "", "unsupported：本版本不支持launchd自启动")
	flags.BoolVar(&system, "system", false, "unsupported：本版本不支持系统服务")
	flags.StringVar(&network, "network", "", "auto|nyist-tec|nyist-stu；未指定时检测当前校园 Wi-Fi")
	flags.BoolVar(&checkUpdate, "check-update", false, "检查 GitHub 最新稳定 Release，不自动安装")
	flags.BoolVar(&showVersion, "version", false, "显示构建版本")
	flags.BoolVar(&doctor, "doctor", false, "只读诊断配置、网络、DNS 与保活状态（不需要密码）")
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("不支持位置参数；请使用 --connect/--status/--disconnect/--daemon/--configure")
	}
	if autostart != "" || system {
		return errors.New("unsupported: macOS launchd/autostart/system service is not implemented; --daemon runs in the foreground only")
	}
	count := 0
	for _, yes := range []bool{connect, status, disconnect, daemon, statusJSON, configJSON, saveJSON, configure, checkUpdate, showVersion, doctor} {
		if yes {
			count++
		}
	}
	if count > 1 {
		return errors.New("只能选择一个操作参数")
	}
	// Huh owns interactive interrupts; flag operations retain signal cancellation.
	if count > 0 && !configure {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
		defer stop()
	}
	if showVersion {
		fmt.Fprintln(out, version.Version)
		return nil
	}
	if checkUpdate {
		return printUpdate(ctx, out)
	}
	if network != "" && network != "auto" && network != "nyist-tec" && network != "nyist-stu" {
		return errors.New("--network 必须是 auto、nyist-tec 或 nyist-stu")
	}
	var err error
	if path == "" {
		path, err = app.ConfigPath()
		if err != nil {
			return err
		}
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return errors.New("invalid configuration path")
	}
	if doctor {
		return runDoctor(ctx, out, newDoctorOptions(path))
	}
	if saveJSON {
		cfg, err := macDecodeConfig(in)
		if err != nil {
			return err
		}
		return macSaveConfig(path, cfg)
	}
	cfg, err := app.Load(path)
	if err != nil {
		if !errors.Is(err, app.ErrPasswordUnavailable) {
			return err
		}
		// Keep the account and network; the password is empty so nothing can
		// authenticate until the user enters it again.
		fmt.Fprintln(errOut, "已保存的密码无法恢复，请在设置中重新输入。")
	}
	if configJSON {
		return json.NewEncoder(out).Encode(cfg)
	}
	if network != "" {
		cfg.Network = network
	} else {
		cfg.Network = "auto"
	}
	report := func(s app.Status) {
		fmt.Fprintf(out, "网络: %s  在线: %t  账号: %s  IP: %s  %s\n", s.Network, s.Online, s.Username, s.IP, s.Message)
	}
	if status || statusJSON {
		s, e := app.InspectStatus(ctx, cfg)
		if e != nil {
			return e
		}
		if statusJSON {
			return json.NewEncoder(out).Encode(s)
		}
		report(s)
		return nil
	}
	locked := func(action func() error) error {
		release, e := macAcquireLock(path)
		if e != nil {
			return e
		}
		defer release()
		return action()
	}
	if connect {
		return locked(func() error {
			s, e := app.Connect(ctx, cfg)
			if e == nil {
				report(s)
			}
			return e
		})
	}
	if disconnect {
		return locked(func() error { return app.Disconnect(ctx, cfg) })
	}
	if daemon {
		return locked(func() error {
			fmt.Fprintln(out, "前台保活；SIGINT/SIGTERM停止且不登出。未安装launchd服务。")
			err = app.Run(ctx, cfg, report)
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		})
	}
	if !term.IsTerminal(int(in.Fd())) {
		return errors.New("交互操作需要终端；请使用 --save-config-stdin/--config-json/--status-json 等参数")
	}
	return runMacUI(in, out, errOut, path, &cfg, configure, locked, report)
}

func macDecodeConfig(in io.Reader) (app.Config, error) {
	b, err := io.ReadAll(io.LimitReader(in, 1024*1024+1))
	if err != nil || len(b) > 1024*1024 {
		return app.Config{}, errors.New("cannot read configuration JSON (maximum 1 MiB)")
	}
	cfg := app.DefaultConfig()
	decoder := json.NewDecoder(strings.NewReader(string(b)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&cfg); err != nil {
		return app.Config{}, errors.New("invalid configuration JSON")
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		return app.Config{}, errors.New("expected exactly one configuration JSON object")
	}
	if strings.TrimSpace(string(b)) == "null" {
		return app.Config{}, errors.New("configuration must be a JSON object")
	}
	if err = cfg.Validate(); err != nil {
		return app.Config{}, err
	}
	return cfg, nil
}
func macSaveConfig(path string, cfg app.Config) error {
	if cfg.AutoStart {
		return errors.New("unsupported: macOS autostart is not implemented; set autoStart=false")
	}
	return app.Save(path, cfg)
}
