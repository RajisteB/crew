package mission

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

var t0 = time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)

func lim(n int) source.Limit { return source.Limit{Set: true, N: n} }

func limits(conc, calls int) Limits {
	return Limits{Concurrency: lim(conc), OrchestratorCalls: lim(calls)}
}

func mis(deps map[string][]string, ids ...string) Mission {
	var m Mission
	for _, id := range ids {
		m.Pieces = append(m.Pieces, Piece{ID: id, Factory: "f", WaitsFor: deps[id]})
	}
	return m
}

// st builds a running mission whose pieces have the given statuses, in order.
func st(m Mission, statuses ...PieceStatus) State {
	s := State{MissionID: "m-1", Status: StatusRunning, NextWait: 1}
	for i, p := range m.Pieces {
		s.Pieces = append(s.Pieces, PieceState{ID: p.ID, Status: statuses[i]})
	}
	return s
}

func obs(now time.Time) Obs {
	return Obs{Now: now, Runs: map[string]RunObs{}, Git: map[string]GitObs{}, PRs: map[string]gh.PR{}}
}

func piece(t *testing.T, s State, id string) PieceState {
	t.Helper()
	p, ok := s.Piece(id)
	if !ok {
		t.Fatalf("no piece %s", id)
	}
	return p
}

// of returns the actions of type T, in order.
func of[T Action](as []Action) []T {
	var out []T
	for _, a := range as {
		if v, ok := a.(T); ok {
			out = append(out, v)
		}
	}
	return out
}

func recordKinds(as []Action) []string {
	var k []string
	for _, r := range of[Record](as) {
		k = append(k, r.Event.Kind)
	}
	return k
}

func hasKind(as []Action, kind string) bool {
	for _, k := range recordKinds(as) {
		if k == kind {
			return true
		}
	}
	return false
}

func doneRun() RunObs { return RunObs{State: run.State{Status: run.StatusDone}} }

func TestDecide_StartsFirstReadyPiece(t *testing.T) {
	m := mis(nil, "P1", "P2")
	s := st(m, PieceQueued, PieceQueued)
	_, as := Decide(m, s, obs(t0), limits(1, 3), 0)
	got := of[StartPiece](as)
	if len(got) != 1 || got[0].Piece != "P1" {
		t.Fatalf("want StartPiece P1 only, got %+v", as)
	}
}

func TestDecide_RespectsWaitsAndConcurrency(t *testing.T) {
	m := mis(map[string][]string{"P2": {"P1"}}, "P1", "P2", "P3")
	s := st(m, PieceRunning, PieceQueued, PieceQueued)
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusRunning}, Live: true}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	if got := of[StartPiece](as); len(got) != 0 {
		t.Fatalf("nothing should start with concurrency 1: %+v", got)
	}
	// With room, P2 still waits for P1 and P3 starts.
	_, as = Decide(m, s, o, limits(2, 3), 0)
	got := of[StartPiece](as)
	if len(got) != 1 || got[0].Piece != "P3" {
		t.Fatalf("want P3 only, got %+v", got)
	}
}

func TestDecide_DoneCleanAheadOpensPR(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	o.Git["P1"] = GitObs{Ahead: 2}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	if got := of[OpenPR](as); len(got) != 1 || got[0].Piece != "P1" {
		t.Fatalf("want OpenPR, got %+v", as)
	}
	if piece(t, ns, "P1").Status != PieceRunning {
		t.Fatal("Decide must not record the PR as opened")
	}
}

func TestDecide_DoneNoCommitsCompletes(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	o.Git["P1"] = GitObs{}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	if piece(t, ns, "P1").Status != PieceComplete {
		t.Fatalf("want complete, got %s", piece(t, ns, "P1").Status)
	}
	if len(of[Cleanup](as)) != 1 || !hasKind(as, EvPieceComplete) {
		t.Fatalf("want Cleanup and piece-complete, got %+v", as)
	}
	if !hasKind(as, EvMissionDone) || ns.Status != StatusDone {
		t.Fatal("the only piece completing should finish the mission")
	}
}

func TestDecide_DoneWithoutGitObsDoesNothing(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	_, as := Decide(m, s, o, limits(1, 3), 0)
	if len(as) != 0 {
		t.Fatalf("want no actions, got %+v", as)
	}
}

func TestDecide_UncommittedSentBackOnceThenAsked(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	o.Git["P1"] = GitObs{Dirty: []string{"a.go", "b.go"}}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	sb := of[SendBack](as)
	if len(sb) != 1 || sb[0].Station != "" || sb[0].Reason != "uncommitted" {
		t.Fatalf("want SendBack uncommitted, got %+v", as)
	}
	if !strings.Contains(sb[0].Note, "a.go") || !strings.Contains(sb[0].Note, "b.go") {
		t.Fatalf("note should list the paths: %q", sb[0].Note)
	}
	s = SentBackDone(s, "P1", "uncommitted")
	_, as = Decide(m, s, o, limits(1, 3), 0)
	ask := of[AskOrchestrator](as)
	if len(ask) != 1 || ask[0].Failure != "uncommitted" || len(of[SendBack](as)) != 0 {
		t.Fatalf("want AskOrchestrator uncommitted, got %+v", as)
	}
}

func TestDecide_UncommittedNoteCapsPaths(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	var dirty []string
	for i := 0; i < 30; i++ {
		dirty = append(dirty, fmt.Sprintf("f%02d.go", i))
	}
	o.Git["P1"] = GitObs{Dirty: dirty}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	note := of[SendBack](as)[0].Note
	if !strings.Contains(note, "f19.go") || strings.Contains(note, "f20.go") {
		t.Fatalf("want exactly 20 paths: %q", note)
	}
}

func TestDecide_RetryableResumedTwiceThenAsked(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusFailed, Retryable: true}}
	for i := 0; i < 2; i++ {
		_, as := Decide(m, s, o, limits(1, 3), 0)
		if len(of[DriveRun](as)) != 1 {
			t.Fatalf("resume %d: want DriveRun, got %+v", i, as)
		}
		s = Resumed(s, "P1")
	}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	ask := of[AskOrchestrator](as)
	if len(ask) != 1 || ask[0].Failure != "failed" {
		t.Fatalf("want AskOrchestrator failed, got %+v", as)
	}
}

func TestDecide_LimitHitAsksWithFailure(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusFailed, LimitRoute: "build.fail"}}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	ask := of[AskOrchestrator](as)
	if len(ask) != 1 || ask[0].Failure != "limit-hit" {
		t.Fatalf("want limit-hit, got %+v", as)
	}
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusFailed}}
	_, as = Decide(m, s, o, limits(1, 3), 0)
	if ask := of[AskOrchestrator](as); len(ask) != 1 || ask[0].Failure != "failed" {
		t.Fatalf("want failed, got %+v", as)
	}
}

func TestDecide_CallLimitGoesToPrincipal(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Calls = 3
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusFailed}}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if len(of[AskOrchestrator](as)) != 0 || p.Wait == nil || p.Wait.Kind != WaitLimit {
		t.Fatalf("want limit wait, got %+v %+v", p.Wait, as)
	}
	if p.Status != PieceWaiting || p.Wait.Back != PieceRunning || p.Wait.ID != "w1" || ns.NextWait != 2 {
		t.Fatalf("bad wait bookkeeping: %+v next=%d", p, ns.NextWait)
	}
	if !hasKind(as, EvWaitingOnPrincipal) || len(of[Notify](as)) != 1 {
		t.Fatalf("want waiting-on-principal and Notify, got %+v", as)
	}
}

func TestDecide_ApprovalStopWaitsOnPrincipal(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{
		Status:  run.StatusAwaitingApproval,
		Pending: &run.Pending{Station: "plan", Verdict: run.Verdict{Summary: "Plan ready: 3 steps"}},
	}}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if p.Status != PieceWaiting || p.Wait == nil || p.Wait.Kind != WaitApproval || p.Wait.Question != "Plan ready: 3 steps" {
		t.Fatalf("bad approval wait: %+v", p)
	}
	if len(of[Notify](as)) != 1 {
		t.Fatalf("want one Notify, got %+v", as)
	}
}

func TestDecide_NoRunObsDrivesRun(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	o := obs(t0)
	_, as := Decide(m, s, o, limits(1, 3), 0)
	if len(of[DriveRun](as)) != 1 {
		t.Fatalf("want DriveRun, got %+v", as)
	}
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusInterrupted}}
	_, as = Decide(m, s, o, limits(1, 3), 0)
	if len(of[DriveRun](as)) != 1 {
		t.Fatalf("want DriveRun for dead interrupted run, got %+v", as)
	}
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusRunning}, Live: true}
	_, as = Decide(m, s, o, limits(1, 3), 0)
	if len(as) != 0 {
		t.Fatalf("live run needs nothing, got %+v", as)
	}
}

func TestDecide_AllCompleteFinishes(t *testing.T) {
	m := mis(nil, "P1", "P2")
	s := st(m, PieceComplete, PieceCancelled)
	ns, as := Decide(m, s, obs(t0), limits(1, 3), 0)
	if ns.Status != StatusDone || !hasKind(as, EvMissionDone) || len(of[Notify](as)) != 1 {
		t.Fatalf("want done, record and Notify: %v %+v", ns.Status, as)
	}
}

func TestDecide_NotRunningDoesNothing(t *testing.T) {
	m := mis(nil, "P1")
	for _, status := range []Status{StatusPlanned, StatusAwaitingGo, StatusStopped, StatusDone} {
		s := st(m, PieceQueued)
		s.Status = status
		ns, as := Decide(m, s, obs(t0), limits(1, 3), 0)
		if len(as) != 0 || !reflect.DeepEqual(ns, s) {
			t.Fatalf("%s: want no change, got %+v", status, as)
		}
	}
}

func TestDecide_DoesNotMutateInput(t *testing.T) {
	m := mis(map[string][]string{"P2": {"P1"}, "P3": {"P1"}}, "P1", "P2", "P3", "P4")
	s := st(m, PiecePROpen, PieceQueued, PieceQueued, PieceWaiting)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: t0}
	s.Pieces[0].SentBack = map[string]int{"uncommitted": 1}
	s.Pieces[0].Decisions = []Decision{{Time: t0, By: "engine", Note: "x"}}
	s.Pieces[3].Wait = &Wait{ID: "w2", Kind: WaitQuestion, Question: "q", Since: t0, Back: PieceRunning}
	s.NextWait = 3
	wantS, wantM := s.Clone(), m
	wantM.Pieces = append([]Piece(nil), m.Pieces...)
	o := obs(t0.Add(time.Hour))
	o.PRs["P1"] = gh.PR{Number: 12, State: "MERGED"}
	Decide(m, s, o, limits(1, 3), time.Minute)
	if !reflect.DeepEqual(s, wantS) {
		t.Fatalf("state mutated:\n got %+v\nwant %+v", s, wantS)
	}
	if !reflect.DeepEqual(m, wantM) {
		t.Fatal("mission mutated")
	}
}

func TestReducers_ReturnNewState(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceQueued)
	orig := s.Clone()
	started := Started(s, "P1", "run-1", "crew/m-1/P1", "/wt")
	p := piece(t, started, "P1")
	if p.Status != PieceRunning || p.RunID != "run-1" || p.Branch != "crew/m-1/P1" || p.Worktree != "/wt" {
		t.Fatalf("Started: %+v", p)
	}
	if p := piece(t, Resumed(started, "P1"), "P1"); p.Resumes != 1 {
		t.Fatalf("Resumed: %+v", p)
	}
	if p := piece(t, SentBackDone(started, "P1", "uncommitted"), "P1"); p.SentBack["uncommitted"] != 1 {
		t.Fatalf("SentBackDone: %+v", p)
	}
	if p := piece(t, Asked(started, "P1"), "P1"); p.Calls != 1 {
		t.Fatalf("Asked: %+v", p)
	}
	if p := piece(t, CleanedUp(started, "P1"), "P1"); p.Worktree != "" || piece(t, started, "P1").Worktree != "/wt" {
		t.Fatalf("CleanedUp: %+v", p)
	}
	opened := PROpened(started, "P1", gh.PR{Number: 7, State: "OPEN", URL: "u"})
	if p := piece(t, opened, "P1"); p.PR != 7 || p.PRURL != "u" || p.Status != PiecePROpen {
		t.Fatalf("PROpened: %+v", p)
	}
	if !reflect.DeepEqual(s, orig) {
		t.Fatal("reducers modified their argument")
	}
	if piece(t, started, "P1").SentBack != nil || piece(t, started, "P1").Resumes != 0 {
		t.Fatal("reducers modified an earlier result")
	}
}
