package mission

import (
	"strings"
	"testing"
)

func TestRunsElsewhere_MissionWithNoLiveEngineHoldsNoRuns(t *testing.T) {
	root := t.TempDir()
	self := newHarnessIn(t, root, independent("P1")...)
	dead := newHarnessIn(t, root, independent("A1", "A2")...)
	dead.runPiece("A1")
	dead.runPiece("A2")
	if got, err := RunsElsewhere(root, self.id); err != nil || got != 2 {
		t.Fatalf("driven: RunsElsewhere = %d, %v; want 2", got, err)
	}
	dead.undrive()
	if got, err := RunsElsewhere(root, self.id); err != nil || got != 0 {
		t.Fatalf("no live engine: RunsElsewhere = %d, %v; want 0", got, err)
	}
}

func TestClaimsElsewhere_MissionWithNoLiveEngineIsNotRunning(t *testing.T) {
	root := t.TempDir()
	this := newHarnessIn(t, root, independent("P1")...)
	other := newHarnessIn(t, root, domainPiece("P1", "backend", "api/**"))
	setStatuses(other, StatusRunning, PieceRunning)
	got, err := ClaimsElsewhere(root, this.id, claimDomains())
	if err != nil || len(got) != 1 || got[0].Stopped {
		t.Fatalf("driven: claims = %+v, %v; want one, not stopped", got, err)
	}
	other.undrive()
	got, err = ClaimsElsewhere(root, this.id, claimDomains())
	if err != nil || len(got) != 1 || !got[0].Stopped {
		t.Fatalf("no live engine: claims = %+v, %v; want one with Stopped", got, err)
	}
}

func TestClaimsElsewhere_ParkedPieceHoldsNothing(t *testing.T) {
	root := t.TempDir()
	this := newHarnessIn(t, root, independent("P1")...)
	other := newHarnessIn(t, root, domainPiece("P1", "backend", "api/**"))
	other.edit(func(s *State) {
		s.Pieces[0] = PieceState{ID: "P1", Status: PieceWaiting, Wait: &Wait{ID: "w1", Back: PieceQueued}}
	})
	got, err := ClaimsElsewhere(root, this.id, claimDomains())
	if err != nil || len(got) != 0 {
		t.Fatalf("claims = %+v, %v; a piece that never started holds nothing", got, err)
	}
}

func TestClaimsElsewhere_MissionIDMustMatchItsFolder(t *testing.T) {
	root := t.TempDir()
	this := newHarnessIn(t, root, independent("P1")...)
	other := newHarnessIn(t, root, domainPiece("P1", "backend", "api/**"))
	other.edit(func(s *State) {
		s.MissionID = this.id
		s.Pieces[0].Status = PieceRunning
	})
	_, err := ClaimsElsewhere(root, this.id, claimDomains())
	if err == nil || !strings.Contains(err.Error(), other.id) {
		t.Fatalf("err = %v, want one naming the folder %s", err, other.id)
	}
	if _, err := RunsElsewhere(root, this.id); err == nil {
		t.Fatal("RunsElsewhere: want an error for a mission id that differs from its folder")
	}
}

func TestEngine_DeadMissionDoesNotStarveAnother(t *testing.T) {
	root := t.TempDir()
	a := newHarnessIn(t, root, independent("A1")...)
	a.runPiece("A1")
	a.undrive()
	b := newHarnessIn(t, root, independent("B1")...)
	b.e.Settings.Limits.Concurrency = lim(1)
	b.tick()
	if got := b.piece("B1").Status; got != PieceRunning {
		t.Fatalf("B1 = %s, want running: A has no live engine", got)
	}
}

// The way out the notification gives for a mission that is not running: an
// amendment queued while it is stopped is applied in the first tick after
// resume, before anything starts.
func TestEngine_AmendmentQueuedWhileStoppedIsAppliedBeforeAnythingStarts(t *testing.T) {
	h := newHarness(t, independent("P1", "P2")...)
	h.runPiece("P1")
	h.edit(func(s *State) { s.Status = StatusStopped })
	m, err := h.store.Mission()
	if err != nil {
		t.Fatal(err)
	}
	drop := Mission{Request: m.Request, Changes: m.Changes}
	if err := h.store.Drop("amend", Amendment{Plan: drop.Render(), Words: "drop both"}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.piece("P1").Status; got != PieceRunning {
		t.Fatalf("a stopped mission applies nothing: P1 = %s", got)
	}
	h.edit(func(s *State) { s.Status = StatusRunning }) // what crew mission resume writes
	st := h.tick()
	if h.log.has("Start ") {
		t.Fatalf("a piece started before the amendment was applied: %v", h.log.calls)
	}
	if p1, p2 := h.piece("P1").Status, h.piece("P2").Status; p1 != PieceCancelled || p2 != PieceCancelled || st.Status != StatusDone {
		t.Fatalf("P1 = %s, P2 = %s, mission = %s; want both cancelled and done", p1, p2, st.Status)
	}
}
