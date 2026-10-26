package platform

import (
	"bufio"
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
	"golang.org/x/term"
)

// DarwinMain implements the macOS CLI. JSON operations reserve stdout solely
// for one JSON value; failures use stderr and exit 1. Credentials never use argv.
func DarwinMain(args []string) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := macMain(ctx, args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 1
	}
	return 0
}
func macMain(ctx context.Context, args []string, in *os.File, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("nyist", flag.ContinueOnError)
	flags.SetOutput(errOut)
	var path, autostart string
	var connect, status, disconnect, daemon, statusJSON, configJSON, saveJSON, configure, system bool
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
	for _, yes := range []bool{connect, status, disconnect, daemon, statusJSON, configJSON, saveJSON, configure} {
		if yes {
			count++
		}
	}
	if count > 1 {
		return errors.New("只能选择一个操作参数")
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
	if saveJSON {
		cfg, err := macDecodeConfig(in)
		if err != nil {
			return err
		}
		return macSaveConfig(path, cfg)
	}
	cfg, err := app.Load(path)
	if err != nil {
		return err
	}
	if configJSON {
		return json.NewEncoder(out).Encode(cfg)
	}
	report := func(s app.Status) {
		fmt.Fprintf(out, "在线: %t  账号: %s  IP: %s  %s\n", s.Online, s.Username, s.IP, s.Message)
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
	fmt.Fprintf(out, "NYIST macOS 终端\n配置: %s\n警告：密码以0600权限明文保存，未使用Keychain。launchd自启动/系统服务暂不支持。\n", path)
	// A fresh one-byte line reader avoids buffering password bytes ahead of the
	// terminal's no-echo ReadPassword call.
	line := func(prompt string) (string, error) { return macReadLine(in, out, prompt) }
	editConfig := func() error {
		next := cfg
		n, e := line("网络 [1 nyist-tec / 2 nyist-stu，默认1]: ")
		if e != nil {
			return e
		}
		switch n {
		case "", "1", "nyist-tec":
			next.Network = "nyist-tec"
		case "2", "nyist-stu":
			next.Network = "nyist-stu"
		default:
			return errors.New("网络选择无效")
		}
		next.Username, e = line("账号: ")
		if e != nil {
			return e
		}
		fmt.Fprint(out, "密码（隐藏）: ")
		p, e := term.ReadPassword(int(in.Fd()))
		fmt.Fprintln(out)
		if e != nil {
			return errors.New("cannot read terminal password")
		}
		next.Password = string(p)
		for i := range p {
			p[i] = 0
		}
		if e = macSaveConfig(path, next); e != nil {
			return e
		}
		cfg = next
		fmt.Fprintln(out, "配置已保存（0600明文；未使用Keychain）。")
		return nil
	}
	if configure {
		return editConfig()
	}
	for {
		fmt.Fprintln(out, "\n1 配置账号  2 认证  3 状态  4 登出  5 前台保活  0 退出")
		choice, e := line("选择: ")
		if errors.Is(e, io.EOF) {
			return nil
		}
		if e != nil {
			return e
		}
		switch choice {
		case "0":
			return nil
		case "1":
			err = editConfig()
		case "2":
			var s app.Status
			err = locked(func() error { var e error; s, e = app.Connect(ctx, cfg); return e })
			if err == nil {
				report(s)
			}
		case "3":
			var s app.Status
			s, err = app.InspectStatus(ctx, cfg)
			if err == nil {
				report(s)
			}
		case "4":
			answer, e := line("登出将中断连接；请先停止保活。输入 LOGOUT 确认: ")
			if e != nil {
				return e
			}
			if answer == "LOGOUT" {
				err = locked(func() error { return app.Disconnect(ctx, cfg) })
			} else {
				fmt.Fprintln(out, "已取消登出。")
			}
		case "5":
			fmt.Fprintln(out, "前台保活；SIGINT/SIGTERM退出，不登出。")
			err = locked(func() error { return app.Run(ctx, cfg, report) })
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		default:
			fmt.Fprintln(out, "无效选择。")
		}
		if err != nil {
			fmt.Fprintln(errOut, "错误:", err)
			err = nil
		}
	}
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
func macReadLine(in io.Reader, out io.Writer, prompt string) (string, error) {
	fmt.Fprint(out, prompt)
	// bufio.Reader is intentionally limited to a reader returning one byte.
	reader := bufio.NewReaderSize(&macByteReader{in}, 16)
	s, err := reader.ReadString('\n')
	return strings.TrimSpace(s), err
}

type macByteReader struct{ io.Reader }

func (r *macByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}
