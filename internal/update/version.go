package update

import (
	"fmt"
	"strings"
)

type version struct {
	core [3]string
	pre  []string
}

// CompareVersions compares semantic versions, accepting an optional leading v.
// It returns -1, 0 or 1. Build metadata does not affect precedence. Unknown
// development strings (such as "dev") return an error, never a fake version.
func CompareVersions(a, b string) (int, error) {
	av, err := parseVersion(a)
	if err != nil {
		return 0, err
	}
	bv, err := parseVersion(b)
	if err != nil {
		return 0, err
	}
	return compare(av, bv), nil
}

func numeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
func identifiers(s string, prerelease bool) ([]string, error) {
	parts := strings.Split(s, ".")
	for _, p := range parts {
		if p == "" {
			return nil, fmt.Errorf("empty version identifier")
		}
		for _, c := range p {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
				return nil, fmt.Errorf("invalid version identifier %q", p)
			}
		}
		if prerelease && numeric(p) && len(p) > 1 && p[0] == '0' {
			return nil, fmt.Errorf("leading zero in prerelease %q", p)
		}
	}
	return parts, nil
}
func parseVersion(s string) (version, error) {
	var v version
	s = strings.TrimPrefix(s, "v")
	if base, meta, ok := strings.Cut(s, "+"); ok {
		if _, err := identifiers(meta, false); err != nil {
			return v, err
		}
		s = base
	}
	if base, pre, ok := strings.Cut(s, "-"); ok {
		ids, err := identifiers(pre, true)
		if err != nil {
			return v, err
		}
		v.pre = ids
		s = base
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return v, fmt.Errorf("expected semantic version major.minor.patch")
	}
	for i, p := range parts {
		if !numeric(p) || len(p) > 1 && p[0] == '0' {
			return v, fmt.Errorf("invalid numeric version component %q", p)
		}
		v.core[i] = p
	}
	return v, nil
}
func cmpString(a, b string) int {
	if a < b {
		return -1
	}
	if a > b {
		return 1
	}
	return 0
}
func cmpNumber(a, b string) int {
	if len(a) < len(b) {
		return -1
	}
	if len(a) > len(b) {
		return 1
	}
	return cmpString(a, b)
}
func compare(a, b version) int {
	for i := range a.core {
		if c := cmpNumber(a.core[i], b.core[i]); c != 0 {
			return c
		}
	}
	if len(a.pre) == 0 && len(b.pre) == 0 {
		return 0
	}
	if len(a.pre) == 0 {
		return 1
	}
	if len(b.pre) == 0 {
		return -1
	}
	for i := 0; i < len(a.pre) && i < len(b.pre); i++ {
		x, y := a.pre[i], b.pre[i]
		xn, yn := numeric(x), numeric(y)
		var c int
		if xn && yn {
			c = cmpNumber(x, y)
		} else if xn {
			c = -1
		} else if yn {
			c = 1
		} else {
			c = cmpString(x, y)
		}
		if c != 0 {
			return c
		}
	}
	if len(a.pre) < len(b.pre) {
		return -1
	}
	if len(a.pre) > len(b.pre) {
		return 1
	}
	return 0
}
