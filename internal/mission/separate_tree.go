package mission

import "slices"

// linksOf are the pieces piece cur depends on directly: its own waits_for (and
// apart_from when withApart is set), the same links of every piece above it,
// and, for a delegated piece, each piece directly under it.
func linksOf(pieces []Piece, index map[string]int, cur int, withApart bool) []int {
	own := pieces[cur].WaitsFor
	if withApart {
		own = slices.Concat(own, pieces[cur].Apart)
	}
	var out []int
	for _, id := range own {
		if k, ok := index[id]; ok {
			out = append(out, k)
		}
	}
	return append(out, treeEdgesWith(pieces, index, cur, withApart)...)
}

// branchesPart returns the two pieces where the branches of a and b part: the
// piece directly under their nearest common ancestor (none for two top-level
// branches) on the way to each. It is meant for ids not above or below one
// another; for a and b themselves it returns a and b.
func branchesPart(a, b string) (string, string) {
	common := ""
	for _, anc := range Ancestors(a) {
		if Under(b, anc) {
			common = anc
			break
		}
	}
	return branchUnder(a, common), branchUnder(b, common)
}

// branchUnder is the ancestor of id (or id) that sits directly under common.
func branchUnder(id, common string) string {
	for ParentID(id) != common {
		id = ParentID(id)
	}
	return id
}

// skipForTree says whether the overlapping pair i, j is not compared because
// of the piece tree: one is above the other, or the two pieces where their
// branches part are themselves in the plan with paths, and so were compared,
// and a sub-piece's paths lie inside its parent's.
func skipForTree(pieces []Piece, index map[string]int, paths [][]string, i, j int) bool {
	a, b := pieces[i].ID, pieces[j].ID
	if Under(a, b) || Under(b, a) {
		return true
	}
	x, y := branchesPart(a, b)
	if x == a && y == b {
		return false
	}
	xi, okx := index[x]
	yi, oky := index[y]
	return okx && oky && len(paths[xi]) > 0 && len(paths[yi]) > 0
}
