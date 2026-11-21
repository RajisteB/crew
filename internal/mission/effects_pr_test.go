package mission

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/source"
)

var oldSince = testNow.Add(-time.Hour)

// prHarness puts P1 in pr-open (PR #1, head h1, pushed an hour ago), with the
// pull request observed as open and mergeable, the product switch set to mode.
func prHarness(t *testing.T, mode principal.AutoMode, checks ...gh.Check) *harness {
	t.Helper()
	h := newHarness(t)
	h.runPiece("P1")
	h.edit(func(s *State) {
		p := &s.Pieces[0]
		p.Status, p.PR, p.Head, p.PRSince = PiecePROpen, 1, "h1", oldSince
	})
	h.git.head = "h1"
	h.gh.prs[1] = gh.PR{Number: 1, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "h1", Checks: checks}
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{modes(map[string]principal.AutoMode{"product": mode})}}
	return h
}

func passing() gh.Check { return gh.Check{Name: "ci", State: gh.CheckPass, URL: "http://x/ci"} }
func failing() gh.Check { return gh.Check{Name: "unit", State: gh.CheckFail, URL: "http://x/unit"} }

func (h *harness) conflicting() {
	h.t.Helper()
	pr := h.gh.prs[1]
	pr.Mergeable = "CONFLICTING"
	h.gh.prs[1] = pr
}

func noteText(h *harness) string {
	var b strings.Builder
	for _, e := range h.events(EvNote) {
		b.WriteString(e.Msg + "\n")
	}
	return b.String()
}

func TestEngine_AutoMergeOnMergesPassingPR(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.tick()
	if h.log.count("MergePR") != 1 || !h.log.has("MergePR 1 squash h1") {
		t.Fatalf("calls: %v", h.log.calls)
	}
	if p := h.piece("P1"); p.Status != PieceComplete || p.Worktree != "" {
		t.Fatalf("piece = %+v", p)
	}
	if len(h.events(EvChecksPassed)) != 1 {
		t.Fatalf("events = %v", h.kinds())
	}
	if m := h.events(EvPRMerged); len(m) != 1 || !strings.Contains(m[0].Msg, "merged by crew") {
		t.Fatalf("pr-merged = %+v", m)
	}
	if !h.log.has("RemoveWorktree " + h.e.RepoRoot + "/.crew/worktrees/" + h.id + "-P1 force=true") {
		t.Fatalf("calls: %v", h.log.calls)
	}
	h.tick()
	if h.piece("P2").Status != PieceRunning {
		t.Fatalf("P2 = %+v", h.piece("P2"))
	}
}

func TestEngine_SwitchOffLeavesPRWaiting(t *testing.T) {
	h := prHarness(t, principal.AutoOff, passing())
	h.tick()
	if h.log.has("MergePR") {
		t.Fatalf("merged: %v", h.log.calls)
	}
	if w := h.piece("P1").Wait; w == nil || w.Kind != WaitMerge {
		t.Fatalf("wait = %+v", w)
	}
}

func TestEngine_MergeRereadsSwitch(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{
		modes(map[string]principal.AutoMode{"product": principal.AutoOn}),
		modes(map[string]principal.AutoMode{"product": principal.AutoOff}),
	}}
	h.tick()
	if h.log.has("MergePR") || !strings.Contains(noteText(h), "turned off") {
		t.Fatalf("calls %v notes %q", h.log.calls, noteText(h))
	}
	if h.piece("P1").Status != PiecePROpen {
		t.Fatalf("piece = %+v", h.piece("P1"))
	}
}

func TestEngine_TamperedSwitchNeverMerges(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{
		{err: fmt.Errorf("/x/automerge.json: checksum mismatch: %w", principal.ErrTampered)}}}
	h.tick()
	if h.log.has("MergePR") || !strings.Contains(noteText(h), "automerge.json") {
		t.Fatalf("calls %v notes %q", h.log.calls, noteText(h))
	}
	if w := h.piece("P1").Wait; w == nil || w.Kind != WaitMerge {
		t.Fatalf("wait = %+v", w)
	}
}

func TestEngine_UnreadableSwitchIsOff(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Switches = &fakeSwitches{log: h.log, reads: []switchRead{{err: errors.New("disk gone")}}}
	h.tick()
	if h.log.has("MergePR") || !strings.Contains(noteText(h), "can't read the auto-merge switch: disk gone") {
		t.Fatalf("calls %v notes %q", h.log.calls, noteText(h))
	}
}

func TestEngine_NilSwitchesIsOff(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Switches = nil
	h.tick()
	if h.log.has("MergePR") || h.piece("P1").Wait == nil {
		t.Fatalf("calls %v", h.log.calls)
	}
}

func TestEngine_MergeRefusedIsANote(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.fail.failNext("MergePR", 1)
	h.tick()
	if h.piece("P1").Status != PiecePROpen || !strings.Contains(noteText(h), "merge_method") {
		t.Fatalf("piece %+v notes %q", h.piece("P1"), noteText(h))
	}
	h.gh.setState(1, "MERGED")
	h.tick()
	if h.piece("P1").Status != PieceComplete {
		t.Fatalf("piece = %+v", h.piece("P1"))
	}
}

func TestEngine_OtherDomainOffBlocks(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Domains["design"] = source.Domain{Name: "design", Paths: []string{"design/**"}}
	h.git.changed = []string{"design/logo.svg"}
	h.tick()
	w := h.piece("P1").Wait
	if h.log.has("MergePR") || w == nil || !strings.Contains(w.Question, "design") {
		t.Fatalf("calls %v wait %+v", h.log.calls, w)
	}
}

func TestEngine_ChecksFailedSendsBackWithLog(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing(), failing())
	h.gh.logs = map[string]string{"unit": "FAIL: TestCredits expected 10"}
	h.tick()
	if len(h.runs.steps) != 1 {
		t.Fatalf("steps = %+v", h.runs.steps)
	}
	st := h.runs.steps[0]
	if st.Kind != "goto" || st.Station != "plan" || !strings.Contains(st.Note, "unit") || !strings.Contains(st.Note, "FAIL: TestCredits expected 10") {
		t.Fatalf("step = %+v", st)
	}
	p := h.piece("P1")
	if p.Status != PieceRunning || p.SentBack["checks-failed"] != 1 {
		t.Fatalf("piece = %+v", p)
	}
	if !strings.Contains(p.Problem, "unit") || strings.Contains(p.Problem, "TestCredits") {
		t.Fatalf("problem = %q", p.Problem)
	}
	if evs := h.events(EvChecksFailed); len(evs) != 1 || !strings.Contains(evs[0].Msg, "unit") || strings.Contains(evs[0].Msg, "TestCredits") {
		t.Fatalf("events = %+v", evs)
	}
}

func TestEngine_ChecksFailedStepFailureChangesNothing(t *testing.T) {
	h := prHarness(t, principal.AutoOn, failing())
	h.fail.failNext("Step", 1)
	h.tick()
	p := h.piece("P1")
	if p.Status != PiecePROpen || p.SentBack["checks-failed"] != 0 || len(h.events(EvChecksFailed)) != 0 {
		t.Fatalf("piece = %+v", p)
	}
}

func TestEngine_ChecksFailedSkipsUnreadableLogAndCapsAtThree(t *testing.T) {
	h := prHarness(t, principal.AutoOn,
		gh.Check{Name: "c1", State: gh.CheckFail}, gh.Check{Name: "c2", State: gh.CheckFail},
		gh.Check{Name: "c3", State: gh.CheckFail}, gh.Check{Name: "c4", State: gh.CheckFail})
	h.fail.failNext("FailedLog", 1)
	h.gh.logs = map[string]string{"c2": "log two", "c3": "log three", "c4": "log four"}
	h.tick()
	note := h.runs.steps[0].Note
	if h.log.count("FailedLog") != 3 || strings.Contains(note, "log four") || strings.Contains(note, "--- c1 ---") ||
		!strings.Contains(note, "--- c2 ---\nlog two") || !strings.Contains(note, "log three") {
		t.Fatalf("calls %v note %q", h.log.calls, note)
	}
}

func TestEngine_ConflictMergedCleanly(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.git.mergedHead = "h2"
	h.tick()
	p := h.piece("P1")
	if !h.log.has("MergeInto") || h.log.count("Push") != 1 || p.Head != "h2" || p.Status != PiecePROpen || !p.PRSince.Equal(testNow) {
		t.Fatalf("calls %v piece %+v", h.log.calls, p)
	}
	if !h.log.has("Fetch main") || !slices.Contains(h.kinds(), EvConflict) {
		t.Fatalf("calls %v kinds %v", h.log.calls, h.kinds())
	}
}

func TestEngine_ConflictAlreadyMergedDoesNothing(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.tick()
	p := h.piece("P1")
	if h.log.has("Push") || !p.PRSince.Equal(oldSince) || p.Head != "h1" {
		t.Fatalf("calls %v piece %+v", h.log.calls, p)
	}
	if len(h.events(EvConflict)) != 0 {
		t.Fatalf("events = %v", h.kinds())
	}
	notes := h.events(EvNote)
	if len(notes) != 1 || !strings.Contains(notes[0].Msg, "merges into the branch cleanly; waiting for GitHub") {
		t.Fatalf("notes = %+v", notes)
	}
}

func TestEngine_ConflictDirtyWorktreeIsANote(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.git.dirty = []string{"wip.go"}
	h.tick()
	if h.log.has("MergeInto") || !strings.Contains(noteText(h), "wip.go") {
		t.Fatalf("calls %v notes %q", h.log.calls, noteText(h))
	}
}

func TestEngine_ConflictSendsBack(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.git.conflicts = []string{"f.go"}
	h.tick()
	if h.log.has("Push") || len(h.runs.steps) != 1 {
		t.Fatalf("calls %v", h.log.calls)
	}
	st := h.runs.steps[0]
	if st.Kind != "goto" || !strings.Contains(st.Note, "f.go") || !strings.Contains(st.Note, "origin/main") {
		t.Fatalf("step = %+v", st)
	}
	p := h.piece("P1")
	if p.Status != PieceRunning || p.SentBack["conflict"] != 1 || p.Problem != "conflicts with main in: f.go" {
		t.Fatalf("piece = %+v", p)
	}
	if evs := h.events(EvConflict); len(evs) != 1 || !strings.Contains(evs[0].Msg, "f.go") {
		t.Fatalf("events = %v", h.kinds())
	}
}

func TestEngine_ConflictNoteCapsFiles(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	for i := range 23 {
		h.git.conflicts = append(h.git.conflicts, fmt.Sprintf("f%02d.go", i))
	}
	h.tick()
	note := h.runs.steps[0].Note
	if !strings.Contains(note, "f19.go") || strings.Contains(note, "f20.go") || !strings.Contains(note, "(and 3 more)") {
		t.Fatalf("note = %q", note)
	}
}

func TestEngine_SentBackPieceReturnsToSamePR(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.git.conflicts = []string{"f.go"}
	h.tick()
	h.git.conflicts, h.git.head = nil, "h3"
	h.runs.setRun("run-P1", doneState(), false)
	h.gh.existing["crew/"+h.id+"/P1"] = gh.PR{Number: 1, State: "OPEN", URL: "u"}
	h.tick()
	p := h.piece("P1")
	if h.log.count("Push") != 1 || h.log.has("CreatePR") || p.Status != PiecePROpen || p.PR != 1 || p.Head != "h3" || !p.PRSince.Equal(testNow) {
		t.Fatalf("calls %v piece %+v", h.log.calls, p)
	}
}

func TestEngine_OpenPRRecordsHead(t *testing.T) {
	h := gatePiece(t)
	h.git.head = "h9"
	h.tick()
	if p := h.piece("P1"); p.Status != PiecePROpen || p.Head != "h9" {
		t.Fatalf("piece = %+v", p)
	}
}

func TestEngine_OpenPRHeadFailureRecordsNothing(t *testing.T) {
	h := gatePiece(t)
	h.fail.failNext("Head", 1)
	h.tick()
	if p := h.piece("P1"); p.Status != PieceRunning || p.PR != 0 || len(h.events(EvNote)) != 1 {
		t.Fatalf("piece = %+v", p)
	}
}

func TestEngine_ConflictAskUsesProblem(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.conflicting()
	h.edit(func(s *State) {
		s.Pieces[0].SentBack = map[string]int{"conflict": 2}
		s.Pieces[0].Problem = "conflicts with main in: f.go"
	})
	h.orch.err = errors.New("no orchestrator")
	h.tick()
	if len(h.orch.sums) != 1 {
		t.Fatalf("calls: %v", h.log.calls)
	}
	sum := h.orch.sums[0]
	if sum.Failure != "conflict" || !strings.Contains(sum.Reason, "f.go") || slices.Contains(sum.Allowed, "retry") {
		t.Fatalf("summary = %+v", sum)
	}
}

func TestEngine_ChecksFailedAskUsesObservedChecks(t *testing.T) {
	h := prHarness(t, principal.AutoOn, failing())
	h.edit(func(s *State) { s.Pieces[0].SentBack = map[string]int{"checks-failed": 2} })
	h.orch.err = errors.New("no orchestrator")
	h.tick()
	if len(h.orch.sums) != 1 || h.orch.sums[0].Reason != "failed checks: unit" {
		t.Fatalf("sums = %+v", h.orch.sums)
	}
}
