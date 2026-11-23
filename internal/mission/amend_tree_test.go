package mission

import (
	"strings"
	"testing"
)

func treeState() State {
	s := exState()
	s.Pieces = append(s.Pieces,
		PieceState{ID: "P3", Status: PieceStatus("delegated")},
		PieceState{ID: "P3.1", Status: PieceQueued},
		PieceState{ID: "P3.2", Status: PieceQueued},
	)
	return s
}

func amendTree(t *testing.T, edit func(*Mission)) (Mission, State, error) {
	t.Helper()
	next := treePlan()
	edit(&next)
	m, s, _, _, err := Amend(treePlan(), treeState(), Amendment{Plan: next.Render(), Words: words}, delegatingInput(2), t0)
	return m, s, err
}

func TestAmend_CannotAddASubPiece(t *testing.T) {
	_, _, err := amendTree(t, func(m *Mission) {
		m.Pieces = append(m.Pieces, Piece{ID: "P3.3", Domain: "design", Factory: "visuals", Brief: "b", Paths: []string{"design/x/**"}})
	})
	want := "piece P3.3 can't be added by an amendment: crew names sub-pieces when it plans a delegated piece; add a top-level piece instead"
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("err = %v", err)
	}
}

func TestAmend_RemovingADelegatedPieceNeedsItsSubPiecesGone(t *testing.T) {
	_, _, err := amendTree(t, func(m *Mission) { m.Pieces = append(m.Pieces[:5], m.Pieces[6:]...) })
	if err == nil || !strings.Contains(err.Error(), "piece P3.1: its parent P3 is not in the plan") {
		t.Fatalf("err = %v", err)
	}
	_, s, err := amendTree(t, func(m *Mission) { m.Pieces = m.Pieces[:5] })
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"P3", "P3.1", "P3.2"} {
		if ps, _ := s.Piece(id); ps.Status != PieceCancelled {
			t.Errorf("%s is %s", id, ps.Status)
		}
	}
}

func TestApplyDecision_AddRefusesADottedID(t *testing.T) {
	p := newPiece()
	p.ID = "P3.3"
	m, s, _, _ := apply(t, OrchDecision{Action: "add", Piece: p, Quote: "a landing page", Reason: "needed"})
	if len(m.Pieces) != len(acme().Pieces) || s.Additions != 0 || s.Pieces[0].Wait == nil {
		t.Fatalf("a dotted id was accepted: %d pieces, additions %d", len(m.Pieces), s.Additions)
	}
	if got := (&applier{}).addProblem(OrchDecision{Piece: p}); got != `the new piece's id can't hold a "."` {
		t.Fatalf("problem = %q", got)
	}
}
