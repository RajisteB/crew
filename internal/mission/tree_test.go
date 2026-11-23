package mission

import (
	"slices"
	"testing"
)

func TestParentLevelUnderAncestors(t *testing.T) {
	if got := ParentID("P3.2"); got != "P3" {
		t.Errorf("ParentID(P3.2) = %q", got)
	}
	if got := ParentID("P3.2.1"); got != "P3.2" {
		t.Errorf("ParentID(P3.2.1) = %q", got)
	}
	if got := ParentID("P3"); got != "" {
		t.Errorf("ParentID(P3) = %q", got)
	}
	for id, want := range map[string]int{"P3": 1, "P3.2": 2, "P3.2.1": 3} {
		if got := Level(id); got != want {
			t.Errorf("Level(%s) = %d, want %d", id, got, want)
		}
	}
	under := []struct {
		id, anc string
		want    bool
	}{
		{"P3.2", "P3", true}, {"P3.2.1", "P3", true}, {"P3.2.1", "P3.2", true},
		{"P3", "P3", false}, {"P30.1", "P3", false}, {"P3", "P3.2", false}, {"P4.1", "P3", false},
	}
	for _, c := range under {
		if got := Under(c.id, c.anc); got != c.want {
			t.Errorf("Under(%s, %s) = %v, want %v", c.id, c.anc, got, c.want)
		}
	}
	if got := Ancestors("P3.2.1"); !slices.Equal(got, []string{"P3.2", "P3"}) {
		t.Errorf("Ancestors(P3.2.1) = %v", got)
	}
	if got := Ancestors("P1"); len(got) != 0 {
		t.Errorf("Ancestors(P1) = %v", got)
	}
}

func TestMissionChildren(t *testing.T) {
	m := Mission{Pieces: []Piece{{ID: "P3"}, {ID: "P3.1"}, {ID: "P3.1.1"}, {ID: "P3.2"}, {ID: "P4"}}}
	var ids []string
	for _, p := range m.Children("P3") {
		ids = append(ids, p.ID)
	}
	if !slices.Equal(ids, []string{"P3.1", "P3.2"}) {
		t.Errorf("Children(P3) = %v", ids)
	}
}

func TestNextChildID(t *testing.T) {
	if got := NextChildID(State{Pieces: []PieceState{{ID: "P3"}}}, "P3"); got != "P3.1" {
		t.Errorf("none: %s", got)
	}
	s := State{Pieces: []PieceState{
		{ID: "P3"}, {ID: "P3.1"}, {ID: "P3.2", Status: PieceCancelled}, {ID: "P3.2.1"}, {ID: "P30.9"},
	}}
	if got := NextChildID(s, "P3"); got != "P3.3" {
		t.Errorf("got %s, want P3.3", got)
	}
}
