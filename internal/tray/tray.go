package tray

import (
	"errors"
	"strings"
)

// State is a snapshot of what the tray shows. It is passed to the state
// callback supplied to Run and is safe to copy.
type State struct {
	// Title is the application name shown in the tooltip and title property.
	Title string
	// StatusText is the human readable status, for example "已连接".
	StatusText string
	// Busy disables connect/disconnect while an operation is in flight.
	Busy bool
	// Connected reports whether the campus network session is up.
	Connected bool
}

// StatusLabel returns the label of the disabled "状态" menu item.
func (s State) StatusLabel() string {
	text := strings.TrimSpace(s.StatusText)
	if text == "" {
		text = "未知"
	}
	return "状态：" + text
}

// Actions are the callbacks invoked by the context menu. Any field may be nil;
// the corresponding menu item is then disabled (the "退出" item stays enabled
// but only stops the tray, see Run).
type Actions struct {
	Connect    func()
	Disconnect func()
	Settings   func()
	OpenLog    func()
	Quit       func()
}

var (
	// ErrUnsupported is returned by all stubs on platforms other than Linux.
	ErrUnsupported = errors.New("tray: 当前平台不支持托盘")
	// ErrNoStatusNotifierHost is returned when the session bus has no
	// org.kde.StatusNotifierWatcher, i.e. the desktop cannot show an SNI icon.
	// GNOME Shell only gains a tray through the AppIndicator/KStatusNotifierItem
	// extension; without it this error is expected and callers should fall back
	// to headless mode plus notifications.
	ErrNoStatusNotifierHost = errors.New("tray: 桌面环境未提供托盘宿主（GNOME 需要 " +
		"AppIndicator/KStatusNotifierItem 扩展）")
	// ErrUnavailable is kept as an alias of ErrNoStatusNotifierHost for callers
	// that only need "a tray is not possible here".
	ErrUnavailable = ErrNoStatusNotifierHost
	// ErrAlreadyRunning is returned by Run when another tray is already active
	// in this process. Only one tray per process is supported.
	ErrAlreadyRunning = errors.New("tray: 托盘已在运行")
)
