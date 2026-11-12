package tray

import (
	"strings"
	"testing"
)

func TestStatusLabel(t *testing.T) {
	cases := []struct {
		name string
		st   State
		want string
	}{
		{"empty", State{}, "状态：未知"},
		{"spaces", State{StatusText: "   "}, "状态：未知"},
		{"text", State{StatusText: "已连接"}, "状态：已连接"},
		{"trimmed", State{StatusText: "  认证失败\n"}, "状态：认证失败"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.st.StatusLabel(); got != tc.want {
				t.Fatalf("StatusLabel() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildMenuLabelsAndOrder(t *testing.T) {
	actions := Actions{
		Connect:    func() {},
		Disconnect: func() {},
		Settings:   func() {},
		OpenLog:    func() {},
		Quit:       func() {},
	}
	entries := buildMenu(State{StatusText: "已连接", Connected: true}, actions)

	wantLabels := []string{"状态：已连接", "", "连接", "断开连接", "", "设置", "打开日志", "", "退出"}
	if len(entries) != len(wantLabels) {
		t.Fatalf("got %d entries, want %d", len(entries), len(wantLabels))
	}
	seen := make(map[int32]bool, len(entries))
	for i, e := range entries {
		if e.Label != wantLabels[i] {
			t.Errorf("entry %d label = %q, want %q", i, e.Label, wantLabels[i])
		}
		if seen[e.ID] {
			t.Errorf("duplicate menu id %d", e.ID)
		}
		seen[e.ID] = true
		if !e.Visible {
			t.Errorf("entry %d (%q) should be visible", i, e.Label)
		}
	}
	for _, i := range []int{1, 3, 4, 6, 7, 9} {
		if !seen[int32(i)] {
			t.Errorf("missing menu id %d", i)
		}
	}
	separators := []int{1, 4, 7}
	for _, i := range separators {
		if entries[i].Kind != kindSeparator {
			t.Errorf("entry %d should be a separator", i)
		}
		if entries[i].Enabled {
			t.Errorf("separator %d must be disabled", i)
		}
	}
	if entries[0].Enabled {
		t.Error("status item must be disabled")
	}
	if !entries[8].Enabled {
		t.Error("quit item must stay enabled")
	}
}

func TestBuildMenuEnabledState(t *testing.T) {
	all := Actions{
		Connect:    func() {},
		Disconnect: func() {},
		Settings:   func() {},
		OpenLog:    func() {},
		Quit:       func() {},
	}
	connectOf := func(e []menuEntry) menuEntry { return e[2] }
	disconnectOf := func(e []menuEntry) menuEntry { return e[3] }

	t.Run("disconnected", func(t *testing.T) {
		e := buildMenu(State{}, all)
		if !connectOf(e).Enabled || disconnectOf(e).Enabled {
			t.Fatalf("connect=%v disconnect=%v", connectOf(e).Enabled, disconnectOf(e).Enabled)
		}
	})
	t.Run("connected", func(t *testing.T) {
		e := buildMenu(State{Connected: true}, all)
		if connectOf(e).Enabled || !disconnectOf(e).Enabled {
			t.Fatalf("connect=%v disconnect=%v", connectOf(e).Enabled, disconnectOf(e).Enabled)
		}
	})
	t.Run("busy", func(t *testing.T) {
		e := buildMenu(State{Connected: true, Busy: true}, all)
		if connectOf(e).Enabled || disconnectOf(e).Enabled {
			t.Fatalf("busy must disable connect and disconnect")
		}
	})
	t.Run("nil actions", func(t *testing.T) {
		e := buildMenu(State{}, Actions{})
		for i, entry := range e {
			if entry.Kind == kindSeparator || entry.ID == menuIDStatus || entry.ID == menuIDQuit {
				continue
			}
			if entry.Enabled {
				t.Errorf("entry %d (%q) must be disabled without an action", i, entry.Label)
			}
		}
	})
}

func TestMenuSignature(t *testing.T) {
	entries := buildMenu(State{StatusText: "已连接"}, Actions{})
	if got := menuSignature(entries); got != menuSignature(entries) {
		t.Fatal("signature is not deterministic")
	}
	changed := buildMenu(State{StatusText: "已断开"}, Actions{})
	if menuSignature(entries) == menuSignature(changed) {
		t.Fatal("signature did not change with the status label")
	}
	withAction := buildMenu(State{StatusText: "已连接"}, Actions{Connect: func() {}})
	if menuSignature(entries) == menuSignature(withAction) {
		t.Fatal("signature did not change when connect became available")
	}
}

func TestMenuItemByID(t *testing.T) {
	entries := buildMenu(State{}, Actions{})
	if _, ok := menuItemByID(entries, 999); ok {
		t.Fatal("unexpected item for unknown id")
	}
	e, ok := menuItemByID(entries, menuIDSettings)
	if !ok || e.Label != "设置" {
		t.Fatalf("got %+v, ok=%v", e, ok)
	}
	if !strings.Contains(menuSignature(entries), "设置") {
		t.Fatal("signature should contain labels")
	}
}
