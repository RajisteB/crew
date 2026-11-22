package mission

import (
	"errors"
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func raiseSteps(h *harness, n int) Settings {
	s := h.e.Settings
	s.Limits.Steps = lim(n)
	return s
}

func TestEngine_RaisedLimitLiftsTheHold(t *testing.T) {
	h := newHarness(t, Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: "one"},
		Piece{ID: "P2", Domain: "product", Factory: "feature", Brief: "two"})
	h.e.Settings.Limits.Steps = lim(2)
	runID := h.runPiece("P1")
	h.runs.setRun(runID, run.State{Status: run.StatusRunning, Step: 2}, true)
	h.tick()
	if h.state().Hold == nil || len(h.events(EvBudgetHit)) != 1 {
		t.Fatalf("not on hold; kinds %v", h.kinds())
	}
	if h.log.has("Start P2") {
		t.Fatal("P2 started on hold")
	}
	calls := 0
	h.e.Reload = func() (Settings, error) { calls++; return raiseSteps(h, 10), nil }
	h.tick()
	if calls != 1 || h.state().Hold != nil || !h.log.has("Start P2") {
		t.Fatalf("calls %d hold %+v log %v", calls, h.state().Hold, h.log.calls)
	}
}

func TestEngine_ReloadIsOnlyCalledOnHold(t *testing.T) {
	h := newHarness(t)
	h.e.Reload = func() (Settings, error) { t.Fatal("Reload called"); return Settings{}, nil }
	for range 3 {
		h.tick()
	}
}

func TestEngine_ReloadFailureIsANote(t *testing.T) {
	h := newHarness(t)
	h.runPiece("P1")
	h.edit(func(s *State) {
		s.Hold = &Hold{Kind: "steps", Count: 100, Max: "100", Since: testNow}
		s.Pieces[0].Steps = 100
	})
	h.e.Reload = func() (Settings, error) { return Settings{}, errors.New("bad yaml") }
	h.tick()
	notes := h.events(EvNote)
	if len(notes) != 1 || notes[0].Msg != "can't read missions.yaml: bad yaml" {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestEngine_AnswerRunsWhileOnHold(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	h.edit(func(s *State) {
		s.Hold = &Hold{Kind: "steps", Count: 100, Max: "100", Since: testNow}
		s.Pieces[0].Steps = 100
		s.Pieces[0].Status = PieceWaiting
		s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitQuestion, Question: "?", Since: testNow, Back: PieceRunning}
	})
	h.e.Settings.Limits.Steps = lim(100)
	if err := h.store.Drop("answer", Answer{Wait: "w1", Do: "retry", Words: "go on"}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if !slices.Contains(h.log.calls, "Step run-P1 retry ") {
		t.Fatalf("calls = %v", h.log.calls)
	}
}
