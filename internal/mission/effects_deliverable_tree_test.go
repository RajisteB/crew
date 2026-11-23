package mission

import (
	"os"
	"path/filepath"
	"testing"
)

// P3.2 posted a brief, the principal answered its deliverable wait with
// cancel, P3.1 completed, so P3 is complete. P4, which waits for P3, is
// handed what P3.1 made and nothing of P3.2's.
func TestPrepareScratch_HandsOnNothingFromACancelledPiece(t *testing.T) {
	h := newHarness(t)
	m := Mission{Request: "r", Pieces: []Piece{
		{ID: "P3", Delegate: true}, {ID: "P3.1"}, {ID: "P3.2"}, {ID: "P4", WaitsFor: []string{"P3"}},
	}}
	s := State{MissionID: h.id, Status: StatusRunning, NextWait: 2, Pieces: []PieceState{
		{ID: "P3", Status: PieceDelegated},
		{ID: "P3.1", Status: PieceComplete, Deliverable: &Deliverable{Files: []string{"logo.svg"}}},
		{ID: "P3.2", Status: PieceDelivered, Deliverable: &Deliverable{Files: []string{"brief.md"}},
			Wait: &Wait{ID: "w1", Kind: WaitDeliverable, Question: "accept?"}},
		{ID: "P4", Status: PieceQueued},
	}}
	writeFile(t, filepath.Join(h.store.DeliverablesDir("P3.1"), "logo.svg"), "<svg/>")
	writeFile(t, filepath.Join(h.store.DeliverablesDir("P3.2"), "brief.md"), "the brief the principal turned down")
	_, s, _, _, err := ApplyAnswer(m, s, Answer{Wait: "w1", Do: "cancel", Words: "no, drop it"}, CheckInput{}, planNow)
	if err != nil {
		t.Fatal(err)
	}
	s, _ = Decide(m, s, Obs{Now: planNow}, Limits{Concurrency: lim(3), OrchestratorCalls: lim(3)}, 0)
	if p, _ := s.Piece("P3"); p.Status != PieceComplete {
		t.Fatalf("setup: P3 = %s", p.Status)
	}
	tk := &tick{e: h.e, m: m, s: s}
	wt := t.TempDir()
	earlier, err := tk.prepareScratch(m.Pieces[3], wt)
	if err != nil {
		t.Fatal(err)
	}
	if len(earlier) != 1 || earlier[0].Piece != "P3.1" {
		t.Fatalf("P4 is handed %+v", earlier)
	}
	if _, err := os.Stat(filepath.Join(wt, ".crew", "inputs", "P3.2")); !os.IsNotExist(err) {
		t.Fatalf("a folder for the cancelled P3.2: %v", err)
	}
}
