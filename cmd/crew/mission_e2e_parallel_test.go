package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

// pathsRig is the end-to-end rig with the station harness that commits into
// the folder a piece's brief names, and runs_at_once set.
func newPathsRig(t *testing.T, runsAtOnce, steps string) *e2eRig {
	t.Helper()
	r := newE2ERig(t)
	editFile(t, filepath.Join(r.crewDir, "harnesses.yaml"), "fakeharness-commit.sh", "fakeharness-paths.sh")
	r.setLimit("runs_at_once", runsAtOnce)
	if steps != "" {
		r.setLimit("steps", steps)
	}
	r.confirmRepo()
	return r
}

// setLimit rewrites one limit in missions.yaml, the way the principal would.
func (r *e2eRig) setLimit(key, value string) {
	r.t.Helper()
	path := filepath.Join(r.crewDir, "missions.yaml")
	b, err := os.ReadFile(path)
	if err != nil {
		r.t.Fatal(err)
	}
	lines := strings.Split(string(b), "\n")
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), key+":") {
			lines[i] = "  " + key + ": " + value
			if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
				r.t.Fatal(err)
			}
			return
		}
	}
	r.t.Fatalf("missions.yaml has no %s", key)
}

// touching is a piece that changes one file in dir ("." is the top folder)
// and may touch paths.
func touching(id, dir string, paths []string, waits ...string) mission.Piece {
	p := e2ePiece(id, waits...)
	p.Brief = "Touch " + dir + " for " + id + "."
	p.Paths = paths
	return p
}

func (r *e2eRig) mergePR() {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.ghDir, "pr-state"), []byte("MERGED\n"), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// drive holds the mission lock until the test ends, as a live engine does,
// so the repo's other missions count this one as being driven.
func (r *e2eRig) drive() {
	r.t.Helper()
	unlock, err := r.store.Lock()
	if err != nil {
		r.t.Fatal(err)
	}
	r.driven = true
	r.t.Cleanup(unlock)
}

func hasPR(t *testing.T, id string) func(mission.State) bool {
	return func(s mission.State) bool { p := piece(t, s, id); return p.PR != 0 && p.Wait != nil }
}

func TestMission_EndToEnd_TwoPiecesTogether(t *testing.T) {
	r := newPathsRig(t, "2", "")
	r.launchPieces(touching("P1", "a", []string{"a/**"}), touching("P2", "b", []string{"b/**"}))

	st := r.tick()
	for _, id := range []string{"P1", "P2"} {
		if got := piece(t, st, id).Status; got != mission.PieceRunning {
			t.Fatalf("%s is %s after the first tick, want running", id, got)
		}
	}
	r.tickUntil("both pull requests", func(s mission.State) bool { return hasPR(t, "P1")(s) && hasPR(t, "P2")(s) })
	if n := strings.Count(r.ghFile("calls"), "pr create"); n != 2 {
		t.Fatalf("pr create ran %d times, want 2", n)
	}
	r.mergePR()
	r.tickUntil("both complete", func(s mission.State) bool {
		return piece(t, s, "P1").Status == mission.PieceComplete && piece(t, s, "P2").Status == mission.PieceComplete
	})
}

func TestMission_EndToEnd_OverlappingPiecesTakeTurns(t *testing.T) {
	r := newPathsRig(t, "2", "")
	plan := planFile("Build the thing.", touching("P1", "src", []string{"src/**"}), touching("P2", "src", []string{"src/**"}))
	planOut := r.cmd(string(plan), "plan")
	if !strings.Contains(planOut, "P2 starts after P1: their paths overlap") {
		t.Fatalf("plan does not name the separation:\n%s", planOut)
	}
	r.launchPieces(touching("P1", "src", []string{"src/**"}), touching("P2", "src", []string{"src/**"}))

	st := r.tickUntil("P1's pull request", hasPR(t, "P1"))
	if got := piece(t, st, "P2").Status; got != mission.PieceQueued {
		t.Fatalf("P2 is %s while P1's pull request is open, want queued", got)
	}
	if status := r.cmd("", "status", r.id); !strings.Contains(status, "P2  starts after P1 (paths overlap)") {
		t.Fatalf("status does not say why P2 waits:\n%s", status)
	}
	r.mergePR()
	r.tickUntil("P1 complete and P2 running", func(s mission.State) bool {
		return piece(t, s, "P1").Status == mission.PieceComplete && piece(t, s, "P2").Status != mission.PieceQueued
	})
}

func TestMission_EndToEnd_TwoMissionsTakeTurns(t *testing.T) {
	a := newPathsRig(t, "2", "")
	a.launchPieces(touching("P1", "src", []string{"src/**"}))
	b := *a
	b.launchPieces(touching("P1", "src", []string{"src/**"}))
	a.drive()

	a.tickUntil("mission A's pull request", hasPR(t, "P1"))
	st := b.tick()
	if got := piece(t, st, "P1").Status; got != mission.PieceQueued {
		t.Fatalf("the second mission's piece is %s, want queued", got)
	}
	want := "starts after P1 of mission " + a.id + " (paths overlap)"
	if status := b.cmd("", "status", b.id); !strings.Contains(status, want) {
		t.Fatalf("status does not name the other mission's piece (%q):\n%s", want, status)
	}
	a.mergePR()
	a.tickUntil("mission A's piece complete", func(s mission.State) bool { return piece(t, s, "P1").Status == mission.PieceComplete })
	b.tickUntil("the second mission's piece starts", func(s mission.State) bool {
		return piece(t, s, "P1").Status != mission.PieceQueued
	})
}

// escalateOrch stands in for the orchestrator: it escalates every call.
type escalateOrch struct{}

func (escalateOrch) Decide(context.Context, string, mission.Summary) (mission.OrchDecision, error) {
	return mission.OrchDecision{Action: "escalate", Note: "What now?", Reason: "test"}, nil
}

func (escalateOrch) Plan(context.Context, string, mission.PlanRequest) (mission.SubPlan, error) {
	return mission.SubPlan{Question: "What now?", Reason: "test"}, nil
}

func TestMission_EndToEnd_FilesOutsidePaths(t *testing.T) {
	r := newPathsRig(t, "2", "")
	r.launchPieces(touching("P1", ".", []string{"docs/**"}))
	r.engine.Orch = escalateOrch{}

	st := r.tickUntil("a paths wait", func(s mission.State) bool {
		p := piece(t, s, "P1")
		return p.Wait != nil && p.Wait.Kind == mission.WaitPaths
	})
	if n := strings.Count(r.ghFile("calls"), "pr create"); n != 0 {
		t.Fatalf("a pull request was opened for files outside the paths")
	}
	status := r.cmd("", "status", r.id)
	if !strings.Contains(status, "  paths  ") || !strings.Contains(status, "\n    "+mission.ShownName(piece(t, st, "P1").Outside[0])+"\n") {
		t.Fatalf("status does not show the paths wait and its files:\n%s", status)
	}
	r.cmd("", "answer", r.id, piece(t, st, "P1").Wait.ID, "--do", "accept", "--words", "fine")
	r.tickUntil("the pull request", hasPR(t, "P1"))
}

func TestMission_EndToEnd_StepBudget(t *testing.T) {
	r := newPathsRig(t, "2", "2")
	r.launchPieces(touching("P1", "a", []string{"a/**"}), touching("P2", "b", []string{"b/**"}, "P1"))

	r.tickUntil("P1's pull request", hasPR(t, "P1"))
	r.mergePR()
	st := r.tickUntil("P1 complete and the hold", func(s mission.State) bool {
		return piece(t, s, "P1").Status == mission.PieceComplete && s.Hold != nil
	})
	if got := piece(t, st, "P2").Status; got != mission.PieceQueued {
		t.Fatalf("P2 is %s on hold, want queued", got)
	}
	status := r.cmd("", "status", r.id)
	if want := "On hold: 2 of 2 station steps used."; !strings.Contains(status, want) {
		t.Fatalf("status missing %q:\n%s", want, status)
	}
	st = r.tick()
	if got := piece(t, st, "P2").Status; st.Hold == nil || got != mission.PieceQueued {
		t.Fatalf("a tick later P2 is %s and hold is %v, want queued and still on hold", got, st.Hold)
	}
	r.setLimit("steps", "20")
	r.tickUntil("P2 starts", func(s mission.State) bool {
		return s.Hold == nil && piece(t, s, "P2").Status != mission.PieceQueued
	})
}
