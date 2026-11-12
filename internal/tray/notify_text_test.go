package tray

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSanitizeNotificationText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		max  int
		want string
	}{
		{"plain", "认证失败", 100, "认证失败"},
		{"nul", "a\x00b", 100, "ab"},
		{"bell", "a\x07b", 100, "ab"},
		{"c1", "a\u0085b", 100, "ab"},
		{"crlf", "a\r\nb", 100, "a\nb"},
		{"cr", "a\rb", 100, "a\nb"},
		{"trim", "  hello \n", 100, "hello"},
		{"tab kept", "a\tb", 100, "a\tb"},
		{"invalid utf8", "a\xffb", 100, "a\uFFFDb"},
		{"empty max", "hello", 0, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizeNotificationText(tc.in, tc.max); got != tc.want {
				t.Fatalf("sanitize(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeNotificationTruncation(t *testing.T) {
	long := strings.Repeat("校", maxNotificationBody+50)
	got := sanitizeNotificationText(long, maxNotificationBody)
	if utf8.RuneCountInString(got) != maxNotificationBody {
		t.Fatalf("truncated to %d runes, want %d", utf8.RuneCountInString(got), maxNotificationBody)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatal("truncated text should end with an ellipsis")
	}
	if strings.HasPrefix(got, "\xff") {
		t.Fatal("invalid prefix")
	}

	title := sanitizeNotificationText(strings.Repeat("a", maxNotificationTitle+1), maxNotificationTitle)
	if utf8.RuneCountInString(title) != maxNotificationTitle {
		t.Fatalf("title truncated to %d runes", utf8.RuneCountInString(title))
	}
}

func TestEscapeNotificationMarkup(t *testing.T) {
	cases := map[string]string{
		"plain":        "plain",
		"<b>bold</b>":  "&lt;b&gt;bold&lt;/b&gt;",
		"a & b":        "a &amp; b",
		"&lt;":         "&amp;lt;",
		"<img src=x>":  "&lt;img src=x&gt;",
		"no markup 中文": "no markup 中文",
	}
	for in, want := range cases {
		if got := escapeNotificationMarkup(in); got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
		}
	}
}
