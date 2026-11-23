package mission

import (
	"strconv"
	"strings"
)

// ParentID is the id of the piece a sub-piece belongs to: "P3" for "P3.2",
// "P3.2" for "P3.2.1", "" for a top-level piece.
func ParentID(id string) string {
	if i := strings.LastIndexByte(id, '.'); i >= 0 {
		return id[:i]
	}
	return ""
}

// Level is 1 for a top-level piece and one more for each "." in the id.
func Level(id string) int {
	return strings.Count(id, ".") + 1
}

// Under reports whether id is a sub-piece of ancestor, at any depth. A piece
// is not under itself. Whole id parts are compared, not string prefixes.
func Under(id, ancestor string) bool {
	rest, found := strings.CutPrefix(id, ancestor+".")
	return found && rest != ""
}

// Ancestors lists the ids above id, nearest first: ["P3.2", "P3"] for "P3.2.1".
func Ancestors(id string) []string {
	var out []string
	for p := ParentID(id); p != ""; p = ParentID(p) {
		out = append(out, p)
	}
	return out
}

// Children lists the pieces of m whose parent is id, in file order.
func (m Mission) Children(id string) []Piece {
	var out []Piece
	for _, p := range m.Pieces {
		if ParentID(p.ID) == id {
			out = append(out, p)
		}
	}
	return out
}

// NextChildID is parent + "." + n, where n is one more than the highest number
// any piece of s has directly under parent, cancelled pieces included;
// "<parent>.1" when none.
func NextChildID(s State, parent string) string {
	highest := 0
	for _, p := range s.Pieces {
		if ParentID(p.ID) != parent {
			continue
		}
		if n, err := strconv.Atoi(p.ID[len(parent)+1:]); err == nil {
			highest = max(highest, n)
		}
	}
	return parent + "." + strconv.Itoa(highest+1)
}
