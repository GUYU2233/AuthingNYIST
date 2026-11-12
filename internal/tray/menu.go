package tray

import (
	"strconv"
	"strings"
)

// Menu item ids. They are stable because some hosts cache them across layouts.
const (
	menuIDStatus     int32 = 1
	menuIDSep1       int32 = 2
	menuIDConnect    int32 = 3
	menuIDDisconnect int32 = 4
	menuIDSep2       int32 = 5
	menuIDSettings   int32 = 6
	menuIDOpenLog    int32 = 7
	menuIDSep3       int32 = 8
	menuIDQuit       int32 = 9
)

type menuKind int

const (
	kindStandard menuKind = iota
	kindSeparator
)

// menuEntry is the platform independent description of one dbusmenu item. It is
// converted to D-Bus variants in menu_linux.go so the layout logic stays
// testable without a bus.
type menuEntry struct {
	ID      int32
	Label   string
	Kind    menuKind
	Enabled bool
	Visible bool
}

// buildMenu derives the menu from the current state and available actions.
func buildMenu(st State, a Actions) []menuEntry {
	busy := st.Busy
	connected := st.Connected
	return []menuEntry{
		{ID: menuIDStatus, Label: st.StatusLabel(), Kind: kindStandard, Enabled: false, Visible: true},
		{ID: menuIDSep1, Kind: kindSeparator, Enabled: false, Visible: true},
		{
			ID:      menuIDConnect,
			Label:   "连接",
			Kind:    kindStandard,
			Enabled: !busy && !connected && a.Connect != nil,
			Visible: true,
		},
		{
			ID:      menuIDDisconnect,
			Label:   "断开连接",
			Kind:    kindStandard,
			Enabled: !busy && connected && a.Disconnect != nil,
			Visible: true,
		},
		{ID: menuIDSep2, Kind: kindSeparator, Enabled: false, Visible: true},
		{ID: menuIDSettings, Label: "设置", Kind: kindStandard, Enabled: a.Settings != nil, Visible: true},
		{ID: menuIDOpenLog, Label: "打开日志", Kind: kindStandard, Enabled: a.OpenLog != nil, Visible: true},
		{ID: menuIDSep3, Kind: kindSeparator, Enabled: false, Visible: true},
		{ID: menuIDQuit, Label: "退出", Kind: kindStandard, Enabled: true, Visible: true},
	}
}

// menuSignature changes whenever anything a user can observe in the menu
// changes; it is used to avoid emitting spurious LayoutUpdated signals.
func menuSignature(entries []menuEntry) string {
	var b strings.Builder
	for _, e := range entries {
		b.WriteString(strconv.Itoa(int(e.ID)))
		b.WriteByte(':')
		b.WriteString(strconv.Itoa(int(e.Kind)))
		b.WriteByte(':')
		if e.Enabled {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		b.WriteByte(':')
		if e.Visible {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
		b.WriteByte(':')
		b.WriteString(e.Label)
		b.WriteByte('\n')
	}
	return b.String()
}

func menuItemByID(entries []menuEntry, id int32) (menuEntry, bool) {
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return menuEntry{}, false
}
