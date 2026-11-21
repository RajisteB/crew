package mission

import (
	"strings"
	"testing"
)

func TestMermaid_NodesAndWaits(t *testing.T) {
	out := Mermaid(acme())
	for _, want := range []string{"flowchart TD", `P1["P1 · product"]`, "P1 --> P2", "P2 --> P6", "P5 --> P6"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Count(out, "-->") != 4 {
		t.Errorf("want 4 arrows:\n%s", out)
	}
}

func TestMermaid_UncheckedPlanCannotInjectMarkup(t *testing.T) {
	m := Mission{Pieces: []Piece{
		{ID: "end", Domain: `d"]\n click x`},
		{ID: "A\"]; click A call evil()", Domain: "<script>#"},
		{ID: "end", WaitsFor: []string{"end", "ghost"}},
	}}
	out := Mermaid(m)
	for _, bad := range []string{"<script>"} {
		if strings.Contains(out, bad) {
			t.Errorf("output contains %q:\n%s", bad, out)
		}
	}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n")[1:] {
		if strings.Contains(line, "[") && strings.Count(line, `"`) != 2 {
			t.Errorf("label quote not escaped: %q", line)
		}
		if strings.HasPrefix(strings.TrimSpace(line), "end") {
			t.Errorf("reserved word used as a node key: %q", line)
		}
	}
	if strings.Contains(out, "ghost") {
		t.Errorf("edge to unknown piece emitted:\n%s", out)
	}
}
