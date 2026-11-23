package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/mission"
)

// delegateOrch stands in for the orchestrator, the main one and the scoped
// one: plan answers a planning call, decide a call about a stuck piece. The
// test may swap either between ticks.
type delegateOrch struct {
	mu     sync.Mutex
	plan   func() mission.SubPlan
	decide func(mission.Summary) mission.OrchDecision
}

func (o *delegateOrch) set(plan func() mission.SubPlan, decide func(mission.Summary) mission.OrchDecision) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.plan, o.decide = plan, decide
}

func (o *delegateOrch) Plan(context.Context, string, mission.PlanRequest) (mission.SubPlan, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.plan(), nil
}

func (o *delegateOrch) Decide(_ context.Context, _ string, s mission.Summary) (mission.OrchDecision, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.decide(s), nil
}

func escalateAll(mission.Summary) mission.OrchDecision {
	return mission.OrchDecision{Action: "escalate", Note: "What now?", Reason: "test"}
}

// twoSubPieces plans two sub-pieces under brand/**, the second waiting for the first.
func twoSubPieces() mission.SubPlan {
	a, b := touching("a", "brand", nil), touching("b", "brand", nil, "a")
	return mission.SubPlan{Pieces: []mission.Piece{a, b}, Reason: "two steps"}
}

// delegatedRig launches P1 (delegated, brand/**) and, when withP2, P2 waiting for P1.
func delegatedRig(t *testing.T, orch *delegateOrch, withP2 bool) *e2eRig {
	t.Helper()
	return delegatedRigWith(t, orch, withP2, "fakeharness-paths.sh")
}

// delegatedRigWith is delegatedRig with the station harness script of the test's choice
// (a path, or a name under testdata).
func delegatedRigWith(t *testing.T, orch *delegateOrch, withP2 bool, script string) *e2eRig {
	t.Helper()
	r := newPathsRig(t, "3", "")
	if script != "fakeharness-paths.sh" {
		editFile(t, filepath.Join(r.crewDir, "harnesses.yaml"), testdataPath("fakeharness-paths.sh"), script)
	}
	p1 := mission.Piece{ID: "P1", Domain: "design", Delegate: true, Brief: "Do the brand.", Paths: []string{"brand/**"}}
	pieces := []mission.Piece{p1}
	if withP2 {
		pieces = append(pieces, touching("P2", ".", nil, "P1"))
	}
	r.launchPieces(pieces...)
	r.engine.Orch = orch
	return r
}

func TestMission_EndToEnd_DelegatedPiece(t *testing.T) {
	orch := &delegateOrch{}
	orch.set(twoSubPieces, escalateAll)
	r := delegatedRig(t, orch, true)

	st := r.tick()
	if got := piece(t, st, "P1").Status; got != mission.PieceDelegated {
		t.Fatalf("P1 is %s after the first tick, want delegated", got)
	}
	st = r.tickUntil("P1.1 running", func(s mission.State) bool { p, ok := s.Piece("P1.1"); return ok && p.Status == mission.PieceRunning })
	if got := piece(t, st, "P1").Status; got != mission.PieceDelegated {
		t.Fatalf("P1 is %s while P1.1 runs, want delegated", got)
	}
	if out := r.cmd("", "status", r.id); !strings.Contains(out, "\nDelegated:\n  P1  2 pieces: 1 running, 1 queued\n") {
		t.Fatalf("status has no Delegated section:\n%s", out)
	}
	r.tickUntil("P1.1's pull request", hasPR(t, "P1.1"))
	if got := piece(t, r.tick(), "P1.2").Status; got != mission.PieceQueued {
		t.Fatalf("P1.2 is %s before P1.1 is merged, want queued", got)
	}
	r.mergePR()
	r.tickUntil("P1.2 started", func(s mission.State) bool {
		return piece(t, s, "P1.1").Status == mission.PieceComplete && piece(t, s, "P1.2").Status != mission.PieceQueued
	})
	r.tickUntil("P1.2's pull request", hasPR(t, "P1.2"))
	r.mergePR()
	st = r.tickUntil("P1 complete", func(s mission.State) bool { return piece(t, s, "P1").Status == mission.PieceComplete })
	if got := piece(t, st, "P1.2").Status; got != mission.PieceComplete {
		t.Fatalf("P1 is complete but P1.2 is %s", got)
	}
	r.tickUntil("P2's pull request", hasPR(t, "P2"))
	r.mergePR()
	r.tickUntil("the mission done", func(s mission.State) bool { return s.Status == mission.StatusDone })

	text, err := os.ReadFile(filepath.Join(r.store.Dir, "mission.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"id: P1.1", "id: P1.2", "scoped orchestrator P1", "planned P1 as P1.1, P1.2"} {
		if !strings.Contains(string(text), want) {
			t.Errorf("mission.md lacks %q:\n%s", want, text)
		}
	}
}

func TestMission_EndToEnd_SubPlanOutsideItsPathsIsRefused(t *testing.T) {
	orch := &delegateOrch{}
	outside := func() mission.SubPlan {
		p := touching("a", "site", []string{"site/**"})
		return mission.SubPlan{Pieces: []mission.Piece{p}, Reason: "site work"}
	}
	orch.set(outside, escalateAll)
	r := delegatedRig(t, orch, false)

	var wait *mission.Wait
	r.tickUntil("a plan wait", func(s mission.State) bool { wait = piece(t, s, "P1").Wait; return wait != nil })
	if wait.Kind != mission.WaitPlan {
		t.Fatalf("wait kind = %s, want plan", wait.Kind)
	}
	out := r.cmd("", "status", r.id)
	if !strings.Contains(out, "  "+wait.ID+"  P1  plan  ") || !strings.Contains(out, "site/**") {
		t.Fatalf("status does not show the plan wait with its reason:\n%s", out)
	}
	if got := piece(t, r.tick(), "P1"); got.Status == mission.PieceDelegated || got.Wait == nil {
		t.Fatalf("P1 is %s with wait %v, want it unplanned and still waiting", got.Status, got.Wait)
	}

	orch.set(twoSubPieces, escalateAll)
	r.cmd("", "answer", r.id, wait.ID, "--do", "retry", "--words", "try again")
	r.tickUntil("P1 planned", func(s mission.State) bool { return piece(t, s, "P1").Status == mission.PieceDelegated })
}

func TestMission_EndToEnd_StuckSubPieceReachesThePrincipal(t *testing.T) {
	orch := &delegateOrch{}
	orch.set(twoSubPieces, escalateAll)
	failing := filepath.Join(t.TempDir(), "fail.sh")
	if err := os.WriteFile(failing, []byte("#!/bin/sh\ncat > /dev/null\necho broken >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	r := delegatedRigWith(t, orch, false, failing)

	r.tickUntil("a question on P1.1", func(s mission.State) bool {
		w := piece(t, s, "P1.1").Wait
		return w != nil && w.Kind == mission.WaitQuestion
	})
	out := r.cmd("", "status", r.id)
	if n := strings.Count(out, "  question  "); n != 1 {
		t.Fatalf("status shows %d question waits, want 1:\n%s", n, out)
	}
	var climb []string
	for _, l := range strings.Split(out, "\n") {
		if _, rest, ok := strings.Cut(l, "  escalated  P1.1  "); ok {
			climb = append(climb, rest)
		}
	}
	if want := []string{"scoped orchestrator P1  What now?", "orchestrator  What now?"}; !slices.Equal(climb, want) {
		t.Fatalf("status shows the climb as %q, want %q:\n%s", climb, want, out)
	}
	var by []string
	evs, err := r.store.EventsSince(0)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if e.Kind == mission.EvEscalated && e.Piece == "P1.1" {
			by = append(by, e.By)
		}
	}
	if want := []string{"scoped orchestrator P1", "orchestrator"}; !slices.Equal(by, want) {
		t.Fatalf("escalated by %v, want %v\nevents: %v", by, want, r.events())
	}
}

func TestMissionResume_WithADelegatedPiece(t *testing.T) {
	orch := &delegateOrch{}
	orch.set(twoSubPieces, escalateAll)
	r := delegatedRig(t, orch, false)

	st := r.tickUntil("P1.1 running", func(s mission.State) bool { p, ok := s.Piece("P1.1"); return ok && p.Status == mission.PieceRunning })
	runID := piece(t, st, "P1.1").RunID
	// The engine dies: its runs stop and the mission is left stopped.
	_ = r.runs.Stop(runID)
	r.runs.Wait()
	st, err := r.store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	st.Status = mission.StatusStopped
	if err := r.store.SaveState(st); err != nil {
		t.Fatal(err)
	}

	r.cmd("", "resume", r.id)
	root, err := gitx.Repo{Dir: r.repo}.Root(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	engine, runs, err := newMissionEngine(r.store, r.crewDir, root, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	r.engine, r.runs = engine, runs
	r.engine.Orch = orch
	st = r.tickUntil("P1.1's pull request", hasPR(t, "P1.1"))
	if got := piece(t, st, "P1.1").RunID; got != runID {
		t.Fatalf("P1.1 run is %s after resume, want the same run %s", got, runID)
	}
	if got := piece(t, st, "P1").Status; got != mission.PieceDelegated {
		t.Fatalf("P1 is %s, want delegated", got)
	}
}
