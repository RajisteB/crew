package mission

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
)

func TestEngine_HappyPathOnePiece(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	st := h.tick()
	p := st.Pieces[0]
	wantWT := filepath.Join(h.e.RepoRoot, ".crew", "worktrees", h.id+"-P1")
	if p.Status != PieceRunning || p.RunID != "run-P1" || p.Worktree != wantWT || p.Branch != "crew/"+h.id+"/P1" {
		t.Fatalf("after start: %+v", p)
	}
	if !h.log.has("Fetch main") || !h.log.has("AddWorktree "+wantWT+" crew/"+h.id+"/P1 origin/main") || !h.log.has("Drive run-P1") {
		t.Fatalf("calls: %v", h.log.calls)
	}

	h.runs.setRun("run-P1", doneState(), false)
	if p := h.tick().Pieces[0]; p.Status != PiecePROpen || p.PR != 1 {
		t.Fatalf("after run done: %+v", p)
	}
	h.tick() // PR is open: waits on the principal
	h.gh.setState(1, "MERGED")
	st = h.tick()
	if st.Status != StatusDone || st.Pieces[0].Status != PieceComplete {
		t.Fatalf("final: %+v", st)
	}
	if !h.log.has("RemoveWorktree " + wantWT + " force=true") {
		t.Fatalf("worktree not removed: %v", h.log.calls)
	}
	want := []string{EvPieceStart, EvPROpened, EvWaitingOnPrincipal, EvNotified, EvPieceComplete, EvPRMerged, EvMissionDone, EvNotified}
	if got := h.kinds(); !slices.Equal(got, want) {
		t.Fatalf("events = %v\nwant     %v", got, want)
	}
}

func TestEngine_SecondPieceCutFromFreshBase(t *testing.T) {
	h := newHarness(t)
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.tick()
	h.tick()
	h.gh.setState(1, "MERGED")
	h.tick() // P1 completes and P2 starts in the same tick
	if p := h.piece("P2"); p.Status != PieceRunning {
		t.Fatalf("P2 = %+v", p)
	}
	fetches := h.log.indexes("Fetch")
	adds := h.log.indexes("AddWorktree")
	if len(fetches) != 2 || len(adds) != 2 || !(fetches[1] < adds[1]) || !strings.Contains(h.log.calls[adds[1]], "-P2 ") {
		t.Fatalf("fetches %v adds %v calls %v", fetches, adds, h.log.calls)
	}
}

func TestEngine_GHFailureRetriesNextTick(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.fail.failNext("CreatePR", 1)
	st := h.tick()
	if p := st.Pieces[0]; p.Status != PieceRunning || p.PR != 0 {
		t.Fatalf("after failure: %+v", p)
	}
	if notes := h.events(EvNote); len(notes) != 1 || notes[0].Piece != "P1" {
		t.Fatalf("notes = %+v", notes)
	}
	if p := h.tick().Pieces[0]; p.Status != PiecePROpen || p.PR != 1 {
		t.Fatalf("retry: %+v", p)
	}
	if h.log.count("Push") != 2 {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_ResumeRestartsDeadRun(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	h.runs.setRun("run-P1", run.State{Status: run.StatusRunning}, false)
	h.tick()
	if h.log.count("Drive") != 2 {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if p := h.piece("P1"); p.Resumes != 0 || p.Status != PieceRunning {
		t.Fatalf("re-attaching must not count as a resume: %+v", p)
	}
}

func TestEngine_FailedRetryableRunIsResumed(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	h.runs.setRun("run-P1", run.State{Status: run.StatusFailed, Retryable: true}, false)
	h.tick()
	if h.log.count("Drive") != 2 || h.piece("P1").Resumes != 1 {
		t.Fatalf("calls %v piece %+v", h.log.calls, h.piece("P1"))
	}
	h.fail.failNext("Drive", 1)
	h.tick()
	if h.piece("P1").Resumes != 1 {
		t.Fatal("a failed drive must not count as a resume")
	}
}

func TestEngine_NeverRerunsPieceWithPR(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.tick()
	starts, drives := h.log.count("Start"), h.log.count("Drive")
	for range 10 {
		h.tick()
	}
	if h.log.count("Start") != starts || h.log.count("Drive") != drives || h.piece("P1").Status != PiecePROpen {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_StartFailureCleansUpAndRetries(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.fail.failNext("Start", 1)
	st := h.tick()
	if st.Pieces[0].Status != PieceQueued || st.Pieces[0].RunID != "" {
		t.Fatalf("piece = %+v", st.Pieces[0])
	}
	wt := filepath.Join(h.e.RepoRoot, ".crew", "worktrees", h.id+"-P1")
	if !h.log.has("RemoveWorktree " + wt + " force=true") {
		t.Fatalf("worktree not removed: %v", h.log.calls)
	}
	if len(h.events(EvNote)) != 1 {
		t.Fatalf("notes = %v", h.events(EvNote))
	}
	if p := h.tick().Pieces[0]; p.Status != PieceRunning || p.RunID != "run-P1" {
		t.Fatalf("retry: %+v", p)
	}
	if h.log.count("AddWorktree") != 2 {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_DriveFailureAfterStartKeepsState(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.fail.failNext("Drive", 1)
	if p := h.tick().Pieces[0]; p.Status != PieceRunning || p.RunID != "run-P1" {
		t.Fatalf("piece = %+v", p)
	}
	h.runs.setRun("run-P1", run.State{Status: run.StatusRunning}, false)
	h.tick()
	if h.log.count("Drive") != 2 || h.log.count("Start") != 1 {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_AdoptsExistingPRInsteadOfCreating(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.tick()
	h.runs.setRun("run-P1", doneState(), false)
	h.gh.existing["crew/"+h.id+"/P1"] = gh.PR{Number: 9, State: "OPEN", URL: "https://github.com/acme/app/pull/9"}
	p := h.tick().Pieces[0]
	if p.PR != 9 || p.Status != PiecePROpen || h.log.has("CreatePR") {
		t.Fatalf("piece %+v calls %v", p, h.log.calls)
	}
	if evs := h.events(EvPROpened); len(evs) != 1 {
		t.Fatalf("events = %v", h.kinds())
	}
}

func TestEngine_StateSavedRightAfterStart(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runs.onDrive = func() {
		if p := h.piece("P1"); p.RunID != "run-P1" || p.Status != PieceRunning {
			t.Errorf("state on disk during Drive: %+v", p)
		}
	}
	h.fail.failNext("Drive", 1)
	h.tick()
	if h.log.count("Drive") != 1 {
		t.Fatalf("calls: %v", h.log.calls)
	}
}

func TestEngine_RepeatedFailureIsOneNote(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runPiece("P1")
	h.runs.setRun("run-P1", doneState(), false)
	h.edit(func(s *State) { s.Pieces[0].Status = PiecePROpen; s.Pieces[0].PR = 4 })
	h.fail.failNext("PRState", 5)
	for range 5 {
		h.tick()
	}
	if notes := h.events(EvNote); len(notes) != 1 {
		t.Fatalf("notes = %+v", notes)
	}
	h.gh.prs[4] = gh.PR{Number: 4, State: "OPEN"}
	h.tick()
	h.fail.failNext("PRState", 1)
	h.tick()
	if notes := h.events(EvNote); len(notes) != 2 {
		t.Fatalf("a failure after a success is news again: %+v", notes)
	}
}

func TestEngine_SendBackUsesFactoryReworkStation(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.e.Checks.Factories[0].Rework = "build"
	runID := h.runPiece("P1")
	h.runs.setRun(runID, doneState(), false)
	h.git.dirty = []string{"a.go"}
	h.tick()
	if !h.log.has("Step run-P1 goto build") {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if n := h.piece("P1").SentBack["uncommitted"]; n != 1 {
		t.Fatalf("sent back = %d", n)
	}
	if got := h.runs.steps[0].Note; !strings.Contains(got, "a.go") {
		t.Fatalf("note = %q", got)
	}
}

func TestEngine_FailedNotifyIsANote(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.notif.fail = errors.New("no display")
	h.runPiece("P1")
	h.edit(func(s *State) { s.Pieces[0].Status = PiecePROpen; s.Pieces[0].PR = 1 })
	h.gh.prs[1] = gh.PR{Number: 1, State: "OPEN"}
	h.tick()
	if len(h.events(EvNote)) != 1 || h.piece("P1").Wait == nil {
		t.Fatalf("events %v", h.kinds())
	}
}

func TestEngine_NilDependenciesAreSafe(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.e.Notifier, h.e.Out, h.e.Now = nil, nil, nil
	h.e.Orch = nil
	h.tick()
	h.runs.setRun("run-P1", limitHit(), false)
	h.runs.summary = limitHit()
	h.tick() // no orchestrator: escalates to the principal
	if p := h.piece("P1"); p.Wait == nil {
		t.Fatalf("piece = %+v", p)
	}
}
