//go:build windows

package main

import (
	"bytes"
	"context"
	_ "embed"
	"flag"
	"fmt"
	"image/png"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/bootservice"
	"github.com/Palvef/AuthingNYIST/internal/platform"
	"github.com/lxn/walk"
	. "github.com/lxn/walk/declarative"
	"github.com/lxn/win"
	"golang.org/x/sys/windows"
	"os"
)

func main() {
	if handled, err := bootservice.HandleCommandLine(); handled {
		if err != nil {
			os.Exit(1)
		}
		return
	}
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
	serviceEnabled, serviceErr := bootservice.Installed()
	// A local config is not evidence of the configuration deployed to the service.
	// Keep the UI available to retry SCM queries, but fail closed on local work.
	ui := &desktop{cfg: cfg, path: absPath, serviceEnabled: serviceEnabled, serviceStateKnown: serviceErr == nil}
	if err := ui.create(); err != nil {
		fatal(err)
		return
	}
	defer ui.mw.Dispose()
	defer ui.tray.Dispose()
	// Synchronize only enqueues callbacks, so workers can finish after the
	// message loop stops without blocking on UI execution. Join before Dispose.
	defer ui.wg.Wait()
	defer ui.stop()
	if *minimized || cfg.StartMinimized {
		ui.mw.Hide()
	} else {
		ui.mw.Show()
	}
	// Defer work until the message loop exists, so synchronization is safe.
	ui.wg.Add(1)
	go func() {
		defer ui.wg.Done()
		time.Sleep(250 * time.Millisecond)
		ui.mw.Synchronize(func() {
			if ui.exiting || ui.busy {
				return
			}
			if ui.serviceStateKnown && !ui.serviceEnabled && cfg.AutoConnect && cfg.Username != "" && cfg.Password != "" {
				// Automatic startup uses only saved settings; never invoke UAC.
				ui.connectSaved()
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
	priorityStart                                *walk.CheckBox
	serviceEnabled                               bool
	serviceConfig                                app.Config
	serviceConfigKnown                           bool
	serviceStateKnown                            bool
	serviceRecoveryRequired                      bool
	busy                                         bool
	saveButton                                   *walk.PushButton
	quitAction                                   *walk.Action
	activeWorkers                                atomic.Int32
}

//go:embed assets/bg.png
var footerBackground []byte

func (d *desktop) create() error {
	im, err := png.Decode(bytes.NewReader(footerBackground))
	if err != nil {
		return fmt.Errorf("读取背景图片: %w", err)
	}
	bitmap, err := walk.NewBitmapFromImage(im)
	if err != nil {
		return err
	}
	// Keep the full image on the client background, not in a cropped footer.
	var backgroundBitmap *walk.Bitmap
	var backgroundBrush *walk.BitmapBrush
	index := 0
	if d.cfg.Network == "nyist-stu" {
		index = 1
	}
	err = (MainWindow{
		AssignTo: &d.mw, Title: "NYIST Connect · 校园网助手", Size: Size{Width: 420, Height: 490}, MinSize: Size{Width: 420, Height: 490},
		Font:   Font{Family: "Microsoft YaHei UI", PointSize: 9},
		Layout: VBox{MarginsZero: true, SpacingZero: true},
		Children: []Widget{
			Composite{Background: TransparentBrush{}, Layout: VBox{Margins: Margins{Left: 12, Top: 10, Right: 12, Bottom: 8}, Spacing: 6}, Children: []Widget{
				Label{Text: "校园网助手", Font: Font{Family: "Microsoft YaHei UI", PointSize: 14, Bold: true}},
				Label{Text: "选择网络，保存账号，一键连接。"},
				Composite{Background: TransparentBrush{}, Layout: Grid{Columns: 2, Spacing: 6}, Children: []Widget{
					Label{Text: "校园网络"}, ComboBox{AssignTo: &d.network, Model: []string{"nyist-tec", "nyist-stu"}, CurrentIndex: index},
					Label{Text: "账号"}, LineEdit{AssignTo: &d.username, Text: d.cfg.Username},
					Label{Text: "密码"}, LineEdit{AssignTo: &d.password, Text: d.cfg.Password, PasswordMode: true},
				}},
				GroupBox{Title: "启动与后台", Background: TransparentBrush{}, Layout: VBox{Margins: Margins{Left: 10, Top: 8, Right: 10, Bottom: 8}, Spacing: 5, Alignment: AlignHNearVNear}, Children: []Widget{
					CheckBox{AssignTo: &d.autoStart, Text: "登录系统时自动启动", ToolTipText: "登录当前 Windows 账号时打开校园网助手；是否自动认证由下方选项控制。", Checked: d.cfg.AutoStart},
					CheckBox{AssignTo: &d.startMin, Text: "启动时最小化到托盘", ToolTipText: "启动后不显示主窗口；点击托盘图标可重新打开。", Checked: d.cfg.StartMinimized},
					CheckBox{AssignTo: &d.background, Text: "后台运行（关闭窗口时保留托盘）", ToolTipText: "关闭主窗口时继续后台运行；要退出程序，请使用托盘右键菜单。", Checked: d.cfg.Background},
					CheckBox{AssignTo: &d.autoConnect, Text: "启动时自动连接校园网", ToolTipText: "普通模式下使用已保存账号自动认证并保活；开机认证服务启用时由服务统一负责。", Checked: d.cfg.AutoConnect},
					Composite{Background: TransparentBrush{}, Layout: HBox{MarginsZero: true, Spacing: 4}, Children: []Widget{
						CheckBox{AssignTo: &d.priorityStart, Text: "高优先级自启动（无需登录系统）", Checked: d.serviceEnabled, OnClicked: d.togglePriorityStart, ToolTipText: "安装开机后台认证服务，无需登录 Windows；点击开启或关闭立即请求管理员授权。不是 CPU 优先级或严格启动顺序保证。"},
						ImageView{Image: walk.IconShield(), MinSize: Size{Width: 16, Height: 16}, MaxSize: Size{Width: 20, Height: 20}, ToolTipText: "开启、关闭或更新开机认证需要管理员授权。"},
						HSpacer{},
					}},
				}},
				Composite{Background: TransparentBrush{}, Layout: HBox{}, Children: []Widget{
					PushButton{AssignTo: &d.saveButton, Text: "保存设置", OnClicked: func() { d.saveWithContinuation(true, nil) }},
					PushButton{AssignTo: &d.connectButton, Text: "连接", OnClicked: d.connect},
					PushButton{AssignTo: &d.checkButton, Text: "检查状态", OnClicked: d.check},
					PushButton{AssignTo: &d.disconnectButton, Text: "断开认证", OnClicked: d.disconnect},
				}},
				TextLabel{AssignTo: &d.status, Text: "就绪", MinSize: Size{Width: 0, Height: 32}},
			}},
			// The remaining client area reveals the background to the very edge.
			VSpacer{MinSize: Size{Height: 80}},
		},
	}).Create()
	if err != nil {
		bitmap.Dispose()
		return err
	}
	updateBackground := func() {
		bounds := d.mw.ClientBoundsPixels()
		if bounds.Width <= 0 || bounds.Height <= 0 {
			return
		}
		next, e := walk.NewBitmap(walk.Size{Width: bounds.Width, Height: bounds.Height})
		if e != nil {
			return
		}
		canvas, e := walk.NewCanvasFromImage(next)
		if e != nil {
			next.Dispose()
			return
		}
		base, e := walk.NewSolidColorBrush(walk.RGB(245, 245, 245))
		if e != nil {
			canvas.Dispose()
			next.Dispose()
			return
		}
		e = canvas.FillRectanglePixels(base, bounds)
		base.Dispose()
		// Scale to client width and anchor the original composition to the bottom.
		height := bounds.Width * im.Bounds().Dy() / im.Bounds().Dx()
		if e == nil {
			e = canvas.DrawImageStretchedPixels(bitmap, walk.Rectangle{Y: bounds.Height - height, Width: bounds.Width, Height: height})
		}
		canvas.Dispose()
		if e != nil {
			next.Dispose()
			return
		}
		brush, e := walk.NewBitmapBrush(next)
		if e != nil {
			next.Dispose()
			return
		}
		d.mw.SetBackground(brush)
		if backgroundBrush != nil {
			backgroundBrush.Dispose()
			backgroundBitmap.Dispose()
		}
		backgroundBrush, backgroundBitmap = brush, next
		d.mw.Invalidate()
	}
	d.mw.SizeChanged().Attach(updateBackground)
	updateBackground()
	d.mw.Disposing().Attach(func() {
		if backgroundBrush != nil {
			backgroundBrush.Dispose()
			backgroundBitmap.Dispose()
		}
		bitmap.Dispose()
	})
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
	d.quitAction = quit
	quit.Triggered().Attach(func() {
		if d.busy || d.exiting {
			return
		}
		d.exiting = true
		d.stop()
		d.mw.Close()
	})
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
		if d.busy {
			*cancel = true
			return
		}
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

// A service toggle is committed immediately through the same elevated save path.
// Restore the real checkbox state whenever validation or worker shutdown defers it.
func (d *desktop) togglePriorityStart() {
	d.saveWithContinuation(true, nil)
	if !d.busy {
		d.priorityStart.SetChecked(d.serviceEnabled)
	}
}

// saveWithContinuation runs only on the UI thread. The immutable snapshot is the
// only config handed to the elevated helper; connect resumes only after all saves.
func (d *desktop) saveWithContinuation(notify bool, next func()) {
	if d.busy || d.exiting {
		return
	}
	c := d.collect()
	wantService := d.priorityStart.Checked()
	if err := c.Validate(); err != nil {
		d.setStatus("设置无效: " + err.Error())
		return
	}
	if wantService && (c.Username == "" || c.Password == "") {
		d.setStatus("开机认证需要先填写账号和密码。")
		return
	}
	actualService, queryErr := bootservice.Installed()
	if queryErr != nil {
		d.serviceStateKnown = false
		d.serviceConfigKnown = false
		d.stop()
		d.setOperationEnabled(true)
		d.setStatus("无法确认开机服务状态，已禁止本地认证；请稍后重试保存: " + queryErr.Error())
		return
	}
	if !d.serviceStateKnown || actualService != d.serviceEnabled || !actualService {
		d.serviceConfigKnown = false
	}
	d.serviceEnabled, d.serviceStateKnown = actualService, true
	serviceChanged := d.serviceRecoveryRequired || wantService != actualService || (wantService && (!d.serviceConfigKnown || !sameServiceConfig(c, d.serviceConfig)))
	if !serviceChanged {
		d.finishSave(c, notify, false, next)
		return
	}
	// Cancelling is not joining: never elevate until the old worker (and its
	// authentication lease) has actually returned. Let the user retry afterward.
	d.stop()
	d.setOperationEnabled(true)
	if d.activeWorkers.Load() > 0 {
		d.setStatus("正在停止当前操作，请稍后再次保存以应用开机认证设置。")
		return
	}
	d.setBusy(true)
	d.setStatus("正在请求管理员授权并同步开机认证服务（首次保存需同步已安装服务的配置） …")
	owner := uintptr(d.mw.Handle())
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		manageErr := bootservice.Manage(owner, wantService, c)
		actual, stateErr := bootservice.Installed()
		d.mw.Synchronize(func() {
			d.setBusy(false)
			d.serviceStateKnown = stateErr == nil
			d.serviceConfigKnown = false
			if stateErr == nil {
				d.serviceEnabled = actual
				d.priorityStart.SetChecked(actual)
			}
			if manageErr != nil || stateErr != nil || actual != wantService {
				// A failed/timed-out helper may still finish later. A mere absent
				// Installed result cannot authorize a competing local worker.
				d.serviceStateKnown = false
				d.serviceRecoveryRequired = true
				message := "开机认证同步未完成；本地设置未保存，未继续连接。服务可能已部分变更，请重试同步。"
				if manageErr != nil {
					message += " 管理错误: " + manageErr.Error()
				}
				if stateErr != nil {
					message += " 服务状态未知，已禁止本地认证: " + stateErr.Error()
				} else if actual != wantService {
					message += " 实际安装状态与请求不一致。"
				}
				d.setStatus(message)
				return
			}
			d.serviceRecoveryRequired = false
			d.serviceConfig, d.serviceConfigKnown = c, wantService
			d.finishSave(c, notify, true, next)
		})
	}()
}

// finishSave is the common UI-thread commit path, with honest partial-success
// reporting rather than pretending the service and local settings are atomic.
func (d *desktop) finishSave(c app.Config, notify, serviceChanged bool, next func()) {
	prefix := ""
	if serviceChanged {
		prefix = "开机认证服务已更新，但"
	}
	if err := app.Save(d.path, c); err != nil {
		d.setStatus(prefix + "本地配置保存失败，未继续连接: " + err.Error())
		return
	}
	d.cfg = c // Match the successfully saved file even if the registry step fails.
	if err := platform.SetWindowsAutoStart(c.AutoStart, d.path); err != nil {
		d.setStatus(prefix + "本地配置已保存，但登录自启动设置失败，未继续连接；请重试保存: " + err.Error())
		return
	}
	if notify {
		if serviceChanged {
			d.setStatus("设置已保存，开机认证服务变更已立即生效；窗口启动偏好在下次启动时生效。")
		} else {
			d.setStatus("设置已保存。窗口启动偏好在下次启动时生效。")
		}
	}
	if next != nil {
		next()
	}
}

func (d *desktop) setOperationEnabled(enabled bool) {
	enabled = enabled && !d.busy && !d.exiting
	d.connectButton.SetEnabled(enabled)
	d.disconnectButton.SetEnabled(enabled)
	d.checkButton.SetEnabled(enabled)
}

func (d *desktop) setBusy(busy bool) {
	d.busy = busy
	enabled := !busy
	d.network.SetEnabled(enabled)
	d.username.SetEnabled(enabled)
	d.password.SetEnabled(enabled)
	for _, check := range []*walk.CheckBox{d.autoStart, d.startMin, d.background, d.autoConnect, d.priorityStart} {
		check.SetEnabled(enabled)
	}
	d.saveButton.SetEnabled(enabled)
	d.quitAction.SetEnabled(enabled)
	d.setOperationEnabled(enabled)
}

// Only authentication fields affect the service. Window preferences never need UAC.
func sameServiceConfig(a, b app.Config) bool {
	return a.Network == b.Network && a.Username == b.Username && a.Password == b.Password && a.Host == b.Host && a.AcID == b.AcID
}

func (d *desktop) setStatus(s string) { d.status.SetText(s) }
func statusText(s app.Status) string {
	message := s.Message
	switch message {
	case "Connected":
		message = "已连接"
	case "Disconnected":
		message = "校园网未认证"
	case "No network connection":
		message = "未连接网络，请先连接校园 Wi-Fi 或有线网络"
	case "Not on selected campus Wi-Fi":
		message = "当前 Wi-Fi 不是所选校园网络，请检查网络选择"
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

func (d *desktop) work(cfg app.Config, local bool, task func(context.Context, app.Config, func(app.Status)) error) {
	if d.busy || d.exiting {
		return
	}
	if local && !d.serviceStateKnown {
		d.setStatus("开机服务状态未知，已禁止本地操作；请先重试保存以确认服务状态。")
		return
	}
	d.stop()
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	generation := d.generation
	d.setOperationEnabled(false)
	d.wg.Add(1)
	d.activeWorkers.Add(1)
	go func() {
		defer d.wg.Done()
		defer d.activeWorkers.Add(-1)
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
		var err error
		if local {
			var release func()
			release, err = bootservice.AcquireAuthentication()
			if err != nil {
				err = fmt.Errorf("认证仍被其他操作持有（当前请求未执行）；若刚停止连接，请稍后重试: %w", err)
			}
			if err == nil {
				// Covers both the first authentication and the entire app.Run loop.
				defer release()
			}
		}
		if err == nil && ctx.Err() == nil {
			err = task(ctx, cfg, report)
		}
		if ctx.Err() != nil {
			return
		}
		d.mw.Synchronize(func() {
			if d.exiting || generation != d.generation {
				return
			}
			d.setOperationEnabled(true)
			if err != nil {
				d.setStatus("操作失败: " + err.Error())
			}
		})
	}()
}

// refreshServiceState never elevates. Other sessions may install/remove the
// service; fail closed on query errors, and invalidate any stale config cache.
func (d *desktop) refreshServiceState() bool {
	if d.serviceRecoveryRequired {
		d.setStatus("上次服务管理未确认完成，已禁止认证操作；请显式保存并重新同步服务。")
		return false
	}
	actual, err := bootservice.Installed()
	if err != nil {
		d.serviceStateKnown, d.serviceConfigKnown = false, false
		d.stop()
		d.setOperationEnabled(true)
		d.setStatus("无法确认开机服务状态，已禁止本地操作: " + err.Error())
		return false
	}
	if !d.serviceStateKnown || actual != d.serviceEnabled || !actual {
		d.serviceConfigKnown = false
	}
	d.serviceEnabled, d.serviceStateKnown = actual, true
	return true
}

func (d *desktop) connect() {
	d.saveWithContinuation(false, d.connectSaved)
}

func (d *desktop) connectSaved() {
	if d.busy || d.exiting || !d.refreshServiceState() {
		return
	}
	d.setStatus("正在连接 " + d.cfg.Network + " …")
	service := d.serviceEnabled
	// work increments generation synchronously before launching this closure.
	generation := d.generation + 1
	d.work(d.cfg, !service, func(ctx context.Context, c app.Config, report func(app.Status)) error {
		if service {
			s, err := bootservice.Request("connect")
			if err == nil {
				report(s)
			}
			return err
		}
		s, err := app.Connect(ctx, c)
		if err != nil {
			return err
		}
		report(s)
		if c.AutoConnect {
			// Never let a cancelled/superseded worker re-enable busy controls.
			d.mw.Synchronize(func() {
				if ctx.Err() == nil && !d.exiting && !d.busy && generation == d.generation {
					d.setOperationEnabled(true)
				}
			})
			return app.Run(ctx, c, report)
		}
		return nil
	})
}

func (d *desktop) check() {
	if d.busy || d.exiting || !d.refreshServiceState() {
		return
	}
	cfg := d.collect()
	d.setStatus("正在检查在线状态 …")
	service := d.serviceEnabled
	// Use the same generation, cancellation, wait group and accounting as all
	// other operations. A status check never saves or requests UAC.
	d.work(cfg, !service, func(parent context.Context, c app.Config, report func(app.Status)) error {
		ctx, cancel := context.WithTimeout(parent, 15*time.Second)
		defer cancel()
		var s app.Status
		var err error
		if service {
			s, err = bootservice.Request("status")
		} else {
			s, err = app.InspectStatus(ctx, c)
		}
		if err == nil {
			report(s)
		}
		return err
	})
}

func (d *desktop) disconnect() {
	if d.busy || d.exiting {
		return
	}
	if walk.MsgBox(d.mw, "断开认证", "确定注销本机校园网认证？这会中断本机网络。", walk.MsgBoxYesNo|walk.MsgBoxIconQuestion) != walk.DlgCmdYes {
		return
	}
	// Recheck after the modal prompt, which pumps Windows messages.
	if d.busy || d.exiting || !d.refreshServiceState() {
		return
	}
	cfg := d.collect()
	d.setStatus("正在注销 …")
	service := d.serviceEnabled
	d.work(cfg, !service, func(ctx context.Context, c app.Config, report func(app.Status)) error {
		if service {
			s, err := bootservice.Request("disconnect")
			if err == nil {
				report(s)
			}
			return err
		}
		e := app.Disconnect(ctx, c)
		if e == nil {
			report(app.Status{Message: "已注销，自动重连已停止。"})
		}
		return e
	})
}
