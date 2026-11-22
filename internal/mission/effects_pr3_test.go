package mission

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/principal"
)

const notConfirmedNote = "GitHub accepted the merge of PR #1 but it isn't merged yet (a merge queue, or GitHub's own auto-merge); crew completes the piece when it is"

func TestEngine_MergeNotConfirmedStaysOpen(t *testing.T) {
	for _, stateErr := range []bool{false, true} {
		h := prHarness(t, principal.AutoOn, passing())
		q := &queueGH{fakeGH: h.gh, stateErr: stateErr}
		h.e.GH = q
		h.tick()
		if !h.log.has("MergePR 1 squash h1") {
			t.Fatalf("calls: %v", h.log.calls)
		}
		if p := h.piece("P1"); p.Status != PiecePROpen || p.Worktree == "" {
			t.Fatalf("stateErr=%v piece = %+v", stateErr, p)
		}
		if len(h.events(EvPRMerged)) != 0 || len(h.events(EvChecksPassed)) != 0 || h.log.has("RemoveWorktree") {
			t.Fatalf("recorded a merge: %v %v", h.kinds(), h.log.calls)
		}
		if !strings.Contains(noteText(h), notConfirmedNote) {
			t.Fatalf("notes %q", noteText(h))
		}
		q.stateErr = false
		h.gh.setState(1, "MERGED")
		h.tick()
		if h.piece("P1").Status != PieceComplete {
			t.Fatalf("not completed later: %+v", h.piece("P1"))
		}
	}
}

func TestEngine_MergeRefusedOpensWait(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.fail.failNext("MergePR", 1)
	h.tick()
	want := "PR #1 (P1): GitHub refused the merge; crew keeps trying, or merge it yourself"
	p := h.piece("P1")
	if p.Status != PiecePROpen || p.Wait == nil || p.Wait.Kind != WaitMerge || p.Wait.Question != want {
		t.Fatalf("piece = %+v wait %+v", p, p.Wait)
	}
	if strings.Contains(p.Wait.Question, "failed") || h.log.count("Notify") != 1 {
		t.Fatalf("notifies %d", h.log.count("Notify"))
	}
	if !strings.Contains(noteText(h), "merge_method") {
		t.Fatalf("notes %q", noteText(h))
	}
	h.tick() // the merge is tried again and goes through
	if p := h.piece("P1"); p.Status != PieceComplete || p.Wait != nil {
		t.Fatalf("piece = %+v", p)
	}
}

func TestEngine_ConflictStuckOpensWaitAfterPatience(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.edit(func(s *State) { s.Pieces[0].PRSince = testNow.Add(-10 * time.Minute) })
	h.tick()
	if w := h.piece("P1").Wait; w != nil {
		t.Fatalf("wait after 10m: %+v", w)
	}
	h.edit(func(s *State) { s.Pieces[0].PRSince = testNow.Add(-31 * time.Minute) })
	h.tick()
	want := "PR #1 (P1): GitHub still reports a conflict after 30m, though main merges into the branch cleanly; it is waiting on you"
	w := h.piece("P1").Wait
	if w == nil || w.Kind != WaitMerge || w.Question != want {
		t.Fatalf("wait = %+v", w)
	}
	n := h.log.count("Notify")
	h.tick()
	if h.log.count("Notify") != n {
		t.Fatal("notified again")
	}
}

func TestEngine_MergeUnreadableSwitchSaysSo(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{
		modes(map[string]principal.AutoMode{"product": principal.AutoOn}),
		{err: errors.New("disk gone")},
	}}
	h.tick()
	if h.log.has("MergePR") {
		t.Fatalf("merged: %v", h.log.calls)
	}
	want := "can't read the auto-merge switch or the changed files for PR #1 right now; not merging"
	if !strings.Contains(noteText(h), want) || strings.Contains(noteText(h), "turned off") {
		t.Fatalf("notes %q", noteText(h))
	}
}

func TestEngine_ConflictAskAfterChecksProblem(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.edit(func(s *State) {
		s.Pieces[0].SentBack = map[string]int{"conflict": 2, "checks-failed": 1}
		s.Pieces[0].Problem = "failed checks: unit"
	})
	h.orch.err = errors.New("no orchestrator")
	h.tick()
	if len(h.orch.sums) != 1 {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if sum := h.orch.sums[0]; sum.Failure != "conflict" || sum.Reason != "conflicts with main" {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestEngine_ConflictTextsNameTheBase(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.edit(func(s *State) { s.Base = "develop" })
	h.conflicting()
	h.git.mergedHead = "h2"
	h.tick()
	evs := h.events(EvConflict)
	if len(evs) != 1 || evs[0].Msg != "develop was merged into the branch; checks run again" {
		t.Fatalf("events = %+v", evs)
	}

	h = prHarness(t, principal.AutoOn, passing())
	h.edit(func(s *State) { s.Base = "develop" })
	h.conflicting()
	h.git.conflicts = []string{"f.go"}
	h.tick()
	note := h.runs.steps[0].Note
	if !strings.HasPrefix(note, "develop has moved and these files conflict: f.go.") || strings.Contains(note, "main") {
		t.Fatalf("note = %q", note)
	}
}
