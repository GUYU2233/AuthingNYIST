//go:build linux
// +build linux

package platform

import (
	"bufio"
	"context"
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
	"golang.org/x/term"
)

// LinuxMain is the Linux-only terminal entry point. It returns an exit code.
func LinuxMain(args []string) int {
	if err := linuxMain(args, os.Stdin, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "错误:", err)
		return 1
	}
	return 0
}
func linuxMain(args []string, in *os.File, out, errOut io.Writer) error {
	flags := flag.NewFlagSet("nyist", flag.ContinueOnError)
	flags.SetOutput(errOut)
	var config, autostart string
	var daemon, connect, status, disconnect, system bool
	flags.StringVar(&config, "config", "", "配置文件路径（转换为绝对路径）")
	flags.BoolVar(&daemon, "daemon", false, "前台运行保活守护进程（供 systemd 使用）")
	flags.BoolVar(&connect, "connect", false, "认证连接")
	flags.BoolVar(&status, "status", false, "查询网络状态")
	flags.BoolVar(&disconnect, "disconnect", false, "明确登出校园网（将中断连接）")
	flags.StringVar(&autostart, "autostart", "", "enable|disable|status，自启动但不立即连接/登出")
	flags.BoolVar(&system, "system", false, "显式使用 root system 服务，未登录也可启动")
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
	for _, v := range []bool{daemon, connect, status, disconnect, autostart != ""} {
		if v {
			count++
		}
	}
	if count > 1 {
		return fmt.Errorf("--daemon/--connect/--status/--disconnect/--autostart 只能选择一个")
	}
	if autostart != "" && autostart != "enable" && autostart != "disable" && autostart != "status" {
		return fmt.Errorf("--autostart 必须是 enable、disable 或 status")
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
	cfg, err := app.Load(config)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		cfg = app.DefaultConfig()
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
		fmt.Fprintf(out, "在线: %t  账号: %s  IP: %s  %s\n", s.Online, s.Username, s.IP, s.Message)
	}
	save := func(c app.Config) error {
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
	setAuto := func(enable bool) error {
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
		return setAuto(autostart == "enable")
	}
	if status {
		s, e := app.Check(ctx, cfg)
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
		err = app.Run(ctx, cfg, report)
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
	if !term.IsTerminal(int(in.Fd())) {
		return fmt.Errorf("交互菜单需要终端；脚本请使用 --connect/--status/--daemon 等参数")
	}
	reader := bufio.NewReader(in)
	line := func(prompt string) (string, error) {
		fmt.Fprint(out, prompt)
		text, e := reader.ReadString('\n')
		return strings.TrimSpace(text), e
	}
	fmt.Fprintf(out, "NYIST Linux 终端\n配置: %s\n密码保存在本地配置，请保护文件；终端输入不回显。\n", config)
	for {
		fmt.Fprintln(out, "\n1 配置账号（nyist-tec/nyist-stu）  2 认证  3 状态\n4 登出（将中断连接）  5 启动后台保活  6 添加自启动\n7 移除自启动  8 自启动状态  9 停止 systemd 保活  0 退出")
		choice, e := line("选择: ")
		if e != nil {
			if errors.Is(e, io.EOF) {
				return nil
			}
			return e
		}
		switch choice {
		case "0":
			return nil
		case "1":
			next := cfg
			n, e := line("网络 [1 nyist-tec / 2 nyist-stu，默认 1]: ")
			if e != nil {
				return e
			}
			switch n {
			case "", "1", "nyist-tec":
				next.Network = "nyist-tec"
			case "2", "nyist-stu":
				next.Network = "nyist-stu"
			default:
				fmt.Fprintln(out, "网络选择无效")
				continue
			}
			next.Username, e = line("账号: ")
			if e != nil {
				return e
			}
			fmt.Fprint(out, "密码（隐藏）: ")
			password, e := term.ReadPassword(int(in.Fd()))
			fmt.Fprintln(out)
			if e != nil {
				return e
			}
			next.Password = string(password)
			for i := range password {
				password[i] = 0
			}
			if e = linuxCredentials(next); e == nil {
				e = save(next)
			}
			if e != nil {
				fmt.Fprintln(out, "配置未保存:", e)
			} else {
				fmt.Fprintln(out, "配置已保存。")
			}
		case "2":
			s, e := app.Connect(ctx, cfg)
			if e != nil {
				fmt.Fprintln(out, e)
			} else {
				report(s)
			}
		case "3":
			s, e := app.Check(ctx, cfg)
			if e != nil {
				fmt.Fprintln(out, e)
			} else {
				report(s)
			}
		case "4":
			answer, e := line("警告：登出会中断校园网。请先停止保活进程，否则会重新认证。输入 LOGOUT 确认: ")
			if e != nil {
				return e
			}
			if answer == "LOGOUT" {
				if e = app.Disconnect(ctx, cfg); e != nil {
					fmt.Fprintln(out, e)
				} else {
					fmt.Fprintln(out, "已登出。")
				}
			} else {
				fmt.Fprintln(out, "已取消登出。")
			}
		case "5":
			if e = linuxCredentials(cfg); e == nil {
				e = save(cfg)
			}
			if e == nil {
				var pid int
				pid, e = linuxStartBackground(exe, config)
				if e == nil {
					fmt.Fprintf(out, "后台 PID: %d；停止命令: kill -TERM %d\n", pid, pid)
				}
			}
			if e != nil {
				fmt.Fprintln(out, e)
			} else {
				fmt.Fprintln(out, "后台进程已启动（不等于自启动）；日志位于配置目录 nyist-daemon.log。停止请向该进程发送 SIGTERM；退出菜单不主动登出。")
			}
		case "6", "7":
			linuxServiceNotice(out, system)
			if choice == "6" {
				if e = linuxCredentials(cfg); e != nil {
					fmt.Fprintln(out, e)
					continue
				}
			}
			if e = setAuto(choice == "6"); e != nil {
				fmt.Fprintln(out, e)
			} else {
				fmt.Fprintln(out, "自启动设置已更新；不会立即启动/停止进程。")
			}
		case "9":
			if _, e = service.ctl(ctx, "stop", "nyist-auth.service"); e != nil {
				fmt.Fprintln(out, e)
			} else {
				fmt.Fprintln(out, "systemd 保活已停止（未登出）；手动后台保活请按其启动提示的 PID 发送 SIGTERM。")
			}
		case "8":
			linuxServiceNotice(out, system)
			enabled, e := service.Enabled(ctx)
			if e != nil {
				fmt.Fprintln(out, e)
			} else {
				fmt.Fprintf(out, "自启动已启用: %t\n", enabled)
			}
		default:
			fmt.Fprintln(out, "无效选择。")
		}
	}
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
	log, err := os.OpenFile(filepath.Join(filepath.Dir(config), "nyist-daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
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
