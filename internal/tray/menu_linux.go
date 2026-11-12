//go:build linux

package tray

import (
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

// menuLayout is the (ia{sv}av) struct returned by GetLayout.
type menuLayout struct {
	ID         int32
	Properties map[string]dbus.Variant
	Children   []dbus.Variant
}

// menuEvent is one (isvu) element of EventGroup.
type menuEvent struct {
	ID        int32
	EventID   string
	Data      dbus.Variant
	Timestamp uint32
}

// menuGroupProperty is one (ia{sv}) element of GetGroupProperties.
type menuGroupProperty struct {
	ID         int32
	Properties map[string]dbus.Variant
}

// menuRemovedProperties is the (ias) record in ItemsPropertiesUpdated.
type menuRemovedProperties struct {
	ID    int32
	Names []string
}

// dbusMenu implements com.canonical.dbusmenu.
type dbusMenu struct {
	srv *server
}

func (m *dbusMenu) GetLayout(parentID int32, recursionDepth int32, propertyNames []string) (uint32, menuLayout, *dbus.Error) {
	entries := buildMenu(m.srv.currentState(), m.srv.actions)
	if parentID != 0 {
		return m.srv.revision.Load(), menuLayout{ID: parentID, Properties: map[string]dbus.Variant{}, Children: []dbus.Variant{}}, nil
	}
	return m.srv.revision.Load(), rootLayout(entries, propertyNames), nil
}

func (m *dbusMenu) GetGroupProperties(ids []int32, propertyNames []string) ([]menuGroupProperty, *dbus.Error) {
	entries := buildMenu(m.srv.currentState(), m.srv.actions)
	return groupProperties(entries, ids, propertyNames), nil
}

func (m *dbusMenu) GetProperty(id int32, name string) (dbus.Variant, *dbus.Error) {
	entries := buildMenu(m.srv.currentState(), m.srv.actions)
	if id == 0 {
		if name != "children-display" {
			return dbus.Variant{}, invalidArgs("unknown property " + name)
		}
		return dbus.MakeVariant("submenu"), nil
	}
	entry, ok := menuItemByID(entries, id)
	if !ok {
		return dbus.Variant{}, invalidArgs("unknown menu item")
	}
	props := entryProperties(entry, []string{name})
	v, ok := props[name]
	if !ok {
		return dbus.Variant{}, invalidArgs("unknown property " + name)
	}
	return v, nil
}

func (m *dbusMenu) Event(id int32, eventID string, data dbus.Variant, timestamp uint32) *dbus.Error {
	entries := buildMenu(m.srv.currentState(), m.srv.actions)
	entry, ok := menuItemByID(entries, id)
	if !ok || entry.Kind == kindSeparator || !entry.Enabled {
		return nil
	}
	if eventID != "clicked" {
		return nil
	}
	switch id {
	case menuIDConnect:
		m.srv.dispatch("connect", m.srv.actions.Connect)
	case menuIDDisconnect:
		m.srv.dispatch("disconnect", m.srv.actions.Disconnect)
	case menuIDSettings:
		m.srv.dispatch("settings", m.srv.actions.Settings)
	case menuIDOpenLog:
		m.srv.dispatch("open log", m.srv.actions.OpenLog)
	case menuIDQuit:
		if m.srv.actions.Quit != nil {
			m.srv.dispatch("quit", m.srv.actions.Quit)
		} else {
			// No owner supplied a quit callback; stopping the tray is the only
			// sensible interpretation of "退出".
			m.srv.stop()
		}
	}
	return nil
}

func (m *dbusMenu) EventGroup(events []menuEvent) ([]int32, *dbus.Error) {
	for _, ev := range events {
		if err := m.Event(ev.ID, ev.EventID, ev.Data, ev.Timestamp); err != nil {
			return []int32{ev.ID}, err
		}
	}
	return nil, nil
}

func (m *dbusMenu) AboutToShow(id int32) (bool, *dbus.Error) {
	// Hosts can cache the initial layout before subscribing to signals.
	// Force a fresh snapshot whenever the user opens the menu.
	m.srv.sync()
	return true, nil
}

func (m *dbusMenu) AboutToShowGroup(ids []int32) ([]int32, []int32, *dbus.Error) {
	return nil, nil, nil
}

// menuProps implements org.freedesktop.DBus.Properties for the menu object.
type menuProps struct {
	srv *server
}

func (p *menuProps) Get(iface, name string) (dbus.Variant, *dbus.Error) {
	if iface != "" && iface != dbusmenuInterface {
		return dbus.Variant{}, invalidArgs("unknown interface " + iface)
	}
	v, ok := p.all()[name]
	if !ok {
		return dbus.Variant{}, invalidArgs("unknown property " + name)
	}
	return v, nil
}

func (p *menuProps) GetAll(iface string) (map[string]dbus.Variant, *dbus.Error) {
	if iface != "" && iface != dbusmenuInterface {
		return map[string]dbus.Variant{}, nil
	}
	return p.all(), nil
}

func (p *menuProps) Set(iface, name string, value dbus.Variant) *dbus.Error {
	return dbus.NewError("org.freedesktop.DBus.Error.PropertyReadOnly", []any{name})
}

func (p *menuProps) all() map[string]dbus.Variant {
	return map[string]dbus.Variant{
		"Version":       dbus.MakeVariant(uint32(3)),
		"TextDirection": dbus.MakeVariant("ltr"),
		"Status":        dbus.MakeVariant("normal"),
		"IconThemePath": dbus.MakeVariant([]string{}),
	}
}

func rootLayout(entries []menuEntry, propertyNames []string) menuLayout {
	root := menuLayout{
		ID:         0,
		Properties: map[string]dbus.Variant{},
		Children:   make([]dbus.Variant, 0, len(entries)),
	}
	if wantsProperty(propertyNames, "children-display") {
		root.Properties["children-display"] = dbus.MakeVariant("submenu")
	}
	for _, e := range entries {
		root.Children = append(root.Children, dbus.MakeVariant(menuLayout{
			ID:         e.ID,
			Properties: entryProperties(e, propertyNames),
			Children:   []dbus.Variant{},
		}))
	}
	return root
}

func entryProperties(e menuEntry, propertyNames []string) map[string]dbus.Variant {
	props := make(map[string]dbus.Variant, 4)
	if wantsProperty(propertyNames, "label") {
		props["label"] = dbus.MakeVariant(e.Label)
	}
	if wantsProperty(propertyNames, "enabled") {
		props["enabled"] = dbus.MakeVariant(e.Enabled)
	}
	if wantsProperty(propertyNames, "visible") {
		props["visible"] = dbus.MakeVariant(e.Visible)
	}
	if wantsProperty(propertyNames, "type") {
		if e.Kind == kindSeparator {
			props["type"] = dbus.MakeVariant("separator")
		} else {
			props["type"] = dbus.MakeVariant("standard")
		}
	}
	return props
}

// groupProperties returns the requested ids (all when ids is empty) in menu
// order.
func groupProperties(entries []menuEntry, ids []int32, propertyNames []string) []menuGroupProperty {
	want := make(map[int32]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	out := make([]menuGroupProperty, 0, len(entries)+1)
	if len(ids) == 0 || want[0] {
		out = append(out, menuGroupProperty{
			ID:         0,
			Properties: map[string]dbus.Variant{"children-display": dbus.MakeVariant("submenu")},
		})
	}
	for _, e := range entries {
		if len(ids) > 0 && !want[e.ID] {
			continue
		}
		out = append(out, menuGroupProperty{ID: e.ID, Properties: entryProperties(e, propertyNames)})
	}
	return out
}

func wantsProperty(names []string, name string) bool {
	if len(names) == 0 {
		return true
	}
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

func menuNode() *introspect.Node {
	return &introspect.Node{
		Interfaces: []introspect.Interface{
			dbusMenuInterfaceData(),
			propertiesInterfaceData(),
			introspect.IntrospectData,
		},
	}
}

func dbusMenuInterfaceData() introspect.Interface {
	return introspect.Interface{
		Name: dbusmenuInterface,
		Methods: []introspect.Method{
			{Name: "GetLayout", Args: []introspect.Arg{
				{Name: "parentId", Type: "i", Direction: "in"},
				{Name: "recursionDepth", Type: "i", Direction: "in"},
				{Name: "propertyNames", Type: "as", Direction: "in"},
				{Name: "revision", Type: "u", Direction: "out"},
				{Name: "layout", Type: "(ia{sv}av)", Direction: "out"},
			}},
			{Name: "GetGroupProperties", Args: []introspect.Arg{
				{Name: "ids", Type: "ai", Direction: "in"},
				{Name: "propertyNames", Type: "as", Direction: "in"},
				{Name: "properties", Type: "a(ia{sv})", Direction: "out"},
			}},
			{Name: "GetProperty", Args: []introspect.Arg{
				{Name: "id", Type: "i", Direction: "in"},
				{Name: "name", Type: "s", Direction: "in"},
				{Name: "value", Type: "v", Direction: "out"},
			}},
			{Name: "Event", Args: []introspect.Arg{
				{Name: "id", Type: "i", Direction: "in"},
				{Name: "eventId", Type: "s", Direction: "in"},
				{Name: "data", Type: "v", Direction: "in"},
				{Name: "timestamp", Type: "u", Direction: "in"},
			}},
			{Name: "EventGroup", Args: []introspect.Arg{
				{Name: "events", Type: "a(isvu)", Direction: "in"},
				{Name: "idErrors", Type: "ai", Direction: "out"},
			}},
			{Name: "AboutToShow", Args: []introspect.Arg{
				{Name: "id", Type: "i", Direction: "in"},
				{Name: "needUpdate", Type: "b", Direction: "out"},
			}},
			{Name: "AboutToShowGroup", Args: []introspect.Arg{
				{Name: "ids", Type: "ai", Direction: "in"},
				{Name: "updatesNeeded", Type: "ai", Direction: "out"},
				{Name: "idErrors", Type: "ai", Direction: "out"},
			}},
		},
		Signals: []introspect.Signal{
			{Name: "ItemsPropertiesUpdated", Args: []introspect.Arg{
				{Name: "updatedProps", Type: "a(ia{sv})", Direction: "out"},
				{Name: "removedProps", Type: "a(ias)", Direction: "out"},
			}},
			{Name: "LayoutUpdated", Args: []introspect.Arg{
				{Name: "revision", Type: "u", Direction: "out"},
				{Name: "parent", Type: "i", Direction: "out"},
			}},
			{Name: "ItemActivationRequested", Args: []introspect.Arg{
				{Name: "id", Type: "i", Direction: "out"},
				{Name: "timestamp", Type: "u", Direction: "out"},
			}},
		},
		Properties: []introspect.Property{
			{Name: "Version", Type: "u", Access: "read"},
			{Name: "TextDirection", Type: "s", Access: "read"},
			{Name: "Status", Type: "s", Access: "read"},
			{Name: "IconThemePath", Type: "as", Access: "read"},
		},
	}
}
