//go:build windows

package main

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/platform"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
)

func main() {
	runtime.LockOSThread()
	defaultPath, err := app.ConfigPath()
	if err != nil {
		fatal(err)
		return
	}
	configPath := flag.String("config", defaultPath, "configuration path")
	flag.Bool("startup", false, "launched at sign-in")
	minimized := flag.Bool("minimized", false, "start hidden in notification area")
	flag.Parse()
	absPath, err := filepath.Abs(*configPath)
	if err != nil {
		fatal(err)
		return
	}
	// One process per Windows session: a second launch raises the existing window.
	mutexName, _ := windows.UTF16PtrFromString(`Local\NYISTConnect`)
	mutex, mutexErr := windows.CreateMutex(nil, false, mutexName)
	if mutexErr == windows.ERROR_ALREADY_EXISTS {
		title, _ := windows.UTF16PtrFromString("NYIST Connect · 校园网助手")
		hwnd := win.FindWindow(nil, title)
		if hwnd != 0 {
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.SetForegroundWindow(hwnd)
		}
		if mutex != 0 {
			windows.CloseHandle(mutex)
		}
		return
	}
	if mutexErr != nil {
		fatal(mutexErr)
		return
	}
	defer windows.CloseHandle(mutex)
	cfg, err := app.Load(absPath)
	if err != nil {
		fatal(fmt.Errorf("读取配置失败: %w", err))
		return
	}
	if enabled, e := platform.WindowsAutoStart(); e == nil {
		cfg.AutoStart = enabled
	}
	ui := &desktop{cfg: cfg, path: absPath}
	if err := ui.create(); err != nil {
		fatal(err)
		return
	}
	defer ui.mw.Dispose()
	defer ui.tray.Dispose()
	defer ui.stop()
	if *minimized || cfg.StartMinimized {
		ui.mw.Hide()
	} else {
		ui.mw.Show()
	}
	// Defer work until the message loop exists, so synchronization is safe.
	go func() {
		time.Sleep(250 * time.Millisecond)
		ui.mw.Synchronize(func() {
			if cfg.AutoConnect && cfg.Username != "" && cfg.Password != "" {
				ui.connect()
			} else {
				ui.check()
			}
		})
	}()
	ui.mw.Run()
}

func fatal(err error) { walk.MsgBox(nil, "NYIST Connect", err.Error(), walk.MsgBoxIconError) }

type desktop struct {
	cfg                                          app.Config
	path                                         string
	mw                                           *walk.MainWindow
	tray                                         *walk.NotifyIcon
	network                                      *walk.ComboBox
	username, password                           *walk.LineEdit
	autoStart, startMin, background, autoConnect *walk.CheckBox
	status                                       *walk.TextLabel
	connectButton, disconnectButton, checkButton *walk.PushButton
	cancel                                       context.CancelFunc
	generation                                   int
	wg                                           sync.WaitGroup
	exiting                                      bool
}

func (d *desktop) create() error {
	index := 0
	if d.cfg.Network == "nyist-stu" {
		index = 1
	}
	err := (MainWindow{
		AssignTo: &d.mw, Title: "NYIST Connect · 校园网助手", Size: Size{Width: 480, Height: 530}, MinSize: Size{Width: 460, Height: 510},
		Font:   Font{Family: "Microsoft YaHei UI", PointSize: 10},
		Layout: VBox{Margins: Margins{Left: 20, Top: 16, Right: 20, Bottom: 16}, Spacing: 12},
		Children: []Widget{
			Label{Text: "校园网助手", Font: Font{Family: "Microsoft YaHei UI", PointSize: 17, Bold: true}},
			Label{Text: "选择网络，保存账号，一键连接。"},
			Composite{Layout: Grid{Columns: 2, Spacing: 10}, Children: []Widget{
				Label{Text: "校园网络"}, ComboBox{AssignTo: &d.network, Model: []string{"nyist-tec", "nyist-stu"}, CurrentIndex: index},
				Label{Text: "账号"}, LineEdit{AssignTo: &d.username, Text: d.cfg.Username},
				Label{Text: "密码"}, LineEdit{AssignTo: &d.password, Text: d.cfg.Password, PasswordMode: true},
			}},
			GroupBox{Title: "启动与后台", Layout: VBox{Spacing: 7}, Children: []Widget{
				CheckBox{AssignTo: &d.autoStart, Text: "登录 Windows 时自动启动", Checked: d.cfg.AutoStart},
				CheckBox{AssignTo: &d.startMin, Text: "启动时最小化到托盘", Checked: d.cfg.StartMinimized},
				CheckBox{AssignTo: &d.background, Text: "后台运行（关闭窗口时保留托盘）", Checked: d.cfg.Background},
				CheckBox{AssignTo: &d.autoConnect, Text: "启动后自动连接并保持在线", Checked: d.cfg.AutoConnect},
			}},
			Composite{Layout: HBox{}, Children: []Widget{
				PushButton{Text: "保存设置", OnClicked: func() { d.save(true) }},
				PushButton{AssignTo: &d.connectButton, Text: "连接", OnClicked: d.connect},
				PushButton{AssignTo: &d.checkButton, Text: "检查状态", OnClicked: d.check},
				PushButton{AssignTo: &d.disconnectButton, Text: "断开认证", OnClicked: d.disconnect},
			}},
			TextLabel{AssignTo: &d.status, Text: "就绪", MinSize: Size{Width: 0, Height: 52}},
			Label{Text: "密码使用 Windows DPAPI 加密；退出应用不会注销校园网。", Font: Font{PointSize: 8}},
		},
	}).Create()
	if err != nil {
		return err
	}
	icon, err := walk.NewIconFromResourceId(2)
	if err != nil {
		icon = walk.IconInformation()
		err = nil
	}
	if err != nil {
		return err
	}
	d.mw.SetIcon(icon)
	d.tray, err = walk.NewNotifyIcon(d.mw)
	if err != nil {
		return err
	}
	if err = d.tray.SetIcon(icon); err != nil {
		return err
	}
	d.tray.SetToolTip("NYIST Connect · 校园网助手")
	open := walk.NewAction()
	open.SetText("打开窗口")
	open.Triggered().Attach(d.show)
	if err = d.tray.ContextMenu().Actions().Add(open); err != nil {
		return err
	}
	quit := walk.NewAction()
	quit.SetText("退出")
	quit.Triggered().Attach(func() { d.exiting = true; d.stop(); d.mw.Close() })
	if err = d.tray.ContextMenu().Actions().Add(quit); err != nil {
		return err
	}
	d.tray.MouseDown().Attach(func(x, y int, b walk.MouseButton) {
		if b == walk.LeftButton {
			d.show()
		}
	})
	if err = d.tray.SetVisible(true); err != nil {
		return fmt.Errorf("无法创建托盘图标: %w", err)
	}
	d.mw.Closing().Attach(func(cancel *bool, reason walk.CloseReason) {
		if !d.exiting && d.background.Checked() {
			*cancel = true
			d.mw.Hide()
		} else {
			d.exiting = true
			d.stop()
		}
	})
	return nil
}

func (d *desktop) show() {
	d.mw.Show()
	win.ShowWindow(d.mw.Handle(), win.SW_RESTORE)
	win.SetForegroundWindow(d.mw.Handle())
}

func (d *desktop) collect() app.Config {
	c := d.cfg
	c.Network = d.network.Text()
	c.Username = d.username.Text()
	c.Password = d.password.Text()
	c.AutoStart = d.autoStart.Checked()
	c.StartMinimized = d.startMin.Checked()
	c.Background = d.background.Checked()
	c.AutoConnect = d.autoConnect.Checked()
	return c
}

func (d *desktop) save(notify bool) bool {
	c := d.collect()
	if err := c.Validate(); err != nil {
		d.setStatus("设置无效: " + err.Error())
		return false
	}
	previous := d.cfg
	if err := app.Save(d.path, c); err != nil {
		d.setStatus("保存失败: " + err.Error())
		return false
	}
	if err := platform.SetWindowsAutoStart(c.AutoStart, d.path); err != nil {
		_ = app.Save(d.path, previous)
		d.setStatus("自启动设置失败: " + err.Error())
		return false
	}
	d.cfg = c
	if notify {
		d.setStatus("设置已保存。启动行为在下次启动时生效。")
	}
	return true
}

func (d *desktop) setStatus(s string) { d.status.SetText(s) }
func statusText(s app.Status) string {
	message := s.Message
	switch message {
	case "Connected":
		message = "已连接"
	case "Disconnected":
		message = "未连接"
	case "Another account is online":
		message = "当前在线账号与所填账号不同"
	}
	if s.Username != "" {
		message += " · " + s.Username
	}
	if s.IP != "" {
		message += " · " + s.IP
	}
	return message
}
func (d *desktop) stop() {
	d.generation++
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
}

func (d *desktop) work(task func(context.Context, app.Config, func(app.Status)) error) {
	d.stop()
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	generation := d.generation
	cfg := d.cfg
	d.connectButton.SetEnabled(false)
	d.disconnectButton.SetEnabled(false)
	d.checkButton.SetEnabled(false)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		report := func(s app.Status) {
			if ctx.Err() != nil {
				return
			}
			d.mw.Synchronize(func() {
				if !d.exiting && generation == d.generation {
					d.setStatus(statusText(s))
				}
			})
		}
		err := task(ctx, cfg, report)
		if ctx.Err() != nil {
			return
		}
		d.mw.Synchronize(func() {
			if d.exiting || generation != d.generation {
				return
			}
			d.connectButton.SetEnabled(true)
			d.disconnectButton.SetEnabled(true)
			d.checkButton.SetEnabled(true)
			if err != nil {
				d.setStatus("操作失败: " + err.Error())
			}
		})
	}()
}

func (d *desktop) connect() {
	if !d.save(false) {
		return
	}
	d.setStatus("正在连接 " + d.cfg.Network + " …")
	d.work(func(ctx context.Context, c app.Config, report func(app.Status)) error {
		s, err := app.Connect(ctx, c)
		if err != nil {
			return err
		}
		report(s)
		if c.AutoConnect {
			// Keep controls usable while the background monitor runs.
			d.mw.Synchronize(func() {
				d.connectButton.SetEnabled(true)
				d.disconnectButton.SetEnabled(true)
				d.checkButton.SetEnabled(true)
			})
			return app.Run(ctx, c, report)
		}
		return nil
	})
}
func (d *desktop) check() {
	cfg := d.collect()
	d.setStatus("正在检查在线状态 …")
	d.checkButton.SetEnabled(false)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		s, err := app.Check(ctx, cfg)
		d.mw.Synchronize(func() {
			if d.exiting {
				return
			}
			d.checkButton.SetEnabled(true)
			if err != nil {
				d.setStatus("状态检查失败: " + err.Error())
			} else {
				d.setStatus(statusText(s))
			}
		})
	}()
}
func (d *desktop) disconnect() {
	if walk.MsgBox(d.mw, "断开认证", "确定注销本机校园网认证？这会中断本机网络。", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	d.cfg = d.collect()
	d.setStatus("正在注销 …")
	d.work(func(ctx context.Context, c app.Config, report func(app.Status)) error {
		e := app.Disconnect(ctx, c)
		if e == nil {
			report(app.Status{Message: "已注销，自动重连已停止。"})
		}
		return e
	})
}
