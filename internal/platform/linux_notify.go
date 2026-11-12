//go:build linux

package platform

import (
	"github.com/Palvef/AuthingNYIST/internal/app"
	"github.com/Palvef/AuthingNYIST/internal/tray"
)

// linuxNotifyOptions wires keepalive conditions to the desktop notification
// daemon. Notifications are best effort: a headless session or a missing
// notification service must never turn a keepalive failure into a crash, so the
// errors are deliberately discarded. The callbacks run on the keepalive
// goroutine, and Notify is bounded by its own timeout.
func linuxNotifyOptions() app.RunOptions {
	return app.RunOptions{
		OnFailure: func(err error) {
			_ = tray.Notify("校园网认证失败", err.Error(), true)
		},
		OnRecovered: func() {
			_ = tray.Notify("校园网已恢复", "保活检测重新通过。", false)
		},
	}
}
