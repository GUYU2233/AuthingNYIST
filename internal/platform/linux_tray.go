//go:build linux

package platform

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/tray"
)

// trayOperationTimeout bounds one user-triggered connect/disconnect so a stale
// request cannot leave the menu disabled forever.
const trayOperationTimeout = 45 * time.Second

// trayRuntime owns the keepalive goroutine and the state shown by the tray.
type trayRuntime struct {
	cfg app.Config
	ctx context.Context
	out io.Writer

	mu        sync.Mutex
	cancel    context.CancelFunc
	running   bool
	status    string
	connected bool
	busy      bool
}

func newTrayRuntime(ctx context.Context, cfg app.Config, out io.Writer) *trayRuntime {
	return &trayRuntime{cfg: cfg, ctx: ctx, out: out, status: "正在检查网络…"}
}

func (t *trayRuntime) state() tray.State {
	t.mu.Lock()
	defer t.mu.Unlock()
	return tray.State{Title: "NYIST Connect", StatusText: t.status, Busy: t.busy, Connected: t.connected}
}

func (t *trayRuntime) set(status string, connected, busy bool) {
	t.mu.Lock()
	t.status, t.connected, t.busy = status, connected, busy
	t.mu.Unlock()
	tray.Refresh()
}

// report is called by the keepalive loop with each status snapshot.
func (t *trayRuntime) report(s app.Status) {
	if s.Online {
		t.set("保活中", true, false)
		return
	}
	text := "离线"
	if s.Message != "" {
		text = "离线：" + s.Message
	}
	t.set(text, false, false)
}

// start launches the keepalive loop unless it is already running. Keeping the
// loop alive is what keeps the campus session up, so "连接" means "开始保活".
func (t *trayRuntime) start() {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(t.ctx)
	t.cancel, t.running, t.busy = cancel, true, true
	t.status = "正在连接…"
	t.mu.Unlock()
	tray.Refresh()

	go func() {
		err := app.Run(ctx, t.cfg, t.report, linuxNotifyOptions())
		t.mu.Lock()
		t.running, t.busy = false, false
		t.cancel = nil
		t.connected = false
		switch {
		case ctx.Err() != nil:
			t.status = "已停止"
		case err != nil:
			t.status = "已停止：" + truncateStatus(err.Error())
		default:
			t.status = "已停止"
		}
		t.mu.Unlock()
		tray.Refresh()
	}()
}

func (t *trayRuntime) stop() context.CancelFunc {
	t.mu.Lock()
	cancel := t.cancel
	t.cancel = nil
	t.running = false
	t.mu.Unlock()
	return cancel
}

func (t *trayRuntime) disconnect() {
	if cancel := t.stop(); cancel != nil {
		cancel()
	}
	t.set("正在登出…", false, true)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), trayOperationTimeout)
		defer cancel()
		if err := app.Disconnect(ctx, t.cfg); err != nil {
			t.set("登出失败："+truncateStatus(err.Error()), false, false)
			return
		}
		t.set("已登出", false, false)
	}()
}

func truncateStatus(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\n", " "))
	if r := []rune(s); len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

// runLinuxTray shows a StatusNotifierItem and keeps the campus session alive
// while it is running. Without a tray host the command fails immediately with
// an actionable error instead of running invisibly.
func runLinuxTray(ctx context.Context, cfg app.Config, configPath string, out io.Writer) error {
	if err := tray.Available(); err != nil {
		return fmt.Errorf("%w\n可改用 --daemon 在无托盘桌面上后台保活，失败时仍会发送桌面通知", err)
	}
	rt := newTrayRuntime(ctx, cfg, out)
	trayCtx, stopTray := context.WithCancel(ctx)
	defer stopTray()
	rt.start()
	logPath := filepath.Join(filepath.Dir(configPath), "nyist-daemon.log")
	fmt.Fprintln(out, "托盘已启动；关闭托盘退出保活。日志: "+logPath)
	err := tray.Run(trayCtx,
		rt.state,
		tray.Actions{
			Connect:    rt.start,
			Disconnect: rt.disconnect,
			OpenLog: func() {
				if e := exec.Command("xdg-open", logPath).Start(); e != nil {
					fmt.Fprintln(out, "打开日志失败:", e)
				}
			},
			Quit: stopTray,
		},
		func(format string, args ...any) {
			fmt.Fprintf(out, format+"\n", args...)
		})
	if cancel := rt.stop(); cancel != nil {
		cancel()
	}
	if err == nil || ctx.Err() != nil {
		return nil
	}
	return err
}
