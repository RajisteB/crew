package mission

import (
	"path"
	"strings"
)

const wildcardChars = "*?["

// MatchPath reports whether file (slash-separated, relative to the repo root)
// matches pattern. "**" matches any number of path segments, including none;
// "*" and "?" match inside one segment, as path.Match does. A pattern with no
// wildcard also matches everything under it as a folder. A malformed pattern
// matches nothing. A leading "./" or trailing "/" on either side is ignored.
func MatchPath(pattern, file string) bool {
	pattern, file = cleanPath(pattern), cleanPath(file)
	if pattern == "" || file == "" {
		return false
	}
	pat := strings.Split(pattern, "/")
	for _, seg := range pat {
		if seg != "**" && !validSegment(seg) {
			return false
		}
	}
	parts := strings.Split(file, "/")
	if !strings.ContainsAny(pattern, wildcardChars) {
		return len(parts) >= len(pat) && matchSegments(pat, parts[:len(pat)])
	}
	return matchSegments(pat, parts)
}

func cleanPath(p string) string {
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return strings.TrimRight(p, "/")
}

func validSegment(seg string) bool {
	_, err := path.Match(seg, "")
	return err == nil
}

func matchSegments(pat, parts []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			rest := pat[1:]
			for i := 0; i <= len(parts); i++ {
				if matchSegments(rest, parts[i:]) {
					return true
				}
			}
			return false
		}
		if len(parts) == 0 {
			return false
		}
		if ok, err := path.Match(pat[0], parts[0]); err != nil || !ok {
			return false
		}
		pat, parts = pat[1:], parts[1:]
	}
	return len(parts) == 0
}
