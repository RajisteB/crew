package mission

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMission_RequestRoundTripsHostileText(t *testing.T) {
	req := "a `$(rm)` b\n---\n## Changes\n- not a change\n"
	m := Mission{Pieces: []Piece{{ID: "P1", Domain: "d", Factory: "f", Brief: "b"}}, Request: req}
	got, err := Parse(m.Render())
	if err != nil {
		t.Fatal(err)
	}
	if got.Request != req {
		t.Errorf("request = %q, want %q", got.Request, req)
	}
	if len(got.Changes) != 0 {
		t.Errorf("changes = %v, want none", got.Changes)
	}
}

func TestMission_RequestEdgeCases(t *testing.T) {
	for name, req := range map[string]string{
		"empty":          "",
		"newlines":       "\n\nhello\n\n\n",
		"marker line":    "x\n<!-- crew:changes -->\n## Changes\n- 2026-01-01T00:00:00Z · a · b · c\ny",
		"marker at end":  "x\n<!-- crew:changes -->",
		"marker at head": "<!-- crew:changes -->\n",
		"frontmatter":    "---\npieces: []\n---\n",
	} {
		t.Run(name, func(t *testing.T) {
			m := Mission{Request: req}.WithChange(Change{Time: time.Unix(0, 0).UTC(), By: "a", What: "b", Why: "c"})
			got, err := Parse(m.Render())
			if err != nil {
				t.Fatal(err)
			}
			if got.Request != req || len(got.Changes) != 1 {
				t.Errorf("request = %q changes = %d", got.Request, len(got.Changes))
			}
		})
	}
}

func TestMission_ChangesRoundTrip(t *testing.T) {
	ts := time.Date(2026, 11, 21, 10, 30, 0, 0, time.UTC)
	m := Mission{Request: "r"}.
		WithChange(Change{Time: ts, By: "orchestrator", What: "added P7", Why: "needs a · separator\nand a newline"}).
		WithChange(Change{Time: ts.Add(time.Hour), By: "prin · cipal", What: "line\nbreak", Why: ""})
	got, err := Parse(m.Render())
	if err != nil {
		t.Fatal(err)
	}
	want := []Change{
		{Time: ts, By: "orchestrator", What: "added P7", Why: "needs a · separator and a newline"},
		{Time: ts.Add(time.Hour), By: "prin - cipal", What: "line break", Why: ""},
	}
	if !reflect.DeepEqual(got.Changes, want) {
		t.Errorf("changes = %#v, want %#v", got.Changes, want)
	}
}

func TestMission_PiecesRoundTrip(t *testing.T) {
	m := Mission{Request: "r", Pieces: []Piece{
		{ID: "P1", Domain: "product", Factory: "f", Brief: "multi\nline: brief", Paths: []string{"app/**"}},
		{ID: "P2", Domain: "product", Factory: "f", Brief: "b", WaitsFor: []string{"P1"}},
	}}
	got, err := Parse(m.Render())
	if err != nil {
		t.Fatal(err)
	}
	p0 := got.Pieces[0]
	if p0.Brief != m.Pieces[0].Brief || !reflect.DeepEqual(p0.Paths, m.Pieces[0].Paths) || got.Pieces[1].WaitsFor[0] != "P1" {
		t.Errorf("pieces = %#v", got.Pieces)
	}
	if p, ok := got.Piece("P2"); !ok || p.ID != "P2" {
		t.Error("Piece(P2) not found")
	}
	if _, ok := got.Piece("nope"); ok {
		t.Error("Piece(nope) found")
	}
}

func TestParse_UnknownPieceKey(t *testing.T) {
	src := "---\npieces:\n  - id: P1\n    colour: red\n---\nreq\n<!-- crew:changes -->\n## Changes\n"
	if _, err := Parse([]byte(src)); err == nil || !strings.Contains(err.Error(), "colour") {
		t.Errorf("err = %v, want one naming colour", err)
	}
}

func TestParse_UnknownTopLevelKey(t *testing.T) {
	src := "---\npieces: []\nextra: 1\n---\nreq\n<!-- crew:changes -->\n## Changes\n"
	if _, err := Parse([]byte(src)); err == nil {
		t.Error("want an error for an unknown top-level key")
	}
}

func TestParse_Malformed(t *testing.T) {
	for name, src := range map[string]string{
		"no frontmatter": "hello",
		"unclosed":       "---\npieces: []\n",
		"no marker":      "---\npieces: []\n---\nreq\n",
		"bad change":     "---\npieces: []\n---\nreq\n<!-- crew:changes -->\n## Changes\n- nonsense\n",
		"bad time":       "---\npieces: []\n---\nreq\n<!-- crew:changes -->\n## Changes\n- yesterday · a · b · c\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(src)); err == nil {
				t.Error("want an error")
			}
		})
	}
}

func TestWithChange_DoesNotModifyReceiver(t *testing.T) {
	base := Mission{Pieces: make([]Piece, 1, 4), Changes: make([]Change, 0, 4)}
	_ = base.WithChange(Change{By: "x"})
	if len(base.Changes) != 0 {
		t.Errorf("receiver changed: %v", base.Changes)
	}
	if got := base.Changes[:cap(base.Changes)][0]; got.By != "" {
		t.Errorf("receiver's backing array written: %v", got)
	}
}
