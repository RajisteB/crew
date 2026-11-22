package source

import (
	"fmt"
	"path"
	"strings"
)

// CheckPaths reports the first entry of a domain's paths that could never
// match what the author meant: an empty or absolute pattern, an empty or ..
// segment, or a segment path.Match rejects. A leading ./ and a trailing / are
// fine.
func CheckPaths(patterns []string) error {
	for _, p := range patterns {
		if why := badPath(p); why != "" {
			return fmt.Errorf("paths: %q: %s", p, why)
		}
	}
	return nil
}

func badPath(p string) string {
	if p == "" {
		return "is empty"
	}
	if strings.HasPrefix(p, "/") {
		return "must be relative to the repo root (no leading /)"
	}
	rest := strings.TrimSuffix(strings.TrimPrefix(p, "./"), "/")
	if rest == "" {
		return "is empty"
	}
	for _, seg := range strings.Split(rest, "/") {
		switch {
		case seg == "":
			return "has an empty segment"
		case seg == "..":
			return "must not contain .."
		}
		if _, err := path.Match(seg, ""); err != nil {
			return "is not a valid pattern"
		}
	}
	return ""
}
