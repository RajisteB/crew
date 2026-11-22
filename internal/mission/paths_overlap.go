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

// segmentsOverlap reports whether one path could match both segment lists.
// Every call works on a suffix of each list, so the answer for each pair of
// remaining lengths is worked out once: patterns with many ** stay quick.
func segmentsOverlap(a, b []string) bool {
	memo := make(map[[2]int]bool)
	var overlap func(i, j int) bool
	overlap = func(i, j int) bool {
		key := [2]int{i, j}
		if got, ok := memo[key]; ok {
			return got
		}
		got := suffixesOverlap(a, b, i, j, overlap)
		memo[key] = got
		return got
	}
	return overlap(0, 0)
}

// suffixesOverlap is one step of segmentsOverlap on a[i:] and b[j:]; rest
// answers for shorter suffixes.
func suffixesOverlap(a, b []string, i, j int, rest func(i, j int) bool) bool {
	ea, eb := i == len(a), j == len(b)
	switch {
	case ea && eb:
		return true
	case !ea && a[i] == "**":
		return rest(i+1, j) || (!eb && rest(i, j+1))
	case !eb && b[j] == "**":
		return rest(i, j+1) || (!ea && rest(i+1, j))
	case ea || eb:
		return false
	}
	return segmentsCanMatch(a[i], b[j]) && rest(i+1, j+1)
}

// segmentsCanMatch reports whether one name could match both segments.
// A segment holding a backslash counts as a wildcard: crew can't tell what
// it matches, so it says yes.
func segmentsCanMatch(a, b string) bool {
	if strings.Contains(a, `\`) || strings.Contains(b, `\`) {
		return true
	}
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
