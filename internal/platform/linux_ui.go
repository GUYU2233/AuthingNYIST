//go:build linux

package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/charmbracelet/huh"
)

// linuxUI owns terminal input only while a form is running. Operations get a
// fresh signal context, so an interrupted request cannot poison the next menu.
type linuxUI struct {
	in  *os.File
	out io.Writer
}

func (ui linuxUI) form(fields ...huh.Field) error {
	return huh.NewForm(huh.NewGroup(fields...)).WithTheme(huh.ThemeCharm()).
		WithInput(ui.in).WithOutput(ui.out).Run()
}

func linuxUIAborted(err error) bool {
	return errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled)
}

func (ui linuxUI) operation(label string, action func(context.Context) error) error {
	fmt.Fprintf(ui.out, "\n%s…（Ctrl+C 退出）\n", label)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	err := action(ctx)
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	if err != nil {
		fmt.Fprintln(ui.out, "操作失败:", err)
	}
	return nil
}

func runLinuxUI(in *os.File, out io.Writer, config, exe string, system bool,
	cfg *app.Config, service *LinuxService,
	save func(context.Context, app.Config) error,
	setAuto func(context.Context, bool) error, report func(app.Status)) error {
	ui := linuxUI{in: in, out: out}
	fmt.Fprintf(out, "\nNYIST · Linux 终端\n配置: %s\n", config)
	for {
		choice := "status"
		err := ui.form(huh.NewSelect[string]().Title("校园网控制台").
			Description("↑/↓ 选择 · Enter 确认 · / 搜索 · Ctrl+C 退出").
			Options(
				huh.NewOption("网络状态", "status"),
				huh.NewOption("账号设置", "config"),
				huh.NewOption("连接校园网", "connect"),
				huh.NewOption("启动后台保活", "background"),
				huh.NewOption("启用自启动", "enable"),
				huh.NewOption("关闭自启动", "disable"),
				huh.NewOption("自启动状态", "service-status"),
				huh.NewOption("停止 systemd 保活", "stop"),
				huh.NewOption("检查更新", "update"),
				huh.NewOption("登出校园网", "logout"),
				huh.NewOption("退出", "exit"),
			).Height(11).Value(&choice))
		if linuxUIAborted(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if choice == "exit" {
			return nil
		}
		if choice == "config" {
			next := *cfg
			err = ui.form(
				huh.NewSelect[string]().Title("网络类型").Description("选择当前校园网类型").Options(huh.NewOption("自动检测", "auto"), huh.NewOption("nyist-tec", "nyist-tec"), huh.NewOption("nyist-stu", "nyist-stu")).Value(&next.Network),
				huh.NewInput().Title("账号").Value(&next.Username).Validate(func(v string) error {
					if strings.TrimSpace(v) == "" {
						return errors.New("账号不能为空")
					}
					return nil
				}),
				huh.NewInput().Title("密码").Description("已有密码可保留").EchoMode(huh.EchoModePassword).Value(&next.Password).Validate(func(v string) error {
					if v == "" {
						return errors.New("密码不能为空")
					}
					return nil
				}),
			)
			if linuxUIAborted(err) {
				return nil
			}
			if err != nil {
				return err
			}
			next.Username = strings.TrimSpace(next.Username)
			err = ui.operation("保存配置", func(ctx context.Context) error {
				if e := linuxCredentials(next); e != nil {
					return e
				}
				if e := save(ctx, next); e != nil {
					return e
				}
				fmt.Fprintln(out, "配置已保存。")
				return nil
			})
		} else {
			if choice == "logout" {
				confirmed := false
				err = ui.form(huh.NewConfirm().Title("确定登出校园网？").Description("连接将中断。若已开启保活，请先停止。").Affirmative("确认登出").Negative("取消").Value(&confirmed))
				if linuxUIAborted(err) {
					return nil
				}
				if err != nil {
					return err
				}
				if !confirmed {
					fmt.Fprintln(out, "已取消登出。")
					continue
				}
			}
			err = ui.operation("执行所选操作", func(ctx context.Context) error {
				switch choice {
				case "connect", "status":
					var s app.Status
					var e error
					if choice == "connect" {
						s, e = app.Connect(ctx, *cfg)
					} else {
						s, e = app.InspectStatus(ctx, *cfg)
					}
					if e == nil {
						report(s)
					}
					return e
				case "logout":
					if e := app.Disconnect(ctx, *cfg); e != nil {
						return e
					}
					fmt.Fprintln(out, "已登出。")
				case "background":
					if e := linuxCredentials(*cfg); e != nil {
						return e
					}
					if e := save(ctx, *cfg); e != nil {
						return e
					}
					pid, e := linuxStartBackground(exe, config)
					if e != nil {
						return e
					}
					fmt.Fprintf(out, "后台 PID: %d；停止命令: kill -TERM %d\n日志: 配置目录下 nyist-daemon.log\n", pid, pid)
				case "enable", "disable":
					linuxServiceNotice(out, system)
					if choice == "enable" {
						if e := linuxCredentials(*cfg); e != nil {
							return e
						}
					}
					if e := setAuto(ctx, choice == "enable"); e != nil {
						return e
					}
					fmt.Fprintln(out, "自启动设置已更新。")
				case "service-status":
					linuxServiceNotice(out, system)
					enabled, e := service.Enabled(ctx)
					if e != nil {
						return e
					}
					fmt.Fprintf(out, "自启动已启用: %t\n", enabled)
				case "stop":
					if _, e := service.ctl(ctx, "stop", "nyist-auth.service"); e != nil {
						return e
					}
					fmt.Fprintln(out, "systemd 保活已停止。")
				case "update":
					return printUpdate(ctx, out)
				}
				return nil
			})
		}
		if linuxUIAborted(err) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
