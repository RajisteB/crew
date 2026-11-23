package mission

import (
	"reflect"
	"slices"
	"testing"
)

func delegated(id string, paths ...string) Piece {
	p := pp(id, paths...)
	p.Delegate = true
	return p
}

func TestSeparateOverlaps_NeverComparesAPieceWithItsParent(t *testing.T) {
	m := Mission{Pieces: []Piece{delegated("P3", "brand/**"), pp("P3.1", "brand/a/**")}}
	got, seps := SeparateOverlaps(m, nil, nil)
	if len(apartOf(got)) != 0 || len(seps) != 0 {
		t.Fatalf("apart = %v seps = %v", apartOf(got), seps)
	}
	m = Mission{Pieces: []Piece{delegated("P3", "brand/**"), delegated("P3.1", "brand/a/**"), pp("P3.1.1", "brand/a/b/**")}}
	if got, _ := SeparateOverlaps(m, nil, nil); len(apartOf(got)) != 0 {
		t.Fatalf("apart = %v", apartOf(got))
	}
}

func TestSeparateOverlaps_OutsiderIsOrderedThroughTheParent(t *testing.T) {
	subs := []Piece{pp("P3.1", "brand/logo/**"), pp("P3.2", "brand/logo/**")}
	m := Mission{Pieces: append([]Piece{delegated("P3", "brand/**"), pp("X", "brand/logo/**")}, subs...)}
	got, _ := SeparateOverlaps(m, nil, nil)
	if want := map[string][]string{"X": {"P3"}, "P3.2": {"P3.1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("X after P3: apart = %v", apartOf(got))
	}
	m = Mission{Pieces: append([]Piece{pp("X", "brand/logo/**"), delegated("P3", "brand/**")}, subs...)}
	got, _ = SeparateOverlaps(m, nil, map[string]bool{"P3": true})
	if want := map[string][]string{"X": {"P3"}, "P3.2": {"P3.1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("X before P3: apart = %v", apartOf(got))
	}
}

func TestSeparateOverlaps_SubPiecesOfTwoParents(t *testing.T) {
	m := Mission{Pieces: []Piece{delegated("P3", "a/**"), delegated("P4", "a/**"), pp("P3.1", "a/x/**"), pp("P4.1", "a/x/**")}}
	got, seps := SeparateOverlaps(m, nil, nil)
	if want := map[string][]string{"P4": {"P3"}}; !reflect.DeepEqual(apartOf(got), want) || len(seps) != 1 {
		t.Fatalf("apart = %v seps = %v", apartOf(got), seps)
	}
}

func TestSeparateOverlaps_ParentWithoutPaths(t *testing.T) {
	m := Mission{Pieces: []Piece{delegated("P3"), pp("X", "a/**"), pp("P3.1", "a/**")}}
	got, seps := SeparateOverlaps(m, nil, nil)
	if want := map[string][]string{"P3.1": {"X"}}; !reflect.DeepEqual(apartOf(got), want) || len(seps) != 1 {
		t.Fatalf("apart = %v seps = %v", apartOf(got), seps)
	}
	m.Pieces[1].WaitsFor = []string{"P3"}
	got, seps = SeparateOverlaps(m, nil, nil)
	if len(apartOf(got)) != 0 || len(seps) != 0 {
		t.Fatalf("waiting for the parent: apart = %v seps = %v", apartOf(got), seps)
	}
}

func TestBranchesPart(t *testing.T) {
	for _, c := range [][4]string{
		{"P3.1.2", "X", "P3", "X"},
		{"P3.1.2", "P3.2", "P3.1", "P3.2"},
		{"P3", "X", "P3", "X"},
		{"P3.1", "P3.2", "P3.1", "P3.2"},
		{"P3.1.1", "P3.2.1", "P3.1", "P3.2"},
	} {
		x, y := branchesPart(c[0], c[1])
		if x != c[2] || y != c[3] {
			t.Errorf("branchesPart(%s, %s) = %s, %s; want %s, %s", c[0], c[1], x, y, c[2], c[3])
		}
	}
}

// testEdges is written apart from the production code: the edges of a piece
// are its own waits_for and apart_from, the same of every ancestor, and, from
// a delegated piece, each piece directly under it.
func testEdges(pieces []Piece) func(id string) []string {
	byID := map[string]Piece{}
	for _, p := range pieces {
		byID[p.ID] = p
	}
	return func(id string) []string {
		var out []string
		for _, q := range append([]string{id}, Ancestors(id)...) {
			out = append(out, byID[q].WaitsFor...)
			out = append(out, byID[q].Apart...)
		}
		if byID[id].Delegate {
			for _, c := range pieces {
				if ParentID(c.ID) == id {
					out = append(out, c.ID)
				}
			}
		}
		return slices.DeleteFunc(out, func(to string) bool { _, ok := byID[to]; return !ok })
	}
}

func hasCircle(pieces []Piece) bool {
	edges := testEdges(pieces)
	state := map[string]int{} // 1 on the path, 2 done
	var visit func(id string) bool
	visit = func(id string) bool {
		state[id] = 1
		for _, to := range edges(id) {
			if state[to] == 1 || (state[to] == 0 && visit(to)) {
				return true
			}
		}
		state[id] = 2
		return false
	}
	for _, p := range pieces {
		if state[p.ID] == 0 && visit(p.ID) {
			return true
		}
	}
	return false
}

// reachable says whether from comes after to: a path of edges leads from one
// to the other.
func reachable(pieces []Piece, from, to string) bool {
	edges := testEdges(pieces)
	seen := map[string]bool{}
	next := []string{from}
	for len(next) > 0 {
		id := next[len(next)-1]
		next = next[:len(next)-1]
		for _, e := range edges(id) {
			if e == to {
				return true
			}
			if !seen[e] {
				seen[e] = true
				next = append(next, e)
			}
		}
	}
	return false
}
