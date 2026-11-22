package mission

import (
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

var briefFiles = []string{"brief.md", "plan.md"}

// deliveredMission is one piece in the product domain.
func deliveredMission(ids ...string) Mission {
	m := mis(nil, ids...)
	for i := range m.Pieces {
		m.Pieces[i].Domain = "product"
	}
	return m
}

// doneWithDeliverable is P1 with a finished run, a clean worktree with no
// commits, and a collected, unaccepted deliverable.
func doneWithDeliverable() (Mission, State, Obs) {
	m := deliveredMission("P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p", Posted: t0}
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	o.Git["P1"] = GitObs{}
	o.Delivered = map[string]Found{"P1": {Files: briefFiles, Print: "p"}}
	return m, s, o
}

func acceptEvents(as []Action) []Event {
	var out []Event
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvDeliverableAccepted {
			out = append(out, r.Event)
		}
	}
	return out
}

func TestDecide_DeliverableOnlyPieceWaitsWhenSwitchOff(t *testing.T) {
	m, s, o := doneWithDeliverable()
	o.Accept = map[string]AcceptObs{"P1": {Mode: principal.AutoOff}}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if p.Status != PieceDelivered || p.Wait == nil || p.Wait.Kind != WaitDeliverable || p.Wait.Back != "" {
		t.Fatalf("piece = %+v", p)
	}
	if want := "deliverable to accept (brief.md, plan.md) in .crew/missions/m-1/deliverables/P1: answer accept"; p.Wait.Question != want {
		t.Fatalf("question = %q, want %q", p.Wait.Question, want)
	}
	if len(of[Cleanup](as)) != 0 {
		t.Fatalf("cleanup: %+v", as)
	}
	if !hasKind(as, EvWaitingOnPrincipal) || len(of[Notify](as)) != 1 {
		t.Fatalf("want a waiting record and a notification: %+v", as)
	}
	if len(acceptEvents(as)) != 0 {
		t.Fatalf("accepted: %+v", as)
	}
}

func TestDecide_WaitQuestionNamesTheIssueAndCountsFiles(t *testing.T) {
	m, s, o := doneWithDeliverable()
	s.Pieces[0].Deliverable.Files = []string{"a", "b", "c", "d", "e"}
	s.Pieces[0].Jira = Tracked{Issue: "ACME-5", Lane: LaneInProgress}
	ns, _ := Decide(m, s, o, limits(1, 3), 0)
	want := "deliverable to accept (a, b, c and 2 more): move ACME-5 to Done on the board, or answer accept"
	if got := piece(t, ns, "P1").Wait; got == nil || got.Question != want {
		t.Fatalf("wait = %+v, want %q", got, want)
	}
}

func TestDecide_SwitchOnAcceptsAtOnce(t *testing.T) {
	for _, mode := range []principal.AutoMode{principal.AutoOn, principal.AutoWithoutChecks} {
		m, s, o := doneWithDeliverable()
		o.Accept = map[string]AcceptObs{"P1": {Mode: mode}}
		ns, as := Decide(m, s, o, limits(1, 3), 0)
		p := piece(t, ns, "P1")
		if p.Status != PieceComplete || p.Wait != nil || p.Deliverable == nil || !p.Deliverable.Accepted || p.Deliverable.By != "engine" {
			t.Fatalf("%s: piece = %+v", mode, p)
		}
		ev := acceptEvents(as)
		want := "auto-merge is " + string(mode) + " for product"
		if len(ev) != 1 || ev[0].By != "engine" || ev[0].Msg != want {
			t.Fatalf("%s: events = %+v", mode, ev)
		}
		if cl := of[Cleanup](as); len(cl) != 1 || !cl[0].Force {
			t.Fatalf("%s: cleanup = %+v", mode, cl)
		}
		if hasKind(as, EvWaitingOnPrincipal) {
			t.Fatalf("%s: opened a wait", mode)
		}
	}
}

func TestDecide_DoneOnTheBoardAcceptsOnlyAPostedDeliverable(t *testing.T) {
	m := deliveredMission("P1")
	moved := map[string]AcceptObs{"P1": {MovedDone: true}}

	// (a) a delivered piece with a deliverable: accepted by the principal.
	s := st(m, PieceDelivered)
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitDeliverable, Since: t0}
	o := obs(t0)
	o.Accept = moved
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p := piece(t, ns, "P1")
	ev := acceptEvents(as)
	if p.Status != PieceComplete || p.Wait != nil || len(ev) != 1 || ev[0].By != "principal" || ev[0].Msg != "its Jira issue was moved to Done" {
		t.Fatalf("(a) piece = %+v, events = %+v", p, ev)
	}
	if cl := of[Cleanup](as); len(cl) != 1 || !cl[0].Force {
		t.Fatalf("(a) cleanup = %+v", cl)
	}

	// (b) a pr-open piece with no deliverable: nothing changes.
	s = st(m, PiecePROpen)
	s.Pieces[0].PR = 7
	o = obs(t0)
	o.PRs["P1"] = gh.PR{Number: 7, State: "OPEN", Mergeable: "MERGEABLE"}
	o.Accept = moved
	before := s.Clone()
	ns, as = Decide(m, s, o, limits(1, 3), 0)
	if got := piece(t, ns, "P1"); got.Status != PiecePROpen || got.Deliverable != nil || len(acceptEvents(as)) != 0 {
		t.Fatalf("(b) piece = %+v", got)
	}
	if s.Pieces[0].Status != before.Pieces[0].Status {
		t.Fatal("Decide modified its argument")
	}

	// (c) a running piece whose run is not done: nothing changes.
	s = st(m, PieceRunning)
	o = obs(t0)
	o.Runs["P1"] = RunObs{Live: true}
	o.Accept = moved
	ns, as = Decide(m, s, o, limits(1, 3), 0)
	if got := piece(t, ns, "P1"); got.Status != PieceRunning || got.Deliverable != nil || len(acceptEvents(as)) != 0 {
		t.Fatalf("(c) piece = %+v", got)
	}
}

func TestDecide_AcceptOnAPROpenPieceChangesOnlyTheDeliverable(t *testing.T) {
	m := deliveredMission("P1")
	s := st(m, PiecePROpen)
	s.Pieces[0].PR = 7
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Question: "q", Since: t0}
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	o := obs(t0)
	o.PRs["P1"] = gh.PR{Number: 7, State: "OPEN", Mergeable: "MERGEABLE"}
	o.Accept = map[string]AcceptObs{"P1": {MovedDone: true}}
	ns, as := Decide(m, s, o, limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if p.Status != PiecePROpen || p.Wait == nil || p.Wait.Kind != WaitMerge || !p.Deliverable.Accepted || p.Deliverable.By != "principal" {
		t.Fatalf("piece = %+v", p)
	}
	if len(acceptEvents(as)) != 1 || len(of[Cleanup](as)) != 0 {
		t.Fatalf("actions = %+v", as)
	}
}

func TestDecide_ChangeAndDeliverable(t *testing.T) {
	m := deliveredMission("P1")
	merged := func(d *Deliverable) (PieceState, []Action) {
		s := st(m, PiecePROpen)
		s.Pieces[0].PR = 7
		s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge, Since: t0}
		s.Pieces[0].Deliverable = d
		o := obs(t0)
		o.PRs["P1"] = gh.PR{Number: 7, State: "MERGED"}
		ns, as := Decide(m, s, o, limits(1, 3), 0)
		return piece(t, ns, "P1"), as
	}
	p, as := merged(&Deliverable{Files: briefFiles, Print: "p"})
	if p.Status != PieceDelivered || p.Wait == nil || p.Wait.Kind != WaitDeliverable {
		t.Fatalf("unaccepted: %+v", p)
	}
	if len(of[Cleanup](as)) != 0 || !hasKind(as, EvPRMerged) || hasKind(as, EvPieceComplete) {
		t.Fatalf("unaccepted actions: %+v", as)
	}
	p, as = merged(&Deliverable{Files: briefFiles, Print: "p", Accepted: true, By: "principal"})
	if p.Status != PieceComplete || p.Wait != nil {
		t.Fatalf("accepted: %+v", p)
	}
	if cl := of[Cleanup](as); len(cl) != 1 || !cl[0].Force {
		t.Fatalf("accepted cleanup: %+v", cl)
	}
	p, _ = merged(nil)
	if p.Status != PieceComplete {
		t.Fatalf("no deliverable: %+v", p)
	}
}

func TestDecide_DeliveredDoesNotHoldARun(t *testing.T) {
	m := deliveredMission("P1", "P2")
	s := st(m, PieceDelivered, PieceQueued)
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitDeliverable, Since: t0}
	_, as := Decide(m, s, obs(t0), limits(1, 3), 0)
	if got := of[StartPiece](as); len(got) != 1 || got[0].Piece != "P2" {
		t.Fatalf("starts = %+v", got)
	}
}

func TestDecide_DependentWaitsForAcceptance(t *testing.T) {
	m := mis(map[string][]string{"P2": {"P1"}}, "P1", "P2")
	s := st(m, PieceDelivered, PieceQueued)
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitDeliverable, Since: t0}
	ns, as := Decide(m, s, obs(t0), limits(1, 3), 0)
	if len(of[StartPiece](as)) != 0 || ns.Status != StatusRunning {
		t.Fatalf("actions = %+v, status = %s", as, ns.Status)
	}
}

func TestDecide_StaleAcceptObsIsIgnored(t *testing.T) {
	m := deliveredMission("P1")
	s := st(m, PieceDelivered)
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p", Accepted: true, By: "engine"}
	o := obs(t0)
	o.Accept = map[string]AcceptObs{"P1": {Mode: principal.AutoOn, MovedDone: true}}
	_, as := Decide(m, s, o, limits(1, 3), 0)
	if len(acceptEvents(as)) != 0 {
		t.Fatalf("accepted twice: %+v", as)
	}
}

func TestMerged_GoesToDeliveredWhenUnaccepted(t *testing.T) {
	s := st(mis(nil, "P1"), PiecePROpen)
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitMerge}
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	p := piece(t, Merged(s, "P1"), "P1")
	if p.Status != PieceDelivered || p.Wait != nil {
		t.Fatalf("unaccepted: %+v", p)
	}
	s.Pieces[0].Deliverable.Accepted = true
	if p := piece(t, Merged(s, "P1"), "P1"); p.Status != PieceComplete {
		t.Fatalf("accepted: %+v", p)
	}
	if !strings.Contains(string(s.Pieces[0].Status), "pr-open") {
		t.Fatal("Merged modified its argument")
	}
}

func TestDecide_DeliveredWaitIsRemindedLikeAnyWait(t *testing.T) {
	m := deliveredMission("P1")
	s := st(m, PieceDelivered)
	s.Pieces[0].Deliverable = &Deliverable{Files: briefFiles, Print: "p"}
	s.Pieces[0].Wait = &Wait{ID: "w1", Kind: WaitDeliverable, Question: "q", Since: t0}
	_, as := Decide(m, s, obs(t0.Add(time.Hour)), limits(1, 3), 30*time.Minute)
	if len(of[Notify](as)) != 1 {
		t.Fatalf("actions = %+v", as)
	}
}
