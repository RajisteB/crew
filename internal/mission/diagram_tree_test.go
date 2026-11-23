package mission

import (
	"strings"
	"testing"
)

func TestMermaid_DelegatedPieceAndItsSubPieces(t *testing.T) {
	m := Mission{Pieces: []Piece{
		{ID: "P3", Domain: "design", Delegate: true},
		{ID: "P3.1", Domain: "design", Factory: "f"},
		{ID: "P3.2", Domain: "design", Factory: "f", WaitsFor: []string{"P3.1"}},
		{ID: "P4", Domain: "design", Factory: "f", WaitsFor: []string{"P3"}},
	}}
	out := Mermaid(m)
	for _, want := range []string{
		`P3["P3 · design · delegated"]`,
		`["P3.1 · design"]`,
		"P3 -.-> node1",
		"P3 -.-> node2",
		"P3 --> P4",
		"node1 --> node2",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "-.->") != 2 {
		t.Errorf("want two dotted arrows, from P3 to each piece directly under it:\n%s", out)
	}
}
