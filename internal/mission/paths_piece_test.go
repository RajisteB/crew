package mission

import (
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func TestEffectivePaths(t *testing.T) {
	d := source.Domain{Paths: []string{"api/**"}}
	if got := EffectivePaths(Piece{Paths: []string{"web/**"}}, d); !slices.Equal(got, []string{"web/**"}) {
		t.Errorf("own paths: got %v", got)
	}
	if got := EffectivePaths(Piece{}, d); !slices.Equal(got, []string{"api/**"}) {
		t.Errorf("domain paths: got %v", got)
	}
	if got := EffectivePaths(Piece{}, source.Domain{}); got != nil {
		t.Errorf("neither: got %v", got)
	}
}

func TestOutsideDomain(t *testing.T) {
	d := source.Domain{Paths: []string{"api/**", "**/*.md"}}
	p := Piece{Paths: []string{"api/v2/**", "deploy/**", "docs/x.md", "**"}}
	if got := OutsideDomain(p, d); !slices.Equal(got, []string{"deploy/**", "**"}) {
		t.Errorf("got %v", got)
	}
	if got := OutsideDomain(Piece{}, d); got != nil {
		t.Errorf("no piece paths: got %v", got)
	}
	if got := OutsideDomain(p, source.Domain{}); got != nil {
		t.Errorf("no domain paths: got %v", got)
	}
}

func TestCheck_PiecePathsAreValidated(t *testing.T) {
	m := acme()
	m.Pieces[0].Paths = []string{"/abs"}
	m.Pieces[1].Paths = []string{"a/../b"}
	var msgs []string
	for _, e := range Check(m, baseInput()) {
		msgs = append(msgs, e.Error())
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{`P1: paths: "/abs"`, `P2: paths: "a/../b"`} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
}
