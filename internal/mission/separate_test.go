package mission

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func pp(id string, paths ...string) Piece {
	return Piece{ID: id, Domain: "d", Factory: "f", Paths: paths}
}

func apartOf(m Mission) map[string][]string {
	out := map[string][]string{}
	for _, p := range m.Pieces {
		if len(p.Apart) > 0 {
			out[p.ID] = p.Apart
		}
	}
	return out
}

func TestSeparateOverlaps_LaterIsKeptApart(t *testing.T) {
	m := Mission{Pieces: []Piece{pp("P1", "api/**"), pp("P2", "web/**"), pp("P3", "api/v2/**")}}
	got, seps := SeparateOverlaps(m, nil, nil)
	if want := map[string][]string{"P3": {"P1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("apart = %v", apartOf(got))
	}
	if want := []Separation{{"P3", "P1", "api/**", "api/v2/**"}}; !reflect.DeepEqual(seps, want) {
		t.Fatalf("seps = %+v", seps)
	}
	if len(apartOf(m)) != 0 {
		t.Fatal("input changed")
	}
	if s := seps[0].String(); s != "P3 starts after P1: their paths overlap (P1: api/**, P3: api/v2/**)" {
		t.Fatalf("string = %q", s)
	}
}

func TestSeparateOverlaps_PiecesThatAlreadyWaitAreLeft(t *testing.T) {
	p2, p3 := pp("P2", "api/**"), pp("P3", "api/**")
	p2.WaitsFor, p3.WaitsFor = []string{"P1"}, []string{"P2"}
	got, seps := SeparateOverlaps(Mission{Pieces: []Piece{pp("P1", "api/**"), p2, p3}}, nil, nil)
	if len(seps) != 0 || len(apartOf(got)) != 0 {
		t.Fatalf("apart = %v seps = %v", apartOf(got), seps)
	}
}

func TestSeparateOverlaps_UsesTheDomainsPaths(t *testing.T) {
	m := Mission{Pieces: []Piece{pp("P1"), pp("P2")}}
	got, _ := SeparateOverlaps(m, []source.Domain{{Name: "d", Paths: []string{"src/**"}}}, nil)
	if want := map[string][]string{"P2": {"P1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("apart = %v", apartOf(got))
	}
	got, _ = SeparateOverlaps(m, []source.Domain{{Name: "d"}}, nil)
	if len(apartOf(got)) != 0 {
		t.Fatalf("apart = %v", apartOf(got))
	}
}

func TestSeparateOverlaps_ChainOfThree(t *testing.T) {
	m := Mission{Pieces: []Piece{pp("P1", "a/**"), pp("P2", "a/**"), pp("P3", "a/**")}}
	got, seps := SeparateOverlaps(m, nil, nil)
	if want := map[string][]string{"P2": {"P1"}, "P3": {"P2", "P1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("apart = %v", apartOf(got))
	}
	if len(seps) != 3 {
		t.Fatalf("seps = %v", seps)
	}
}

func TestSeparateOverlaps_SettledPieceIsNeverMoved(t *testing.T) {
	m := Mission{Pieces: []Piece{pp("P1", "a/**"), pp("P2", "a/**")}}
	got, seps := SeparateOverlaps(m, nil, map[string]bool{"P2": true})
	if want := map[string][]string{"P1": {"P2"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("apart = %v", apartOf(got))
	}
	if want := (Separation{"P1", "P2", "a/**", "a/**"}); seps[0] != want {
		t.Fatalf("sep = %+v", seps[0])
	}
	got, seps = SeparateOverlaps(m, nil, map[string]bool{"P1": true, "P2": true})
	if len(apartOf(got)) != 0 || len(seps) != 0 {
		t.Fatalf("apart = %v", apartOf(got))
	}
}

func TestSeparateOverlaps_StartsFromScratch(t *testing.T) {
	p1, p2, p3 := pp("P1", "a/**"), pp("P2", "b/**"), pp("P3", "a/x/**")
	p2.Apart, p3.Apart = []string{"P1"}, []string{"P2", "P9"}
	got, _ := SeparateOverlaps(Mission{Pieces: []Piece{p1, p2, p3}}, nil, nil)
	if want := map[string][]string{"P3": {"P1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("apart = %v", apartOf(got))
	}
}

func TestSeparateOverlaps_NeverMakesACircle(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	patterns := []string{"a/**", "a/b/**", "c/**", "*/x"}
	for range 200 {
		n := 1 + rng.Intn(8)
		var m Mission
		for i := range n {
			p := pp(string(rune('A'+i)), patterns[rng.Intn(len(patterns))])
			for j := range i {
				if rng.Intn(3) == 0 {
					p.WaitsFor = append(p.WaitsFor, m.Pieces[j].ID)
				}
			}
			m.Pieces = append(m.Pieces, p)
		}
		settled := map[string]bool{}
		for _, p := range m.Pieces {
			settled[p.ID] = rng.Intn(4) == 0
		}
		got, _ := SeparateOverlaps(m, nil, settled)
		joined := Mission{}
		index := map[string]int{}
		for i, p := range got.Pieces {
			p.WaitsFor = append(append([]string(nil), p.WaitsFor...), p.Apart...)
			joined.Pieces = append(joined.Pieces, p)
			index[p.ID] = i
		}
		if rings := waitCircles(joined.Pieces, index); len(rings) > 0 {
			t.Fatalf("circle %v in %+v", rings, got.Pieces)
		}
		checkEveryOverlapIsHeld(t, got, settled)
	}
}

// checkEveryOverlapIsHeld fails when two overlapping pieces of m are neither
// ordered by waits_for alone, nor named by a direct apart_from entry, nor
// either of them settled.
func checkEveryOverlapIsHeld(t *testing.T, m Mission, settled map[string]bool) {
	t.Helper()
	index := map[string]int{}
	for i, p := range m.Pieces {
		index[p.ID] = i
	}
	for j, b := range m.Pieces {
		for i, a := range m.Pieces[:j] {
			if _, _, ok := PathsOverlap(a.Paths, b.Paths); !ok || settled[a.ID] || settled[b.ID] {
				continue
			}
			if waitsReach(m.Pieces, index, i, j) || waitsReach(m.Pieces, index, j, i) {
				continue
			}
			if slices.Contains(a.Apart, b.ID) || slices.Contains(b.Apart, a.ID) {
				continue
			}
			t.Fatalf("%s and %s overlap with nothing holding them apart: %+v", a.ID, b.ID, m.Pieces)
		}
	}
}

// waitsReach says whether piece from reaches piece to through waits_for alone.
func waitsReach(pieces []Piece, index map[string]int, from, to int) bool {
	seen := map[int]bool{}
	stack := []int{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, id := range pieces[cur].WaitsFor {
			k := index[id]
			if k == to {
				return true
			}
			if !seen[k] {
				seen[k] = true
				stack = append(stack, k)
			}
		}
	}
	return false
}

func TestStartedPieces_ParkedBeforeStartingIsNotSettled(t *testing.T) {
	s := State{Pieces: []PieceState{
		{ID: "P1", Status: PieceRunning},
		{ID: "P2", Status: PieceWaiting, Wait: &Wait{Back: PieceQueued}},
		{ID: "P3", Status: PieceWaiting, Wait: &Wait{Back: PieceRunning}},
		{ID: "P4", Status: PieceQueued},
	}}
	got := StartedPieces(s)
	if !got["P1"] || got["P2"] || !got["P3"] || got["P4"] {
		t.Fatalf("settled = %v", got)
	}
}

func TestSeparateOverlaps_ParkedPieceKeepsItsEntry(t *testing.T) {
	p2 := pp("P2", "a/**")
	p2.Apart = []string{"P1"}
	m := Mission{Pieces: []Piece{pp("P1", "a/**"), p2}}
	s := State{Pieces: []PieceState{
		{ID: "P1", Status: PieceRunning},
		{ID: "P2", Status: PieceWaiting, Wait: &Wait{Back: PieceQueued}},
	}}
	got, _ := SeparateOverlaps(m, nil, StartedPieces(s))
	if want := map[string][]string{"P2": {"P1"}}; !reflect.DeepEqual(apartOf(got), want) {
		t.Fatalf("apart = %v", apartOf(got))
	}
}
