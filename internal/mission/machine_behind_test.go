package mission

import (
	"strings"
	"testing"
)

var behindA = Blocker{Mission: "20261122-ab12", Piece: "P9", PathA: "api/**", PathB: "api/v2/**"}

func obsBehind(id string, b Blocker) Obs {
	o := obs(t0)
	o.Behind = map[string]Blocker{id: b}
	return o
}

func TestDecide_PieceBehindAnotherMissionDoesNotStart(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceQueued)
	o := obsBehind("P1", behindA)
	s, as := Decide(m, s, o, limits(3, 3), 0)
	if got := of[StartPiece](as); len(got) != 0 {
		t.Fatalf("started %+v", got)
	}
	if p := piece(t, s, "P1"); p.Behind == nil || *p.Behind != behindA {
		t.Fatalf("Behind = %+v", p.Behind)
	}
	recs := of[Record](as)
	if len(recs) != 1 || recs[0].Event.Kind != EvNote || !strings.HasPrefix(recs[0].Event.Msg, "starts after ") {
		t.Fatalf("records = %+v", recs)
	}
	s, as = Decide(m, s, o, limits(3, 3), 0)
	if len(of[Record](as)) != 0 || len(of[StartPiece](as)) != 0 {
		t.Fatalf("second decide: %+v", as)
	}
	s, as = Decide(m, s, obs(t0), limits(3, 3), 0)
	if p := piece(t, s, "P1"); p.Behind != nil {
		t.Fatalf("Behind = %+v, want nil", p.Behind)
	}
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P1" {
		t.Fatalf("got %+v", as)
	}
}

func TestDecide_BehindAStoppedMissionNotifiesOnce(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceQueued)
	stopped := behindA
	stopped.Stopped = true
	o := obsBehind("P1", stopped)
	s, as := Decide(m, s, o, limits(3, 3), 0)
	ns := of[Notify](as)
	if len(ns) != 1 {
		t.Fatalf("notifications = %+v", ns)
	}
	for _, want := range []string{"P1", "P9", behindA.Mission, "crew mission amend", "which is stopped"} {
		if !strings.Contains(ns[0].Body, want) {
			t.Errorf("body %q lacks %q", ns[0].Body, want)
		}
	}
	if !strings.HasPrefix(ns[0].Title, "crew mission ") {
		t.Errorf("title = %q", ns[0].Title)
	}
	if _, as = Decide(m, s, o, limits(3, 3), 0); len(of[Notify](as)) != 0 || len(of[Record](as)) != 0 {
		t.Fatalf("second decide: %+v", as)
	}

	s2 := st(m, PieceQueued)
	s2, _ = Decide(m, s2, obsBehind("P1", behindA), limits(3, 3), 0)
	_, as = Decide(m, s2, o, limits(3, 3), 0)
	if len(of[Record](as)) != 1 || len(of[Notify](as)) != 1 {
		t.Fatalf("became stopped: %+v", as)
	}
}

func TestDecide_BehindIsOnlyForReadyPieces(t *testing.T) {
	m := mis(map[string][]string{"P2": {"P1"}}, "P1", "P2")
	s := st(m, PieceRunning, PieceQueued)
	s, as := Decide(m, s, obsBehind("P2", behindA), limits(3, 3), 0)
	if p := piece(t, s, "P2"); p.Behind != nil {
		t.Fatalf("Behind = %+v", p.Behind)
	}
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvNote {
			t.Fatalf("unexpected note %+v", r)
		}
	}
}

func TestDecide_OtherReadyPiecesStillStart(t *testing.T) {
	m := mis(nil, "P1", "P2")
	s := st(m, PieceQueued, PieceQueued)
	_, as := Decide(m, s, obsBehind("P1", behindA), limits(3, 3), 0)
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P2" {
		t.Fatalf("got %+v", got)
	}
}

func TestStarted_ClearsBehind(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceQueued)
	b := behindA
	s.Pieces[0].Behind = &b
	next := Started(s, "P1", "r", "br", "wt")
	if next.Pieces[0].Behind != nil {
		t.Fatal("Started kept Behind")
	}
	if s.Pieces[0].Behind == nil {
		t.Fatal("Started changed its input")
	}
}

func TestPieceState_CloneCopiesBehind(t *testing.T) {
	b := behindA
	p := PieceState{ID: "P1", Behind: &b}
	c := p.clone()
	c.Behind.Piece = "X"
	if p.Behind.Piece != "P9" {
		t.Fatal("clone shares Behind")
	}
}

func TestCancelPiece_ClearsBehind(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceQueued)
	b := behindA
	s.Pieces[0].Behind = &b
	d := &decider{s: s.Clone(), m: m, o: obs(t0)}
	d.cancelPiece("P1", "principal")
	if p := piece(t, d.s, "P1"); p.Behind != nil || p.Status != PieceCancelled {
		t.Fatalf("piece = %+v", p)
	}
}
