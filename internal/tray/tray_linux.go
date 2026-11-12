//go:build linux

package tray

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/introspect"
)

const (
	sniInterface      = "org.kde.StatusNotifierItem"
	dbusmenuInterface = "com.canonical.dbusmenu"
	propertiesIface   = "org.freedesktop.DBus.Properties"
	introspectIface   = "org.freedesktop.DBus.Introspectable"

	watcherService = "org.kde.StatusNotifierWatcher"
	watcherPath    = dbus.ObjectPath("/StatusNotifierWatcher")

	sniPath  = dbus.ObjectPath("/StatusNotifierItem")
	menuPath = dbus.ObjectPath("/MenuBar")

	callTimeout = 3 * time.Second
	pollPeriod  = time.Second
)

// bus is the subset of *dbus.Conn the tray uses. Keep it small: tests inject a
// fake and never need a session bus.
type bus interface {
	Export(v any, path dbus.ObjectPath, iface string) error
	Emit(path dbus.ObjectPath, name string, values ...any) error
	Signal(ch chan<- *dbus.Signal)
	RemoveSignal(ch chan<- *dbus.Signal)
	Object(dest string, path dbus.ObjectPath) dbus.BusObject
	RequestName(name string, flags dbus.RequestNameFlags) (dbus.RequestNameReply, error)
	ReleaseName(name string) (dbus.ReleaseNameReply, error)
	Close() error
}

type dialFunc func(context.Context) (bus, error)

func dialSession(ctx context.Context) (bus, error) {
	conn, err := dbus.ConnectSessionBus(dbus.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("tray: 无法连接会话总线: %w", err)
	}
	return conn, nil
}

// activeTray is the process-wide tray used by Refresh. Only one tray per
// process is supported; see ErrAlreadyRunning.
var activeTray atomic.Pointer[server]

// Refresh asks the running tray to re-read the state callback and update the
// icon, tooltip and menu immediately. It never blocks and is a no-op if no tray
// is running.
func Refresh() {
	if s := activeTray.Load(); s != nil {
		s.refresh()
	}
}

// Available reports whether a StatusNotifierItem tray can be shown on the
// current session, i.e. whether the session bus is reachable and exports
// org.kde.StatusNotifierWatcher. It returns ErrNoStatusNotifierHost when the
// watcher is missing (for example in a bare SSH session, or GNOME Shell without
// the AppIndicator/KStatusNotifierItem extension).
func Available() error {
	ctx, cancel := context.WithTimeout(context.Background(), callTimeout)
	defer cancel()
	return available(ctx, dialSession)
}

func available(ctx context.Context, dial dialFunc) error {
	conn, err := dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	s := &server{conn: conn}
	return s.probeWatcher(ctx)
}

// Run shows the tray icon and blocks until ctx is canceled, the "退出" menu item
// is activated while Actions.Quit is nil, or setup fails. It returns nil for a
// normal shutdown and an error (possibly wrapping ErrNoStatusNotifierHost)
// otherwise.
//
// state is polled at startup, on every Refresh call and once per second; see the
// package documentation. actions and state are called from multiple goroutines.
// logger may be nil and must be safe for concurrent use.
func Run(ctx context.Context, state func() State, actions Actions, logger func(string, ...any)) error {
	if logger == nil {
		logger = func(string, ...any) {}
	}
	if state == nil {
		state = func() State { return State{} }
	}
	return run(ctx, state, actions, logger, dialSession)
}

func run(ctx context.Context, state func() State, actions Actions, log func(string, ...any), dial dialFunc) error {
	if ctx == nil {
		return errors.New("tray: nil context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	conn, err := dial(ctx)
	if err != nil {
		return err
	}
	s := &server{
		conn:      conn,
		state:     state,
		actions:   actions,
		log:       log,
		stopCh:    make(chan struct{}),
		refreshCh: make(chan struct{}, 1),
	}
	if !activeTray.CompareAndSwap(nil, s) {
		_ = conn.Close()
		return ErrAlreadyRunning
	}
	defer activeTray.CompareAndSwap(s, nil)
	defer conn.Close()

	if err := s.probeWatcher(ctx); err != nil {
		return err
	}

	s.busName = fmt.Sprintf("org.kde.StatusNotifierItem-%d-1", os.Getpid())
	reply, err := conn.RequestName(s.busName, dbus.NameFlagDoNotQueue)
	if err != nil {
		return fmt.Errorf("tray: 申请总线名 %s 失败: %w", s.busName, err)
	}
	if reply != dbus.RequestNameReplyPrimaryOwner {
		return fmt.Errorf("tray: 总线名 %s 已被占用", s.busName)
	}
	defer func() { _, _ = conn.ReleaseName(s.busName) }()

	s.initState()

	if err := s.export(); err != nil {
		return err
	}
	if err := s.register(ctx); err != nil {
		return err
	}
	if err := s.addWatcherMatch(ctx); err != nil {
		log("tray: cannot watch StatusNotifierWatcher lifecycle: %v", err)
	}
	return s.loop(ctx)
}

// server owns the exported objects and the state cache.
type server struct {
	conn    bus
	busName string
	state   func() State
	actions Actions
	log     func(string, ...any)

	stopCh    chan struct{}
	stopOnce  sync.Once
	refreshCh chan struct{}

	revision atomic.Uint32

	mu      sync.Mutex
	last    State
	menuSig string
}

func (s *server) refresh() {
	select {
	case s.refreshCh <- struct{}{}:
	default:
	}
}

func (s *server) stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *server) initState() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = s.safeState()
	s.menuSig = menuSignature(buildMenu(s.last, s.actions))
	s.revision.Add(1)
}

// safeState calls the caller-supplied callback and turns a panic into a log
// line, keeping the previous state. A broken callback must not kill the tray.
func (s *server) safeState() (st State) {
	defer func() {
		if r := recover(); r != nil {
			s.log("tray: state callback panicked: %v", r)
			st = s.last
		}
	}()
	return s.state()
}

func (s *server) currentState() State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func (s *server) probeWatcher(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	obj := s.conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.GetNameOwner", 0, watcherService)
	if call.Err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("%w: %v", ErrNoStatusNotifierHost, call.Err)
	}
	return nil
}

func (s *server) register(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	obj := s.conn.Object(watcherService, watcherPath)
	method := watcherService + ".RegisterStatusNotifierItem"
	call := obj.CallWithContext(ctx, method, 0, s.busName)
	if call.Err == nil {
		return nil
	}
	firstErr := call.Err
	// Some watchers only accept an object path; try both spellings.
	call = obj.CallWithContext(ctx, method, 0, string(sniPath))
	if call.Err == nil {
		return nil
	}
	return fmt.Errorf("tray: register with %s: %w", watcherService, firstErr)
}

func (s *server) addWatcherMatch(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	rule := "type='signal',interface='org.freedesktop.DBus',member='NameOwnerChanged'," +
		"path='/org/freedesktop/DBus',arg0='" + watcherService + "'"
	obj := s.conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus")
	call := obj.CallWithContext(ctx, "org.freedesktop.DBus.AddMatch", 0, rule)
	return call.Err
}

func (s *server) export() error {
	if err := s.conn.Export(&sniItem{srv: s}, sniPath, sniInterface); err != nil {
		return fmt.Errorf("tray: export %s: %w", sniInterface, err)
	}
	if err := s.conn.Export(&sniProps{srv: s}, sniPath, propertiesIface); err != nil {
		return fmt.Errorf("tray: export sni properties: %w", err)
	}
	if err := s.conn.Export(introspect.NewIntrospectable(sniNode()), sniPath, introspectIface); err != nil {
		return fmt.Errorf("tray: export sni introspection: %w", err)
	}
	if err := s.conn.Export(&dbusMenu{srv: s}, menuPath, dbusmenuInterface); err != nil {
		return fmt.Errorf("tray: export %s: %w", dbusmenuInterface, err)
	}
	if err := s.conn.Export(&menuProps{srv: s}, menuPath, propertiesIface); err != nil {
		return fmt.Errorf("tray: export menu properties: %w", err)
	}
	if err := s.conn.Export(introspect.NewIntrospectable(menuNode()), menuPath, introspectIface); err != nil {
		return fmt.Errorf("tray: export menu introspection: %w", err)
	}
	return nil
}

func (s *server) loop(ctx context.Context) error {
	signals := make(chan *dbus.Signal, 16)
	s.conn.Signal(signals)
	defer s.conn.RemoveSignal(signals)

	ticker := time.NewTicker(pollPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-s.stopCh:
			return nil
		case <-s.refreshCh:
			s.sync()
		case <-ticker.C:
			s.sync()
		case sig, ok := <-signals:
			if !ok {
				signals = nil
				continue
			}
			s.handleSignal(ctx, sig)
		}
	}
}

func (s *server) handleSignal(ctx context.Context, sig *dbus.Signal) {
	if sig == nil || sig.Name != "org.freedesktop.DBus.NameOwnerChanged" {
		return
	}
	if len(sig.Body) != 3 {
		return
	}
	name, _ := sig.Body[0].(string)
	if name != watcherService {
		return
	}
	newOwner, _ := sig.Body[2].(string)
	if newOwner == "" {
		s.log("tray: %s disappeared; waiting for it to return", watcherService)
		return
	}
	// A restarted watcher forgot about us.
	if err := s.register(ctx); err != nil {
		s.log("tray: re-register after watcher restart: %v", err)
	}
}

// sync re-reads the state, emits property/signal changes and bumps the menu
// revision when the layout changed.
func (s *server) sync() {
	st := s.safeState()
	entries := buildMenu(st, s.actions)
	sig := menuSignature(entries)

	s.mu.Lock()
	old, oldSig := s.last, s.menuSig
	s.last, s.menuSig = st, sig
	s.mu.Unlock()

	if old == st {
		if oldSig != sig {
			s.emitMenuUpdated()
		}
		return
	}
	s.emitState(old, st)
	if oldSig != sig {
		s.emitMenuUpdated()
	}
}

func (s *server) emitState(old, st State) {
	changed := make(map[string]dbus.Variant, 3)
	oldStatus, newStatus := statusForState(old), statusForState(st)
	if titleOf(old) != titleOf(st) {
		changed["Title"] = dbus.MakeVariant(titleOf(st))
		if err := s.conn.Emit(sniPath, sniInterface+".NewTitle"); err != nil {
			s.log("tray: emit NewTitle: %v", err)
		}
	}
	if oldStatus != newStatus {
		changed["Status"] = dbus.MakeVariant(newStatus)
		if err := s.conn.Emit(sniPath, sniInterface+".NewStatus", newStatus); err != nil {
			s.log("tray: emit NewStatus: %v", err)
		}
	}
	if titleOf(old) != titleOf(st) || old.StatusText != st.StatusText {
		changed["ToolTip"] = dbus.MakeVariant(tooltipFor(st))
		if err := s.conn.Emit(sniPath, sniInterface+".NewToolTip"); err != nil {
			s.log("tray: emit NewToolTip: %v", err)
		}
	}
	if len(changed) == 0 {
		return
	}
	if err := s.conn.Emit(sniPath, propertiesIface+".PropertiesChanged", sniInterface, changed, []string{}); err != nil {
		s.log("tray: emit PropertiesChanged: %v", err)
	}
}

func (s *server) emitMenuUpdated() {
	// AppIndicator updates existing items through ItemsPropertiesUpdated;
	// LayoutUpdated alone is not sufficient for cached labels and sensitivity.
	entries := buildMenu(s.currentState(), s.actions)
	updates := groupProperties(entries, nil, nil)
	if err := s.conn.Emit(menuPath, dbusmenuInterface+".ItemsPropertiesUpdated", updates, []menuRemovedProperties{}); err != nil {
		s.log("tray: emit ItemsPropertiesUpdated: %v", err)
	}
	rev := s.revision.Add(1)
	if err := s.conn.Emit(menuPath, dbusmenuInterface+".LayoutUpdated", rev, int32(0)); err != nil {
		s.log("tray: emit LayoutUpdated: %v", err)
	}
}

// dispatch runs a menu action on its own goroutine, recovering panics so a bad
// callback cannot take down the tray.
func (s *server) dispatch(name string, fn func()) {
	if fn == nil {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.log("tray: %s action panicked: %v", name, r)
			}
		}()
		fn()
	}()
}
