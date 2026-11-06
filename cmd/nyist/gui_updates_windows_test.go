//go:build windows

package main

import (
	"strings"
	"testing"

	"github.com/Palvef/AuthingNYIST/internal/update"
)

func TestUpdateMessage(t *testing.T) {
	tests := []struct {
		name   string
		result update.Result
		want   string
	}{
		{"development", update.Result{CurrentVersion: "dev", Latest: update.Release{TagName: "v1.2.0"}}, "无法自动判断升级"},
		{"available", update.Result{CurrentVersion: "v1.0.0", CurrentKnown: true, UpdateAvailable: true, Latest: update.Release{TagName: "v1.2.0"}}, "发现新版本 v1.2.0"},
		{"current", update.Result{CurrentVersion: "v1.2.0", CurrentKnown: true, Latest: update.Release{TagName: "v1.2.0"}}, "无需更新"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := updateMessage(&tt.result)
			if !strings.Contains(got, tt.want) || !strings.Contains(got, tt.result.CurrentVersion) || !strings.Contains(got, tt.result.Latest.TagName) {
				t.Fatalf("unexpected update message: %q", got)
			}
		})
	}
}
