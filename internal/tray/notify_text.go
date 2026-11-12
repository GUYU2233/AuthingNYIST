package tray

import (
	"strings"
	"unicode/utf8"
)

const (
	maxNotificationTitle = 128  // runes
	maxNotificationBody  = 1024 // runes
)

var markupEscaper = strings.NewReplacer(
	"&", "&amp;",
	"<", "&lt;",
	">", "&gt;",
)

// sanitizeNotificationText makes s safe for a D-Bus string: invalid UTF-8 is
// replaced, NUL and other C0/C1 control characters are dropped (newline and
// tab survive), CRLF is normalised to LF and the result is truncated to maxRunes
// runes with a trailing ellipsis.
func sanitizeNotificationText(s string, maxRunes int) string {
	s = strings.ToValidUTF8(s, "\uFFFD")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\t':
			return r
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return -1
		}
		return r
	}, s)
	s = strings.TrimSpace(s)
	if maxRunes <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	runes := []rune(s)
	return strings.TrimRight(string(runes[:maxRunes-1]), " \t\n") + "…"
}

// escapeNotificationMarkup escapes the three characters that notification
// demos may interpret as the start of HTML markup. Bodies are escaped once,
// after truncation, so the escaped text still fits the size limit.
func escapeNotificationMarkup(s string) string {
	return markupEscaper.Replace(s)
}
