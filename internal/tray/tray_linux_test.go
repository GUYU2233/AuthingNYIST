//go:build linux

package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// --- fake bus ---------------------------------------------------------------

type fakeCall struct {
	Dest   string
	Path   dbus.ObjectPath
	Method string
	Args   []any
}

type fakeBus struct {
	mu        sync.Mutex
	handler   func(method string, args []any) ([]any, error)
	calls     []fakeCall
	exported  map[string]any
	emitted   []string
	closed    int
	signalCh  chan<- *dbus.Signal
	nameReply dbus.RequestNameReply
}

func newFakeBus(handler func(method string, args []any) ([]any, error)) *fakeBus {
	if handler == nil {
		handler = defaultFakeHandler
	}
	return &fakeBus{
		handler:   handler,
		exported:  make(map[string]any),
		nameReply: dbus.RequestNameReplyPrimaryOwner,
	}
}

func defaultFakeHandler(method string, args []any) ([]any, error) {
	switch method {
	case "org.freedesktop.DBus.GetNameOwner":
		return []any{"1.42"}, nil
	case "org.freedesktop.DBus.AddMatch":
		return nil, nil
	case "org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem":
		return nil, nil
	case "org.freedesktop.Notifications.Notify":
		return []any{uint32(7)}, nil
	}
	return nil, nil
}

func (f *fakeBus) Export(v any, path dbus.ObjectPath, iface string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exported[string(path)+"|"+iface] = v
	return nil
}

func (f *fakeBus) Emit(path dbus.ObjectPath, name string, values ...any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.emitted = append(f.emitted, name)
	return nil
}

func (f *fakeBus) Signal(ch chan<- *dbus.Signal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.signalCh = ch
}

func (f *fakeBus) RemoveSignal(ch chan<- *dbus.Signal) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.signalCh == ch {
		f.signalCh = nil
	}
}

func (f *fakeBus) Object(dest string, path dbus.ObjectPath) dbus.BusObject {
	return &fakeObject{bus: f, dest: dest, path: path}
}

func (f *fakeBus) RequestName(name string, flags dbus.RequestNameFlags) (dbus.RequestNameReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{Method: "org.freedesktop.DBus.RequestName", Args: []any{name}})
	return f.nameReply, nil
}

func (f *fakeBus) ReleaseName(name string) (dbus.ReleaseNameReply, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, fakeCall{Method: "org.freedesktop.DBus.ReleaseName", Args: []any{name}})
	return dbus.ReleaseNameReplyReleased, nil
}

func (f *fakeBus) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
	return nil
}

func (f *fakeBus) call(method string, args []any) ([]any, error) {
	f.mu.Lock()
	handler := f.handler
	f.mu.Unlock()
	return handler(method, args)
}

func (f *fakeBus) record(c fakeCall) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, c)
}

func (f *fakeBus) hasCall(method string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.Method == method {
			return true
		}
	}
	return false
}

func (f *fakeBus) callCount(method string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c.Method == method {
			n++
		}
	}
	return n
}

func (f *fakeBus) lastCall(method string) (fakeCall, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.calls) - 1; i >= 0; i-- {
		if f.calls[i].Method == method {
			return f.calls[i], true
		}
	}
	return fakeCall{}, false
}

func (f *fakeBus) closeCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

func (f *fakeBus) exportedObject(path dbus.ObjectPath, iface string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exported[string(path)+"|"+iface]
}

func (f *fakeBus) signalSubscribed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.signalCh != nil
}

func (f *fakeBus) sendSignal(sig *dbus.Signal) bool {
	f.mu.Lock()
	ch := f.signalCh
	f.mu.Unlock()
	if ch == nil {
		return false
	}
	select {
	case ch <- sig:
		return true
	default:
		return false
	}
}

type fakeObject struct {
	bus  *fakeBus
	dest string
	path dbus.ObjectPath
}

func (o *fakeObject) CallWithContext(ctx context.Context, method string, flags dbus.Flags, args ...any) *dbus.Call {
	o.bus.record(fakeCall{Dest: o.dest, Path: o.path, Method: method, Args: args})
	body, err := o.bus.call(method, args)
	call := &dbus.Call{
		Destination: o.dest,
		Path:        o.path,
		Method:      method,
		Args:        args,
		Done:        make(chan *dbus.Call, 1),
		Err:         err,
		Body:        body,
	}
	call.Done <- call
	return call
}

func (o *fakeObject) Call(method string, flags dbus.Flags, args ...any) *dbus.Call {
	return o.CallWithContext(context.Background(), method, flags, args...)
}

func (o *fakeObject) GoWithContext(ctx context.Context, method string, flags dbus.Flags, ch chan *dbus.Call, args ...any) *dbus.Call {
	call := o.CallWithContext(ctx, method, flags, args...)
	if ch != nil {
		ch <- call
	}
	return call
}

func (o *fakeObject) Go(method string, flags dbus.Flags, ch chan *dbus.Call, args ...any) *dbus.Call {
	return o.GoWithContext(context.Background(), method, flags, ch, args...)
}

func (o *fakeObject) AddMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return o.Call("org.freedesktop.DBus.AddMatch", 0, iface, member)
}

func (o *fakeObject) RemoveMatchSignal(iface, member string, options ...dbus.MatchOption) *dbus.Call {
	return o.Call("org.freedesktop.DBus.RemoveMatch", 0, iface, member)
}

func (o *fakeObject) GetProperty(p string) (dbus.Variant, error) { return dbus.Variant{}, nil }
func (o *fakeObject) StoreProperty(p string, value any) error    { return nil }
func (o *fakeObject) SetProperty(p string, v any) error          { return nil }
func (o *fakeObject) Destination() string                        { return o.dest }
func (o *fakeObject) Path() dbus.ObjectPath                      { return o.path }

// --- harness ----------------------------------------------------------------

type stateHolder struct {
	mu sync.Mutex
	st State
}

func (h *stateHolder) get() State {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.st
}

func (h *stateHolder) set(st State) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.st = st
}

type harness struct {
	bus        *fakeBus
	holder     *stateHolder
	actions    Actions
	cancel     context.CancelFunc
	cancelOnce sync.Once
	errCh      chan error
	finished   bool
	gotErr     error
	ready      chan struct{}
}

func startTray(t *testing.T, st State, actions Actions, handler func(string, []any) ([]any, error)) *harness {
	t.Helper()
	f := newFakeBus(handler)
	base := f.handler
	ready := make(chan struct{})
	var once sync.Once
	f.handler = func(method string, args []any) ([]any, error) {
		if method == "org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem" {
			once.Do(func() { close(ready) })
		}
		return base(method, args)
	}

	holder := &stateHolder{st: st}
	ctx, cancel := context.WithCancel(context.Background())
	h := &harness{
		bus:     f,
		holder:  holder,
		actions: actions,
		cancel:  cancel,
		errCh:   make(chan error, 1),
		ready:   ready,
	}
	go func() {
		h.errCh <- run(ctx, holder.get, actions, func(string, ...any) {}, func(context.Context) (bus, error) {
			return f, nil
		})
	}()
	select {
	case <-ready:
	case err := <-h.errCh:
		cancel()
		t.Fatalf("Run failed before registering: %v", err)
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("timed out waiting for registration")
	}
	t.Cleanup(func() {
		if !h.finished {
			h.shutdown(t)
		}
		if got := h.bus.closeCount(); got != 1 {
			t.Errorf("connection closed %d times, want 1", got)
		}
	})
	return h
}

func (h *harness) shutdown(t *testing.T) error {
	t.Helper()
	h.cancelOnce.Do(h.cancel)
	select {
	case err := <-h.errCh:
		h.finished = true
		h.gotErr = err
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return after cancellation")
		return nil
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// --- Run lifecycle ----------------------------------------------------------

func TestRunLifecycle(t *testing.T) {
	h := startTray(t, State{Title: "NYIST", StatusText: "已连接", Connected: true}, Actions{
		Connect:    func() {},
		Disconnect: func() {},
		Settings:   func() {},
		OpenLog:    func() {},
	}, nil)

	if !h.bus.hasCall("org.freedesktop.DBus.GetNameOwner") {
		t.Error("Run did not probe the watcher")
	}
	if !h.bus.hasCall("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem") {
		t.Error("Run did not register with the watcher")
	}
	nameCall, ok := h.bus.lastCall("org.freedesktop.DBus.RequestName")
	if !ok {
		t.Fatal("Run did not request a bus name")
	}
	wantName := fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	if got := nameCall.Args[0]; got != wantName {
		t.Errorf("bus name = %v, want %s", got, wantName)
	}

	for _, iface := range []string{sniInterface, propertiesIface, introspectIface} {
		if h.bus.exportedObject(sniPath, iface) == nil {
			t.Errorf("%s was not exported at %s", iface, sniPath)
		}
	}
	for _, iface := range []string{dbusmenuInterface, propertiesIface, introspectIface} {
		if h.bus.exportedObject(menuPath, iface) == nil {
			t.Errorf("%s was not exported at %s", iface, menuPath)
		}
	}

	item, ok := h.bus.exportedObject(sniPath, sniInterface).(*sniItem)
	if !ok {
		t.Fatal("sniItem export has the wrong type")
	}
	if err := item.Activate(0, 0); err != nil {
		t.Errorf("Activate: %v", err)
	}

	if err := h.shutdown(t); err != nil {
		t.Fatalf("Run returned %v after cancel, want nil", err)
	}
	if !h.bus.hasCall("org.freedesktop.DBus.ReleaseName") {
		t.Error("Run did not release its bus name")
	}
}

func TestRunCancelIsIdempotent(t *testing.T) {
	h := startTray(t, State{}, Actions{}, nil)
	h.cancelOnce.Do(h.cancel)
	h.cancel()
	if err := h.shutdown(t); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

func TestRunUnavailableWatcher(t *testing.T) {
	f := newFakeBus(func(method string, args []any) ([]any, error) {
		if method == "org.freedesktop.DBus.GetNameOwner" {
			return nil, errors.New("name has no owner")
		}
		return defaultFakeHandler(method, args)
	})
	err := run(context.Background(), func() State { return State{} }, Actions{}, func(string, ...any) {},
		func(context.Context) (bus, error) { return f, nil })
	if !errors.Is(err, ErrNoStatusNotifierHost) {
		t.Fatalf("Run = %v, want ErrNoStatusNotifierHost", err)
	}
	if f.closeCount() != 1 {
		t.Errorf("connection closed %d times, want 1", f.closeCount())
	}
	if f.hasCall("org.freedesktop.DBus.RequestName") {
		t.Error("Run requested a bus name without a watcher")
	}
}

func TestRunDialError(t *testing.T) {
	want := errors.New("no session bus")
	err := run(context.Background(), func() State { return State{} }, Actions{}, func(string, ...any) {},
		func(context.Context) (bus, error) { return nil, want })
	if !errors.Is(err, want) {
		t.Fatalf("Run = %v, want %v", err, want)
	}
}

func TestRunAlreadyRunning(t *testing.T) {
	h := startTray(t, State{}, Actions{}, nil)
	err := run(context.Background(), func() State { return State{} }, Actions{}, func(string, ...any) {},
		func(context.Context) (bus, error) { return newFakeBus(nil), nil })
	if !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second Run = %v, want ErrAlreadyRunning", err)
	}
	if err := h.shutdown(t); err != nil {
		t.Fatalf("first Run = %v, want nil", err)
	}
}

func TestRunNameNotPrimaryOwner(t *testing.T) {
	f := newFakeBus(nil)
	f.nameReply = dbus.RequestNameReplyInQueue
	err := run(context.Background(), func() State { return State{} }, Actions{}, func(string, ...any) {},
		func(context.Context) (bus, error) { return f, nil })
	if err == nil || !strings.Contains(err.Error(), "已被占用") {
		t.Fatalf("Run = %v, want already owned error", err)
	}
}

func TestRunRegisterFailure(t *testing.T) {
	f := newFakeBus(func(method string, args []any) ([]any, error) {
		if method == "org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem" {
			return nil, errors.New("watcher rejected item")
		}
		return defaultFakeHandler(method, args)
	})
	err := run(context.Background(), func() State { return State{} }, Actions{}, func(string, ...any) {},
		func(context.Context) (bus, error) { return f, nil })
	if err == nil || !strings.Contains(err.Error(), "register") {
		t.Fatalf("Run = %v, want registration error", err)
	}
	if f.closeCount() != 1 {
		t.Errorf("connection closed %d times", f.closeCount())
	}
}

func TestRunNilContext(t *testing.T) {
	//lint:ignore SA1012 testing the documented nil guard
	err := run(nil, func() State { return State{} }, Actions{}, func(string, ...any) {}, nil)
	if err == nil {
		t.Fatal("nil context should be rejected")
	}
}

func TestRunReRegisterOnWatcherRestart(t *testing.T) {
	h := startTray(t, State{}, Actions{}, nil)
	waitFor(t, "signal subscription", h.bus.signalSubscribed)
	before := h.bus.callCount("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem")
	if !h.bus.sendSignal(&dbus.Signal{
		Name: "org.freedesktop.DBus.NameOwnerChanged",
		Body: []any{watcherService, "1.1", "1.2"},
	}) {
		t.Fatal("server did not subscribe to signals")
	}
	waitFor(t, "re-registration", func() bool {
		return h.bus.callCount("org.kde.StatusNotifierWatcher.RegisterStatusNotifierItem") > before
	})
}

// --- menu -------------------------------------------------------------------

func menuOf(t *testing.T, h *harness) *dbusMenu {
	t.Helper()
	menu, ok := h.bus.exportedObject(menuPath, dbusmenuInterface).(*dbusMenu)
	if !ok {
		t.Fatal("dbusMenu export has the wrong type")
	}
	return menu
}

func layoutLabel(t *testing.T, menu *dbusMenu, index int) string {
	t.Helper()
	_, layout, dbusErr := menu.GetLayout(0, 1, nil)
	if dbusErr != nil {
		t.Fatalf("GetLayout: %v", dbusErr)
	}
	if index >= len(layout.Children) {
		t.Fatalf("layout has %d children, want index %d", len(layout.Children), index)
	}
	v, ok := layout.Children[index].Value().(menuLayout)
	if !ok {
		t.Fatalf("child %d has type %T", index, layout.Children[index].Value())
	}
	return v.Properties["label"].Value().(string)
}

func TestMenuEventsDispatch(t *testing.T) {
	connect := make(chan struct{}, 1)
	disconnect := make(chan struct{}, 1)
	settings := make(chan struct{}, 1)
	openLog := make(chan struct{}, 1)
	h := startTray(t, State{StatusText: "已断开"}, Actions{
		Connect:    func() { connect <- struct{}{} },
		Disconnect: func() { disconnect <- struct{}{} },
		Settings:   func() { settings <- struct{}{} },
		OpenLog:    func() { openLog <- struct{}{} },
	}, nil)
	menu := menuOf(t, h)

	if err := menu.Event(menuIDSettings, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Fatalf("Event: %v", err)
	}
	if err := menu.Event(menuIDOpenLog, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Fatalf("Event: %v", err)
	}
	if err := menu.Event(menuIDConnect, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Fatalf("Event: %v", err)
	}
	if err := menu.Event(menuIDDisconnect, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Fatalf("Event: %v", err)
	}
	for name, ch := range map[string]chan struct{}{
		"settings": settings, "open log": openLog, "connect": connect,
	} {
		select {
		case <-ch:
		case <-time.After(2 * time.Second):
			t.Fatalf("%s action was not invoked", name)
		}
	}
	select {
	case <-disconnect:
		t.Fatal("disabled disconnect action was invoked")
	case <-time.After(50 * time.Millisecond):
	}

	if err := menu.Event(999, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Errorf("unknown id should be ignored, got %v", err)
	}
	if err := menu.Event(menuIDSettings, "hovered", dbus.MakeVariant(""), 0); err != nil {
		t.Errorf("unknown event should be ignored, got %v", err)
	}
}

func TestMenuQuitWithoutCallbackStopsTray(t *testing.T) {
	h := startTray(t, State{}, Actions{}, nil)
	menu := menuOf(t, h)
	if err := menu.Event(menuIDQuit, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Fatalf("Event: %v", err)
	}
	if err := h.shutdown(t); err != nil {
		t.Fatalf("Run = %v, want nil after quit", err)
	}
}

func TestMenuQuitWithCallback(t *testing.T) {
	quit := make(chan struct{}, 1)
	h := startTray(t, State{}, Actions{Quit: func() { quit <- struct{}{} }}, nil)
	menu := menuOf(t, h)
	if err := menu.Event(menuIDQuit, "clicked", dbus.MakeVariant(""), 0); err != nil {
		t.Fatalf("Event: %v", err)
	}
	select {
	case <-quit:
	case <-time.After(2 * time.Second):
		t.Fatal("quit action was not invoked")
	}
}

func TestMenuLayoutProperties(t *testing.T) {
	h := startTray(t, State{StatusText: "已连接", Connected: true}, Actions{
		Connect: func() {},
	}, nil)

	if got := layoutLabel(t, menuOf(t, h), 0); got != "状态：已连接" {
		t.Errorf("status label = %q", got)
	}

	rev, layout, dbusErr := menuOf(t, h).GetLayout(0, 1, []string{"label", "enabled"})
	if dbusErr != nil {
		t.Fatalf("GetLayout: %v", dbusErr)
	}
	if rev == 0 {
		t.Error("revision should be non-zero")
	}
	if len(layout.Properties) != 0 {
		t.Errorf("filtered root should have no properties, got %v", layout.Properties)
	}
	connect := layout.Children[2].Value().(menuLayout)
	if connect.ID != menuIDConnect {
		t.Fatalf("child 2 id = %d", connect.ID)
	}
	if connect.Properties["enabled"].Value().(bool) {
		t.Error("connect should be disabled while connected")
	}
	if _, ok := connect.Properties["visible"]; ok {
		t.Error("visible was not requested but is present")
	}

	_, partial, _ := menuOf(t, h).GetLayout(5, 1, nil)
	if partial.ID != 5 {
		t.Errorf("non-root parent id = %d", partial.ID)
	}
}

func TestMenuGroupPropertiesAndGetProperty(t *testing.T) {
	h := startTray(t, State{StatusText: "已断开"}, Actions{Connect: func() {}}, nil)
	menu := menuOf(t, h)

	groups, dbusErr := menu.GetGroupProperties([]int32{menuIDConnect, 0}, nil)
	if dbusErr != nil {
		t.Fatalf("GetGroupProperties: %v", dbusErr)
	}
	if len(groups) != 2 || groups[0].ID != 0 || groups[1].ID != menuIDConnect {
		t.Fatalf("groups = %+v", groups)
	}
	if !groups[1].Properties["enabled"].Value().(bool) {
		t.Error("connect should be enabled")
	}

	v, dbusErr := menu.GetProperty(menuIDConnect, "label")
	if dbusErr != nil {
		t.Fatalf("GetProperty: %v", dbusErr)
	}
	if v.Value().(string) != "连接" {
		t.Errorf("label = %v", v.Value())
	}
	if _, dbusErr := menu.GetProperty(menuIDConnect, "nope"); dbusErr == nil {
		t.Error("unknown property should fail")
	}
	if _, dbusErr := menu.GetProperty(menuIDConnect, "children-display"); dbusErr == nil {
		t.Error("children-display belongs to the root only")
	}

	if _, dbusErr := menu.AboutToShow(0); dbusErr != nil {
		t.Errorf("AboutToShow: %v", dbusErr)
	}
	if _, _, dbusErr := menu.AboutToShowGroup([]int32{0}); dbusErr != nil {
		t.Errorf("AboutToShowGroup: %v", dbusErr)
	}
	if errs, dbusErr := menu.EventGroup([]menuEvent{
		{ID: menuIDConnect, EventID: "clicked"},
	}); dbusErr != nil || len(errs) != 0 {
		t.Errorf("EventGroup = %v, %v", errs, dbusErr)
	}
}

func TestMenuProps(t *testing.T) {
	h := startTray(t, State{}, Actions{}, nil)
	props, ok := h.bus.exportedObject(menuPath, propertiesIface).(*menuProps)
	if !ok {
		t.Fatal("menu properties export has the wrong type")
	}
	v, dbusErr := props.Get("", "Version")
	if dbusErr != nil || v.Value().(uint32) != 3 {
		t.Fatalf("Version = %v, %v", v, dbusErr)
	}
	all, dbusErr := props.GetAll("")
	if dbusErr != nil || all["IconThemePath"].Value().([]string) == nil {
		t.Fatalf("GetAll = %v, %v", all, dbusErr)
	}
	if dbusErr := props.Set("", "Version", dbus.MakeVariant(uint32(4))); dbusErr == nil {
		t.Fatal("Set should report a read-only property")
	}
	if _, dbusErr := props.Get("", "nope"); dbusErr == nil {
		t.Fatal("unknown property should fail")
	}
}

// --- SNI --------------------------------------------------------------------

func TestSNIProps(t *testing.T) {
	h := startTray(t, State{Title: "NYIST", StatusText: "认证失败", Busy: true}, Actions{}, nil)
	props, ok := h.bus.exportedObject(sniPath, propertiesIface).(*sniProps)
	if !ok {
		t.Fatal("SNI properties export has the wrong type")
	}

	status, dbusErr := props.Get(sniInterface, "Status")
	if dbusErr != nil || status.Value().(string) != "NeedsAttention" {
		t.Fatalf("Status = %v, %v", status, dbusErr)
	}
	title, _ := props.Get(sniInterface, "Title")
	if title.Value().(string) != "NYIST" {
		t.Errorf("Title = %v", title.Value())
	}
	menuV, _ := props.Get(sniInterface, "Menu")
	if menuV.Value().(dbus.ObjectPath) != menuPath {
		t.Errorf("Menu = %v", menuV.Value())
	}
	all, dbusErr := props.GetAll(sniInterface)
	if dbusErr != nil || len(all) < 15 {
		t.Fatalf("GetAll returned %d properties, %v", len(all), dbusErr)
	}
	if _, ok := all["ItemIsMenu"]; !ok {
		t.Error("ItemIsMenu missing")
	}
	tooltip, ok := all["ToolTip"].Value().(sniTooltip)
	if !ok || tooltip.Title != "NYIST" || tooltip.Subtitle != "认证失败" {
		t.Errorf("ToolTip = %+v (%T)", all["ToolTip"].Value(), all["ToolTip"].Value())
	}
	if _, dbusErr := props.Get("other.Interface", "Status"); dbusErr == nil {
		t.Error("unknown interface should fail")
	}
	if dbusErr := props.Set(sniInterface, "Title", dbus.MakeVariant("x")); dbusErr == nil {
		t.Error("SNI properties are read-only")
	}
}

func TestStatusForState(t *testing.T) {
	cases := []struct {
		st   State
		want string
	}{
		{State{}, "Active"},
		{State{StatusText: "已停止"}, "Active"},
		{State{Busy: true}, "NeedsAttention"},
		{State{Connected: true}, "Active"},
		{State{Busy: true, Connected: true}, "NeedsAttention"},
	}
	for _, tc := range cases {
		if got := statusForState(tc.st); got != tc.want {
			t.Errorf("statusForState(%+v) = %q, want %q", tc.st, got, tc.want)
		}
	}
}

func TestSNIItemNoops(t *testing.T) {
	h := startTray(t, State{}, Actions{}, nil)
	item := h.bus.exportedObject(sniPath, sniInterface).(*sniItem)
	if err := item.ContextMenu(0, 0); err != nil {
		t.Error(err)
	}
	if err := item.SecondaryActivate(0, 0); err != nil {
		t.Error(err)
	}
	if err := item.Scroll(1, "vertical"); err != nil {
		t.Error(err)
	}
	if err := item.ProvideXdgActivationToken("tok"); err != nil {
		t.Error(err)
	}
}

func TestIntrospectionNodes(t *testing.T) {
	sni := sniNode()
	menu := menuNode()
	if len(sni.Interfaces) != 3 || len(menu.Interfaces) != 3 {
		t.Fatalf("interfaces: sni=%d menu=%d", len(sni.Interfaces), len(menu.Interfaces))
	}
	if sni.Interfaces[0].Name != sniInterface || menu.Interfaces[0].Name != dbusmenuInterface {
		t.Errorf("unexpected first interface: %q %q", sni.Interfaces[0].Name, menu.Interfaces[0].Name)
	}
}

// --- refresh ----------------------------------------------------------------

func TestRefreshUpdatesMenu(t *testing.T) {
	h := startTray(t, State{StatusText: "已连接", Connected: true}, Actions{}, nil)
	if got := layoutLabel(t, menuOf(t, h), 0); got != "状态：已连接" {
		t.Fatalf("initial label = %q", got)
	}
	h.holder.set(State{StatusText: "已断开"})
	Refresh()
	waitFor(t, "menu refresh", func() bool {
		_, layout, dbusErr := menuOf(t, h).GetLayout(0, 1, nil)
		if dbusErr != nil || len(layout.Children) == 0 {
			return false
		}
		props := layout.Children[0].Value().(menuLayout).Properties
		return props["label"].Value().(string) == "状态：已断开"
	})
}

func TestRefreshWithoutTrayIsNoop(t *testing.T) {
	Refresh() // must not panic or block
}

// --- notifications ----------------------------------------------------------

func TestNotifyDBus(t *testing.T) {
	var mu sync.Mutex
	var got mockNotifyArgs
	f := newFakeBus(func(method string, args []any) ([]any, error) {
		if method == "org.freedesktop.Notifications.Notify" {
			mu.Lock()
			got = parseNotifyArgs(args)
			mu.Unlock()
			return []any{uint32(1)}, nil
		}
		return defaultFakeHandler(method, args)
	})
	err := notify(context.Background(), func(context.Context) (bus, error) { return f, nil },
		"认证失败", "密码错误 <b>", true)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if got.summary != "认证失败" {
		t.Errorf("summary = %q", got.summary)
	}
	if got.body != "密码错误 &lt;b&gt;" {
		t.Errorf("body = %q", got.body)
	}
	if got.urgency != byte(2) {
		t.Errorf("urgency = %d, want 2", got.urgency)
	}
	if got.app != appName {
		t.Errorf("app = %q", got.app)
	}
	if f.closeCount() != 1 {
		t.Errorf("notification connection closed %d times", f.closeCount())
	}
}

type mockNotifyArgs struct {
	app, summary, body string
	urgency            byte
}

func parseNotifyArgs(args []any) mockNotifyArgs {
	out := mockNotifyArgs{}
	if len(args) < 8 {
		return out
	}
	out.app, _ = args[0].(string)
	out.summary, _ = args[3].(string)
	out.body, _ = args[4].(string)
	if hints, ok := args[6].(map[string]dbus.Variant); ok {
		out.urgency, _ = hints["urgency"].Value().(byte)
	}
	return out
}

func TestNotifyFallsBackToNotifySend(t *testing.T) {
	oldPath, oldRun := notifyLookPath, notifyRun
	defer func() { notifyLookPath, notifyRun = oldPath, oldRun }()

	var gotName string
	var gotArgs []string
	notifyLookPath = func(string) (string, error) { return "/usr/bin/notify-send", nil }
	notifyRun = func(ctx context.Context, name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	}

	dialErr := errors.New("no bus")
	err := notify(context.Background(), func(context.Context) (bus, error) { return nil, dialErr },
		"标题", "正文 <x>", true)
	if err != nil {
		t.Fatalf("notify: %v", err)
	}
	if gotName != "/usr/bin/notify-send" {
		t.Errorf("binary = %q", gotName)
	}
	want := []string{"--app-name=" + appName, "--urgency=critical", "--", "标题", "正文 &lt;x&gt;"}
	if strings.Join(gotArgs, "\x00") != strings.Join(want, "\x00") {
		t.Errorf("args = %q, want %q", gotArgs, want)
	}
}

func TestNotifyNoDaemonNoNotifySend(t *testing.T) {
	oldPath, oldRun := notifyLookPath, notifyRun
	defer func() { notifyLookPath, notifyRun = oldPath, oldRun }()
	notifyLookPath = func(string) (string, error) { return "", errors.New("not found") }
	called := false
	notifyRun = func(context.Context, string, ...string) error { called = true; return nil }

	err := notify(context.Background(), func(context.Context) (bus, error) { return nil, errors.New("no bus") },
		"t", "b", false)
	if err == nil {
		t.Fatal("expected an error when nothing is available")
	}
	if called {
		t.Error("notify-send must not run when LookPath fails")
	}
}

func TestNotifyContextCanceledSkipsFallback(t *testing.T) {
	oldPath, oldRun := notifyLookPath, notifyRun
	defer func() { notifyLookPath, notifyRun = oldPath, oldRun }()
	notifyLookPath = func(string) (string, error) { return "/usr/bin/notify-send", nil }
	called := false
	notifyRun = func(context.Context, string, ...string) error { called = true; return nil }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := notify(ctx, func(context.Context) (bus, error) { return nil, errors.New("no bus") }, "t", "b", false); err == nil {
		t.Fatal("expected an error")
	}
	if called {
		t.Error("fallback must not run after context cancellation")
	}
}

func TestNotifyRejectsUrgencyVariants(t *testing.T) {
	if urgencyByte(false) != 1 || urgencyByte(true) != 2 {
		t.Fatal("unexpected urgency mapping")
	}
}

// --- Available --------------------------------------------------------------

func TestAvailable(t *testing.T) {
	f := newFakeBus(nil)
	if err := available(context.Background(), func(context.Context) (bus, error) { return f, nil }); err != nil {
		t.Fatalf("available: %v", err)
	}
	if f.closeCount() != 1 {
		t.Errorf("probe connection closed %d times", f.closeCount())
	}

	noWatcher := newFakeBus(func(method string, args []any) ([]any, error) {
		return nil, errors.New("no such name")
	})
	if err := available(context.Background(), func(context.Context) (bus, error) { return noWatcher, nil }); !errors.Is(err, ErrNoStatusNotifierHost) {
		t.Fatalf("available = %v, want ErrNoStatusNotifierHost", err)
	}

	dialErr := errors.New("no bus")
	if err := available(context.Background(), func(context.Context) (bus, error) { return nil, dialErr }); !errors.Is(err, dialErr) {
		t.Fatalf("available = %v, want dial error", err)
	}
}
