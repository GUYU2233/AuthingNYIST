//go:build linux

package tray

import (
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	defaultTitle = "AuthingNYIST"
	iconName     = "network-wireless-symbolic"
)

// sniPixmap is the (iiay) pixmap record used by StatusNotifierItem.
type sniPixmap struct {
	Width  int32
	Height int32
	Data   []byte
}

// sniTooltip is the (sa(iiay)ss) tooltip record.
type sniTooltip struct {
	IconName   string
	IconPixmap []sniPixmap
	Title      string
	Subtitle   string
}

func titleOf(st State) string {
	if t := strings.TrimSpace(st.Title); t != "" {
		return t
	}
	return defaultTitle
}

func tooltipFor(st State) sniTooltip {
	return sniTooltip{
		IconName:   iconName,
		IconPixmap: make([]sniPixmap, 0),
		Title:      titleOf(st),
		Subtitle:   strings.TrimSpace(st.StatusText),
	}
}

// statusForState maps the application state onto the SNI status strings.
func statusForState(st State) string {
	switch {
	case st.Busy:
		// Busy means "look at me": the user may need to finish authentication.
		return "NeedsAttention"
	default:
		// SNI status controls visibility, not campus connectivity. Passive is
		// hidden by AppIndicator; keep the menu reachable even when offline.
		return "Active"
	}
}

// sniItem implements org.kde.StatusNotifierItem.
type sniItem struct {
	srv *server
}

// Activate is emitted when the user clicks the icon. Settings is the closest
// equivalent of opening the application window.
func (i *sniItem) Activate(x, y int32) *dbus.Error {
	i.srv.dispatch("settings", i.srv.actions.Settings)
	return nil
}

// ContextMenu is a no-op: the menu is exported at Menu and hosts use it
// directly.
func (i *sniItem) ContextMenu(x, y int32) *dbus.Error {
	return nil
}

func (i *sniItem) SecondaryActivate(x, y int32) *dbus.Error {
	return nil
}

func (i *sniItem) Scroll(delta int32, orientation string) *dbus.Error {
	return nil
}

func (i *sniItem) ProvideXdgActivationToken(token string) *dbus.Error {
	return nil
}

// sniProps implements org.freedesktop.DBus.Properties for the item.
type sniProps struct {
	srv *server
}

func (p *sniProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != "" && iface != sniInterface {
		return dbus.Variant{}, invalidArgs("unknown interface " + iface)
	}
	props := p.all(p.srv.currentState())
	v, ok := props[name]
	if !ok {
		return dbus.Variant{}, invalidArgs("unknown property " + name)
	}
	return v, nil
}

func (p *sniProps) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != "" && iface != sniInterface {
		return map[string]dbus.Variant{}, nil
	}
	return p.all(p.srv.currentState()), nil
}

func (p *sniProps) Set(iface, name string, value dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{name})
}

func (p *sniProps) all(st State) map[string]dbus.Variant {
	empty := make([]sniPixmap, 0)
	return map[string]dbus.Variant{
		"Category":            dbus.MakeVariant("ApplicationStatus"),
		"Id":                  dbus.MakeVariant("authing-nyist"),
		"Title":               dbus.MakeVariant(titleOf(st)),
		"Status":              dbus.MakeVariant(statusForState(st)),
		"WindowId":            dbus.MakeVariant(int32(0)),
		"IconName":            dbus.MakeVariant(iconName),
		"IconPixmap":          dbus.MakeVariant(empty),
		"OverlayIconName":     dbus.MakeVariant(""),
		"OverlayIconPixmap":   dbus.MakeVariant(make([]sniPixmap, 0)),
		"AttentionIconName":   dbus.MakeVariant(iconName),
		"AttentionIconPixmap": dbus.MakeVariant(make([]sniPixmap, 0)),
		"AttentionMovieName":  dbus.MakeVariant(""),
		"ToolTip":             dbus.MakeVariant(tooltipFor(st)),
		"ItemIsMenu":          dbus.MakeVariant(true),
		"Menu":                dbus.MakeVariant(menuPath),
	}
}

func invalidArgs(msg string) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.InvalidArgs", []any{msg})
}

func sniNode() *introspect.Node {
	return &introspect.Node{
		Interfaces: []introspect.Interface{
			sniInterfaceData(),
			propertiesInterfaceData(),
			introspect.IntrospectData,
		},
	}
}

func sniInterfaceData() introspect.Interface {
	xy := func() []introspect.Arg {
		return []introspect.Arg{
			{Name: "x", Type: "i", Direction: "in"},
			{Name: "y", Type: "i", Direction: "in"},
		}
	}
	return introspect.Interface{
		Name: sniInterface,
		Methods: []introspect.Method{
			{Name: "ContextMenu", Args: xy()},
			{Name: "Activate", Args: xy()},
			{Name: "SecondaryActivate", Args: xy()},
			{Name: "Scroll", Args: []introspect.Arg{
				{Name: "delta", Type: "i", Direction: "in"},
				{Name: "orientation", Type: "s", Direction: "in"},
			}},
			{Name: "ProvideXdgActivationToken", Args: []introspect.Arg{
				{Name: "token", Type: "s", Direction: "in"},
			}},
		},
		Signals: []introspect.Signal{
			{Name: "NewTitle"},
			{Name: "NewIcon"},
			{Name: "NewAttentionIcon"},
			{Name: "NewOverlayIcon"},
			{Name: "NewToolTip"},
			{Name: "NewStatus", Args: []introspect.Arg{
				{Name: "status", Type: "s", Direction: "out"},
			}},
		},
		Properties: []introspect.Property{
			{Name: "Category", Type: "s", Access: "read"},
			{Name: "Id", Type: "s", Access: "read"},
			{Name: "Title", Type: "s", Access: "read"},
			{Name: "Status", Type: "s", Access: "read"},
			{Name: "WindowId", Type: "i", Access: "read"},
			{Name: "IconName", Type: "s", Access: "read"},
			{Name: "IconPixmap", Type: "a(iiay)", Access: "read"},
			{Name: "OverlayIconName", Type: "s", Access: "read"},
			{Name: "OverlayIconPixmap", Type: "a(iiay)", Access: "read"},
			{Name: "AttentionIconName", Type: "s", Access: "read"},
			{Name: "AttentionIconPixmap", Type: "a(iiay)", Access: "read"},
			{Name: "AttentionMovieName", Type: "s", Access: "read"},
			{Name: "ToolTip", Type: "(sa(iiay)ss)", Access: "read"},
			{Name: "ItemIsMenu", Type: "b", Access: "read"},
			{Name: "Menu", Type: "o", Access: "read"},
		},
	}
}

func propertiesInterfaceData() introspect.Interface {
	return introspect.Interface{
		Name: propertiesIface,
		Methods: []introspect.Method{
			{Name: "Get", Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "in"},
				{Name: "property", Type: "s", Direction: "in"},
				{Name: "value", Type: "v", Direction: "out"},
			}},
			{Name: "GetAll", Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "in"},
				{Name: "props", Type: "a{sv}", Direction: "out"},
			}},
			{Name: "Set", Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "in"},
				{Name: "property", Type: "s", Direction: "in"},
				{Name: "value", Type: "v", Direction: "in"},
			}},
		},
		Signals: []introspect.Signal{
			{Name: "PropertiesChanged", Args: []introspect.Arg{
				{Name: "interface", Type: "s", Direction: "out"},
				{Name: "changed", Type: "a{sv}", Direction: "out"},
				{Name: "invalidated", Type: "as", Direction: "out"},
			}},
		},
	}
}
