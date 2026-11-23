package mission

import (
	"path"
	"strings"
)

// PatternWithin reports whether every file child can match is also matched by
// parent. When it can't tell, it says no. A malformed or empty pattern is
// within nothing.
func PatternWithin(child, parent string) bool {
	sc, okC := patternSegments(child)
	sp, okP := patternSegments(parent)
	return okC && okP && segmentsWithin(sc, sp)
}

// PathsWithin returns the first pattern of child that no single pattern of
// parent holds whole; ok is true when there is none. An empty parent holds
// everything.
func PathsWithin(child, parent []string) (outside string, ok bool) {
	if len(parent) == 0 {
		return "", true
	}
	for _, c := range child {
		if !patternWithinAny(c, parent) {
			return c, false
		}
	}
	return "", true
}

func patternWithinAny(child string, parent []string) bool {
	for _, p := range parent {
		if PatternWithin(child, p) {
			return true
		}
	}
	return false
}

// segmentsWithin reports whether every path matching the child segment list
// also matches the parent's. Every call works on a suffix of each list, so
// each pair of positions is worked out once.
func segmentsWithin(child, parent []string) bool {
	memo := make(map[[2]int]bool)
	var within func(i, j int) bool
	within = func(i, j int) bool {
		key := [2]int{i, j}
		if got, ok := memo[key]; ok {
			return got
		}
		got := suffixWithin(child, parent, i, j, within)
		memo[key] = got
		return got
	}
	return within(0, 0)
}

// suffixWithin is one step of segmentsWithin on child[i:] and parent[j:];
// rest answers for shorter suffixes.
func suffixWithin(child, parent []string, i, j int, rest func(i, j int) bool) bool {
	ec, ep := i == len(child), j == len(parent)
	switch {
	case ec && ep:
		return true
	case !ep && parent[j] == "**":
		return rest(i, j+1) || (!ec && rest(i+1, j))
	case ec || ep:
		return false
	case child[i] == "**":
		return false
	}
	return segmentWithin(child[i], parent[j]) && rest(i+1, j+1)
}

// segmentWithin reports whether every name matching child also matches
// parent. A segment holding a backslash is never plain text: crew says no.
func segmentWithin(child, parent string) bool {
	if parent == "*" {
		return true
	}
	if child == parent {
		return true
	}
	if strings.ContainsAny(child, wildcardChars+`\`) {
		return false
	}
	ok, err := path.Match(parent, child)
	return err == nil && ok
}
