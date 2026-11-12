//go:build linux

package tray

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	notificationsService = "org.freedesktop.Notifications"
	notificationsIface   = "org.freedesktop.Notifications"
	notificationsPath    = dbus.ObjectPath("/org/freedesktop/Notifications")
	appName              = "AuthingNYIST"
	notifySendBinary     = "notify-send"

	notificationTimeout = 3 * time.Second
	fallbackTimeout     = 2 * time.Second
)

// Package variables so tests can exercise the fallback without a notification
// daemon or the real notify-send binary.
var (
	notifyLookPath = exec.LookPath
	notifyRun      = func(ctx context.Context, name string, args ...string) error {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.WaitDelay = time.Second
		return cmd.Run()
	}
)

// Notify shows a desktop notification. It is best effort: it returns an error
// when neither org.freedesktop.Notifications nor notify-send is usable, and the
// caller may ignore it. It never blocks for more than a few seconds and never
// escapes its arguments through a shell.
//
// The title is plain text; the body is stripped of control characters,
// truncated and HTML-escaped so daemons that interpret simple markup render it
// literally instead of executing injected tags.
func Notify(title, body string, urgent bool) error {
	return notify(context.Background(), dialSession, title, body, urgent)
}

func notify(ctx context.Context, dial dialFunc, title, body string, urgent bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	summary := sanitizeNotificationText(title, maxNotificationTitle)
	text := escapeNotificationMarkup(sanitizeNotificationText(body, maxNotificationBody))

	dbusErr := notifyDBus(ctx, dial, summary, text, urgent)
	if dbusErr == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("tray: notify: %w", dbusErr)
	}
	if sendErr := notifySend(ctx, summary, text, urgent); sendErr == nil {
		return nil
	} else {
		return fmt.Errorf("tray: notify: %w", errors.Join(dbusErr, sendErr))
	}
}

func notifyDBus(ctx context.Context, dial dialFunc, summary, body string, urgent bool) error {
	ctx, cancel := context.WithTimeout(ctx, notificationTimeout)
	defer cancel()

	conn, err := dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	hints := map[string]dbus.Variant{
		"urgency": dbus.MakeVariant(urgencyByte(urgent)),
	}
	obj := conn.Object(notificationsService, notificationsPath)
	call := obj.CallWithContext(ctx, notificationsIface+".Notify", 0,
		appName, uint32(0), "", summary, body, []string{}, hints, int32(-1))
	if call.Err != nil {
		if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return ctx.Err()
		}
		return call.Err
	}
	return nil
}

func urgencyByte(urgent bool) byte {
	if urgent {
		return 2 // critical
	}
	return 1 // normal
}

// notifySend is the graceful fallback for systems without a notification
// daemon. Arguments are passed as argv, never through a shell.
func notifySend(ctx context.Context, summary, body string, urgent bool) error {
	path, err := notifyLookPath(notifySendBinary)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, fallbackTimeout)
	defer cancel()

	urgency := "--urgency=normal"
	if urgent {
		urgency = "--urgency=critical"
	}
	args := []string{"--app-name=" + appName, urgency, "--", summary, body}
	if err := notifyRun(ctx, path, args...); err != nil {
		return fmt.Errorf("tray: %s: %w", notifySendBinary, err)
	}
	return nil
}
