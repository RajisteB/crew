package mission

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestEngine_CollectCopiesAndRecords(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	st := h.tick()
	wt := st.Pieces[0].Worktree
	writeFile(t, filepath.Join(wt, ".crew", "deliverables", "brief.md"), "the brief")
	writeFile(t, filepath.Join(wt, ".crew", "deliverables", "sub", "plan.md"), "the plan")
	h.runs.setRun("run-P1", doneState(), false)

	got := h.tick().Pieces[0]
	dir := h.store.DeliverablesDir("P1")
	if b, err := os.ReadFile(filepath.Join(dir, "sub", "plan.md")); err != nil || string(b) != "the plan" {
		t.Fatalf("copy: %q %v", b, err)
	}
	if got.Deliverable == nil || !slices.Equal(got.Deliverable.Files, []string{"brief.md", "sub/plan.md"}) || got.Deliverable.Accepted {
		t.Fatalf("deliverable = %+v", got.Deliverable)
	}
	if got.Status != PieceRunning {
		t.Fatalf("collecting must not open the PR this tick: %+v", got)
	}
	evs, _ := h.store.EventsSince(0)
	var found bool
	for _, e := range evs {
		if e.Kind == EvDeliverablePosted && e.Piece == "P1" && e.Msg == "brief.md, sub/plan.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no deliverable-posted event: %+v", evs)
	}
	if p := h.tick().Pieces[0]; p.Status != PiecePROpen {
		t.Fatalf("second tick should open the PR: %+v", p)
	}
}

func TestEngine_StartCopiesEarlierDeliverables(t *testing.T) {
	h := newHarness(t)
	st := h.state()
	st.Pieces[0].Status = PieceComplete
	st.Pieces[0].Deliverable = &Deliverable{Files: []string{"brief.md"}, Print: "p"}
	if err := h.store.SaveState(st); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.store.DeliverablesDir("P1"), "brief.md"), "the brief")

	p2 := h.tick().Pieces[1]
	if p2.Status != PieceRunning {
		t.Fatalf("P2 = %+v", p2)
	}
	crew := filepath.Join(p2.Worktree, run.CrewDir)
	if b, err := os.ReadFile(filepath.Join(crew, "inputs", "P1", "brief.md")); err != nil || string(b) != "the brief" {
		t.Fatalf("input copy: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(crew, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	if ents, err := os.ReadDir(filepath.Join(crew, "deliverables")); err != nil || len(ents) != 0 {
		t.Fatalf("deliverables folder: %v %v", ents, err)
	}
	if len(h.runs.inputs) != 1 || !strings.Contains(h.runs.inputs[0], ".crew/inputs/P1/brief.md") {
		t.Fatalf("input = %v", h.runs.inputs)
	}
}
