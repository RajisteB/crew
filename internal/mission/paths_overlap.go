package mission

import (
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// EffectivePaths is where a piece may change files: its own paths, or its
// domain's when it lists none. nil means the piece can't be checked.
func EffectivePaths(p Piece, d source.Domain) []string {
	if len(p.Paths) > 0 {
		return p.Paths
	}
	if len(d.Paths) > 0 {
		return d.Paths
	}
	return nil
}

// PatternsOverlap reports whether one file could match both patterns. When it
// can't tell, it says yes. A malformed or empty pattern overlaps nothing.
func PatternsOverlap(a, b string) bool {
	sa, okA := patternSegments(a)
	sb, okB := patternSegments(b)
	return okA && okB && segmentsOverlap(sa, sb)
}

// PathsOverlap returns the first pair, one pattern from each list, that
// overlaps.
func PathsOverlap(a, b []string) (pa, pb string, ok bool) {
	for _, x := range a {
		for _, y := range b {
			if PatternsOverlap(x, y) {
				return x, y, true
			}
		}
	}
	return "", "", false
}

// Outside returns the files that match none of the patterns, in the order
// given. With no patterns it returns nil.
func Outside(patterns, files []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	var out []string
	for _, f := range files {
		if !matchesAny(patterns, f) {
			out = append(out, f)
		}
	}
	return out
}

func matchesAny(patterns []string, file string) bool {
	for _, p := range patterns {
		if MatchPath(p, file) {
			return true
		}
	}
	return false
}

// OutsideDomain returns the entries of the piece's own paths that its
// domain's paths don't cover. nil when the piece lists no paths or the domain
// has none.
func OutsideDomain(p Piece, d source.Domain) []string {
	if len(p.Paths) == 0 || len(d.Paths) == 0 {
		return nil
	}
	var out []string
	for _, entry := range p.Paths {
		if !coveredBy(d.Paths, entry) {
			out = append(out, entry)
		}
	}
	return out
}

// coveredBy checks an entry by its literal lead: the segments before the first
// one that holds a wildcard.
func coveredBy(domain []string, entry string) bool {
	var lead []string
	for _, seg := range strings.Split(cleanPath(entry), "/") {
		if strings.ContainsAny(seg, wildcardChars) {
			break
		}
		lead = append(lead, seg)
	}
	if len(lead) == 0 {
		for _, d := range domain {
			if d == "**" || cleanPath(d) == cleanPath(entry) {
				return true
			}
		}
		return false
	}
	return matchesAny(domain, strings.Join(lead, "/"))
}

// patternSegments cleans a pattern as MatchPath does and splits it. A pattern
// with no wildcard stands for itself and everything under it.
func patternSegments(p string) ([]string, bool) {
	p = cleanPath(p)
	if p == "" {
		return nil, false
	}
	segs := strings.Split(p, "/")
	for _, s := range segs {
		if s != "**" && !validSegment(s) {
			return nil, false
		}
	}
	if !strings.ContainsAny(p, wildcardChars) {
		segs = append(segs, "**")
	}
	return segs, true
}

func segmentsOverlap(a, b []string) bool {
	switch {
	case len(a) == 0 && len(b) == 0:
		return true
	case len(a) > 0 && a[0] == "**":
		return segmentsOverlap(a[1:], b) || (len(b) > 0 && segmentsOverlap(a, b[1:]))
	case len(b) > 0 && b[0] == "**":
		return segmentsOverlap(a, b[1:]) || (len(a) > 0 && segmentsOverlap(a[1:], b))
	case len(a) == 0 || len(b) == 0:
		return false
	}
	return segmentsCanMatch(a[0], b[0]) && segmentsOverlap(a[1:], b[1:])
}

// segmentsCanMatch reports whether one name could match both segments.
func segmentsCanMatch(a, b string) bool {
	wa, wb := strings.ContainsAny(a, wildcardChars), strings.ContainsAny(b, wildcardChars)
	if !wa && !wb {
		return a == b
	}
	pa, pb := literalPrefix(a), literalPrefix(b)
	sa, sb := literalSuffix(a), literalSuffix(b)
	return (strings.HasPrefix(pa, pb) || strings.HasPrefix(pb, pa)) &&
		(strings.HasSuffix(sa, sb) || strings.HasSuffix(sb, sa))
}

func literalPrefix(seg string) string {
	if i := strings.IndexAny(seg, wildcardChars); i >= 0 {
		return seg[:i]
	}
	return seg
}

func literalSuffix(seg string) string {
	if i := strings.LastIndexAny(seg, "*?]"); i >= 0 {
		return seg[i+1:]
	}
	return seg
}
