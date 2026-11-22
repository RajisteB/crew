package mission

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func apartMission() Mission {
	m := mis(nil, "P1", "P2")
	m.Pieces[1].Apart = []string{"P1"}
	return m
}

func TestDecide_ApartPieceWaitsUntilTheOtherIsDone(t *testing.T) {
	m := apartMission()
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusRunning}, Live: true}
	for _, status := range []PieceStatus{PieceRunning, PiecePROpen} {
		_, as := Decide(m, st(m, status, PieceQueued), o, limits(5, 3), 0)
		if got := of[StartPiece](as); len(got) != 0 {
			t.Fatalf("P1 %s: started %+v", status, got)
		}
	}
	_, as := Decide(m, st(m, PieceComplete, PieceQueued), o, limits(5, 3), 0)
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P2" {
		t.Fatalf("got %+v", got)
	}
}

func TestDecide_ApartFromACancelledPieceStarts(t *testing.T) {
	m := apartMission()
	s, as := Decide(m, st(m, PieceCancelled, PieceQueued), obs(t0), limits(5, 3), 0)
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P2" {
		t.Fatalf("got %+v", as)
	}
	if p := piece(t, s, "P2"); p.Wait != nil || strings.Contains(strings.Join(recordKinds(as), ","), "wait") {
		t.Fatalf("a wait was opened: %+v", p)
	}
}

func TestDecide_ApartFromAMissingPieceStarts(t *testing.T) {
	m := mis(nil, "P2")
	m.Pieces[0].Apart = []string{"P9"}
	_, as := Decide(m, st(m, PieceQueued), obs(t0), limits(5, 3), 0)
	if got := of[StartPiece](as); len(got) != 1 {
		t.Fatalf("got %+v", as)
	}
}

func TestParseRender_ApartFromRoundTrips(t *testing.T) {
	m := Mission{Request: "r", Pieces: []Piece{pp("P1", "a/**"), pp("P2", "a/**")}}
	m.Pieces[1].Apart = []string{"P1"}
	out := string(m.Render())
	if strings.Index(out, "apart_from") < strings.LastIndex(out, "waits_for") {
		t.Fatalf("apart_from is not written after waits_for:\n%s", out)
	}
	back, err := Parse([]byte(out))
	if err != nil || len(back.Pieces[1].Apart) != 1 || back.Pieces[1].Apart[0] != "P1" || len(back.Pieces[0].Apart) != 0 {
		t.Fatalf("back = %+v, %v", back.Pieces, err)
	}
	if strings.Count(out, "apart_from") != 1 {
		t.Fatalf("empty apart_from rendered:\n%s", out)
	}
}

func TestAmend_NewOverlappingPieceIsKeptApart(t *testing.T) {
	p7 := Piece{ID: "P7", Domain: "product", Factory: "feature", Brief: "v2", Paths: []string{"app/api/v2/**"}, Apart: []string{"P9"}}
	m, _, _, _, err := amendWith(t, exState(), func(n *Mission) { n.Pieces = append(n.Pieces, p7) })
	if err != nil {
		t.Fatal(err)
	}
	got, _ := m.Piece("P7")
	if len(got.Apart) != 1 || got.Apart[0] != "P1" {
		t.Fatalf("apart = %v", got.Apart)
	}
	last := m.Changes[len(m.Changes)-1]
	if last.By != "crew" || !strings.Contains(last.What, "P7 starts after P1") {
		t.Fatalf("changes = %+v", m.Changes)
	}
}

func TestApplyDecision_AddedPieceIsKeptApart(t *testing.T) {
	m := acme()
	s := exState()
	d := OrchDecision{Action: "add", Reason: "r", Quote: "a landing page", Piece: &Piece{ID: "P7", Domain: "product", Factory: "feature", Brief: "v2", Paths: []string{"app/api/v2/**"}, Apart: []string{"P9"}}}
	got, _, _, _ := ApplyDecision(m, s, "P1", "limit-hit", d, "orchestrator", exInput(), t0)
	p, ok := got.Piece("P7")
	if !ok || len(p.Apart) != 1 || p.Apart[0] != "P1" {
		t.Fatalf("piece = %+v", p)
	}
}
