// Package tray implements a Linux system-tray icon and desktop notifications
// for AuthingNYIST using only pure-Go D-Bus (github.com/godbus/dbus/v5). It
// deliberately avoids cgo, GTK, AppIndicator and other native libraries so the
// release binaries can be built with CGO_ENABLED=0.
//
// # Tray icon
//
// The tray icon is a StatusNotifierItem (org.kde.StatusNotifierItem) with a
// com.canonical.dbusmenu context menu. The process must talk to a session bus
// that hosts a StatusNotifierWatcher (KDE Plasma, GNOME with the AppIndicator
// extension, Xfce, Cinnamon, …). GNOME Shell has no tray of its own, so without
// that extension Run fails fast with ErrNoStatusNotifierHost rather than
// registering an icon nobody will ever show. Call Available before deciding
// whether to start a tray:
//
//	if err := tray.Available(); err != nil {
//		// headless session, no watcher, or GNOME without the AppIndicator
//		// extension: run headless and rely on tray.Notify instead.
//	}
//
// Run blocks until ctx is canceled, the tray is stopped, or setup fails:
//
//	err := tray.Run(ctx,
//		func() tray.State {
//			return tray.State{Title: "NYIST", StatusText: "已连接", Connected: true}
//		},
//		tray.Actions{
//			Connect:    func() { ... },
//			Disconnect: func() { ... },
//			Settings:   func() { ... },
//			OpenLog:    func() { ... },
//			Quit:       func() { cancel() },
//		},
//		logger.Printf)
//
// # Pushing updates
//
// State is read lazily: the tray calls the state callback once at startup,
// whenever Refresh is called, and once per second as a safety net. This means
// callers do not need to feed a channel and may freely mutate their own state;
// they only need to call Refresh (or wait up to a second) for it to appear.
// Refresh is safe to call from any goroutine and is a no-op when no tray is
// running:
//
//	status = "重新认证失败"
//	tray.Refresh()
//
// The callback and the Actions functions are called from D-Bus goroutines, so
// they must be safe for concurrent use. Actions are invoked on a separate
// goroutine and may block (for example on an interactive D-Bus prompt); a
// panic inside an action is recovered and logged.
//
// # Notifications
//
// Notify sends a desktop notification through org.freedesktop.Notifications and
// never blocks for more than a few seconds. If the notification daemon is
// missing it returns an error the caller may ignore; if notify-send exists it is
// used as a fallback. Passwords and other secrets must never be passed to
// Notify or the logger: this package does no redaction.
//
// # Non-Linux
//
// On Windows and macOS every function in this package is a stub that returns
// ErrUnsupported, so cross-builds keep working.
package tray
