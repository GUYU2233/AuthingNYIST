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

// macUI releases terminal ownership before starting network operations.
type macUI struct {
	in  *os.File
	out io.Writer
}

func (ui macUI) form(fields ...huh.Field) error {
	return huh.NewForm(huh.NewGroup(fields...)).WithTheme(huh.ThemeCharm()).WithInput(ui.in).WithOutput(ui.out).Run()
}

func macUIAborted(err error) bool {
	return errors.Is(err, huh.ErrUserAborted) || errors.Is(err, context.Canceled)
}

// Every operation owns a fresh signal context. Foreground keepalive intentionally
// has no deadline; ordinary requests cannot hold the console indefinitely.
func (ui macUI) operation(label string, keepalive bool, action func(context.Context) error) error {
	fmt.Fprintf(ui.out, "\n%s…（Ctrl+C 退出）\n", label)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !keepalive {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 45*time.Second)
		defer cancel()
	}
	err := action(ctx)
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	return err
}

func (ui macUI) configure(path string, cfg *app.Config) error {
	next := *cfg
	err := ui.form(
		huh.NewSelect[string]().Title("网络类型").Options(huh.NewOption("自动检测", "auto"), huh.NewOption("nyist-tec", "nyist-tec"), huh.NewOption("nyist-stu", "nyist-stu")).Value(&next.Network),
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
	if err != nil {
		return err
	}
	next.Username = strings.TrimSpace(next.Username)
	if err := macSaveConfig(path, next); err != nil {
		return err
	}
	*cfg = next
	fmt.Fprintln(ui.out, "配置已保存。")
	return nil
}

func runMacUI(in *os.File, out, errOut io.Writer, path string, cfg *app.Config, configure bool, locked func(func() error) error, report func(app.Status)) error {
	ui := macUI{in: in, out: out}
	fmt.Fprintf(out, "\nNYIST · macOS 终端\n配置: %s\n", path)
	if configure {
		err := ui.configure(path, cfg)
		if macUIAborted(err) {
			return nil
		}
		return err
	}
	for {
		choice := "status"
		err := ui.form(huh.NewSelect[string]().Title("校园网控制台").Description("↑/↓ 选择 · Enter 确认 · / 搜索 · Ctrl+C 退出").Options(
			huh.NewOption("网络状态", "status"),
			huh.NewOption("账号设置", "config"),
			huh.NewOption("连接校园网", "connect"),
			huh.NewOption("前台保活", "daemon"),
			huh.NewOption("检查更新", "update"),
			huh.NewOption("登出校园网", "logout"),
			huh.NewOption("退出", "exit"),
		).Height(7).Value(&choice))
		if macUIAborted(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if choice == "exit" {
			return nil
		}
		if choice == "config" {
			err = ui.configure(path, cfg)
		} else {
			if choice == "logout" {
				confirmed := false
				err = ui.form(huh.NewConfirm().Title("确定登出校园网？").Description("连接将中断，请先停止保活。").Affirmative("确认登出").Negative("取消").Value(&confirmed))
				if macUIAborted(err) {
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
			labels := map[string]string{"status": "查询网络状态", "connect": "连接校园网", "daemon": "前台保活", "update": "检查更新", "logout": "登出校园网"}
			err = ui.operation(labels[choice], choice == "daemon", func(ctx context.Context) error {
				switch choice {
				case "status", "connect":
					var s app.Status
					var e error
					if choice == "connect" {
						e = locked(func() error { s, e = app.Connect(ctx, *cfg); return e })
					} else {
						s, e = app.InspectStatus(ctx, *cfg)
					}
					if e == nil {
						report(s)
					}
					return e
				case "daemon":
					return locked(func() error { return app.Run(ctx, *cfg, report) })
				case "logout":
					if e := locked(func() error { return app.Disconnect(ctx, *cfg) }); e != nil {
						return e
					}
					fmt.Fprintln(out, "已登出。")
				case "update":
					return printUpdate(ctx, out)
				}
				return nil
			})
		}
		if macUIAborted(err) {
			return nil
		}
		if err != nil {
			fmt.Fprintln(errOut, "错误:", err)
		}
	}
}
