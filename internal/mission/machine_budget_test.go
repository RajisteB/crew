package mission

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func stepLimits(steps source.Limit) Limits {
	return Limits{Concurrency: lim(5), OrchestratorCalls: lim(3), Steps: steps}
}

func runAt(step int) RunObs {
	return RunObs{State: run.State{Status: run.StatusRunning, Step: step}, Live: true}
}

func budgetRecords(as []Action) []Event {
	var out []Event
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvBudgetHit {
			out = append(out, r.Event)
		}
	}
	return out
}

func TestStepsUsed(t *testing.T) {
	s := State{Pieces: []PieceState{{Steps: 3}, {}, {Steps: 4}}}
	if got := StepsUsed(s); got != 7 {
		t.Fatalf("StepsUsed = %d, want 7", got)
	}
}

func TestDecide_RecordsStepsFromRuns(t *testing.T) {
	m := mis(nil, "a", "b")
	s := st(m, PieceRunning, PieceRunning)
	s.Pieces[0].Steps, s.Pieces[1].Steps = 2, 4
	o := obs(t0)
	o.Runs["a"], o.Runs["b"] = runAt(5), runAt(1)
	ns, _ := Decide(m, s, o, stepLimits(lim(100)), 0)
	if a, b := piece(t, ns, "a").Steps, piece(t, ns, "b").Steps; a != 5 || b != 4 {
		t.Fatalf("steps = %d, %d; want 5, 4", a, b)
	}
}

func TestDecide_BudgetHitHoldsOnce(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PieceRunning)
	s.Pieces[0].Steps = 6
	o := obs(t0)
	o.Runs["a"] = runAt(6)
	s1, as := Decide(m, s, o, stepLimits(lim(6)), 0)
	if s1.Hold == nil || s1.Hold.Kind != "steps" || s1.Hold.Count != 6 || s1.Hold.Max != "6" || !s1.Hold.Since.Equal(t0) {
		t.Fatalf("hold = %+v", s1.Hold)
	}
	rec := budgetRecords(as)
	if len(rec) != 1 || rec[0].Count != 6 || rec[0].Max != "6" || rec[0].Msg != "station steps for this mission" {
		t.Fatalf("records = %+v", rec)
	}
	ns := of[Notify](as)
	want := "step budget reached (6 of 6): no new piece starts and nothing is sent back until limits.steps is raised in missions.yaml"
	if len(ns) != 1 || ns[0].Body != want {
		t.Fatalf("notifies = %+v", ns)
	}
	_, as2 := Decide(m, s1, o, stepLimits(lim(6)), 0)
	if len(budgetRecords(as2)) != 0 || len(of[Notify](as2)) != 0 {
		t.Fatalf("second decide repeated itself: %+v", as2)
	}
}

// heldCase: a mission over budget with one piece of each kind of work.
func heldCase() (Mission, State, Obs) {
	m := mis(nil, "q", "dirty", "pr", "ok")
	s := st(m, PieceQueued, PieceRunning, PiecePROpen, PieceRunning)
	s.Hold = &Hold{Kind: "steps", Count: 6, Max: "6", Since: t0}
	s.Pieces[1].Steps = 6
	s.Pieces[2].PR, s.Pieces[2].Head, s.Pieces[2].PRSince = 7, "abc", t0.Add(-1e12)
	o := obs(t0)
	o.Runs["dirty"] = RunObs{State: run.State{Status: run.StatusDone, Step: 6}}
	o.Git["dirty"] = GitObs{Ahead: 1, Dirty: []string{"x.go"}}
	o.Runs["ok"] = RunObs{State: run.State{Status: run.StatusDone}}
	o.Git["ok"] = GitObs{Ahead: 1}
	o.PRs["pr"] = gh.PR{Number: 7, State: "OPEN", Mergeable: "MERGEABLE", HeadSHA: "abc", Checks: []gh.Check{chk("fail")}}
	return m, s, o
}

func TestDecide_HoldDropsNewWorkOnly(t *testing.T) {
	m, s, o := heldCase()
	_, as := Decide(m, s, o, stepLimits(lim(6)), 0)
	if n := len(of[StartPiece](as)) + len(of[SendBack](as)) + len(of[AskOrchestrator](as)) + len(of[DriveRun](as)); n != 0 {
		t.Fatalf("new work was not dropped: %#v", as)
	}
	if got := of[OpenPR](as); len(got) != 1 || got[0].Piece != "ok" {
		t.Fatalf("OpenPR = %+v", got)
	}
}

func TestDecide_HoldKeepsDeliverablesAndWaits(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PieceRunning)
	s.Hold = &Hold{Kind: "steps", Count: 6, Max: "6", Since: t0}
	s.Pieces[0].Steps = 6
	o := obs(t0)
	o.Runs["a"] = RunObs{State: run.State{Status: run.StatusAwaitingApproval, Step: 6}}
	ns, as := Decide(m, s, o, stepLimits(lim(6)), 0)
	if p := piece(t, ns, "a"); p.Wait == nil || p.Wait.Kind != WaitApproval || !hasKind(as, EvWaitingOnPrincipal) {
		t.Fatalf("wait not opened: %+v %+v", p, as)
	}
}

func TestDecide_HoldLiftsWhenTheLimitRises(t *testing.T) {
	m := mis(nil, "a", "q")
	s := st(m, PieceRunning, PieceQueued)
	s.Hold = &Hold{Kind: "steps", Count: 6, Max: "6", Since: t0}
	s.Pieces[0].Steps = 6
	o := obs(t0)
	o.Runs["a"] = runAt(6)
	ns, as := Decide(m, s, o, stepLimits(lim(10)), 0)
	if ns.Hold != nil {
		t.Fatalf("hold = %+v", ns.Hold)
	}
	var note *Event
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvNote {
			note = &r.Event
		}
	}
	if note == nil || note.Msg != "step budget raised to 10: work goes on" {
		t.Fatalf("note = %+v", note)
	}
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "q" {
		t.Fatalf("starts = %+v", got)
	}
}

func TestDecide_UnlimitedStepsNeverHold(t *testing.T) {
	m := mis(nil, "a", "q")
	s := st(m, PieceRunning, PieceQueued)
	s.Pieces[0].Steps = 5000
	o := obs(t0)
	o.Runs["a"] = runAt(5000)
	ns, as := Decide(m, s, o, stepLimits(source.Limit{Set: true, Unlimited: true}), 0)
	if ns.Hold != nil || len(budgetRecords(as)) != 0 || len(of[StartPiece](as)) != 1 {
		t.Fatalf("hold = %+v actions = %#v", ns.Hold, as)
	}
}

func TestDecide_CallLimitRecordsBudgetHit(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Calls = 3
	o := obs(t0)
	o.Runs["P1"] = RunObs{State: run.State{Status: run.StatusFailed}}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	rec := budgetRecords(as)
	if len(rec) != 1 || rec[0].Piece != "P1" || rec[0].Count != 3 || rec[0].Max != "3" || rec[0].Msg != "orchestrator calls for this piece" {
		t.Fatalf("records = %+v", rec)
	}
}

func TestDecide_StepBudgetNoticeNamesTheLimit(t *testing.T) {
	m := mis(nil, "a")
	s := st(m, PieceRunning)
	s.Pieces[0].Steps = 2
	o := obs(t0)
	o.Runs["a"] = runAt(2)
	_, as := Decide(m, s, o, stepLimits(lim(2)), 0)
	if n := of[Notify](as); len(n) != 1 || !strings.Contains(n[0].Body, "(2 of 2)") {
		t.Fatalf("notifies = %+v", n)
	}
}
