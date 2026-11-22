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

// startedWithDone starts P1, then returns its worktree with the run done.
func startedWithDone(t *testing.T) (*harness, string) {
	t.Helper()
	h := newHarness(t, twoPieces()[0])
	wt := h.tick().Pieces[0].Worktree
	h.runs.setRun("run-P1", doneState(), false)
	return h, wt
}

func (h *harness) notes() []string {
	h.t.Helper()
	evs, _ := h.store.EventsSince(0)
	var out []string
	for _, e := range evs {
		if e.Kind == EvNote {
			out = append(out, e.Msg)
		}
	}
	return out
}

func assertIgnoredLink(t *testing.T, h *harness) {
	t.Helper()
	p := h.tick().Pieces[0]
	if p.Deliverable != nil {
		t.Fatalf("deliverable recorded: %+v", p.Deliverable)
	}
	if _, err := os.Stat(h.store.DeliverablesDir("P1")); err == nil {
		t.Fatal("something was copied into the mission folder")
	}
	if p.Status != PiecePROpen {
		t.Fatalf("the piece should go on to its pull request: %+v", p)
	}
	var found bool
	for _, n := range h.notes() {
		found = found || strings.Contains(n, "the deliverables folder is not a plain folder and was ignored: ")
	}
	if !found {
		t.Fatalf("no note: %v", h.notes())
	}
}

func TestEngine_LinkedDeliverablesRootIsIgnored(t *testing.T) {
	h, wt := startedWithDone(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "s")
	dir := filepath.Join(wt, ".crew", "deliverables")
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, dir); err != nil {
		t.Fatal(err)
	}
	assertIgnoredLink(t, h)
}

func TestEngine_LinkedCrewFolderIsIgnored(t *testing.T) {
	h, wt := startedWithDone(t)
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "deliverables", "secret.txt"), "s")
	if err := os.RemoveAll(filepath.Join(wt, ".crew")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wt, ".crew")); err != nil {
		t.Fatal(err)
	}
	assertIgnoredLink(t, h)
}

func TestEngine_StartRefusesALinkedInputsFolder(t *testing.T) {
	h := newHarness(t)
	st := h.state()
	st.Pieces[0].Status = PieceComplete
	st.Pieces[0].Deliverable = &Deliverable{Files: []string{"brief.md"}, Print: "p"}
	if err := h.store.SaveState(st); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(h.store.DeliverablesDir("P1"), "brief.md"), "the brief")
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "P1", "keep.txt"), "keep")
	wt := filepath.Join(h.e.RepoRoot, ".crew", "worktrees", h.id+"-P2")
	if err := os.MkdirAll(filepath.Join(wt, ".crew"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(wt, ".crew", "inputs")); err != nil {
		t.Fatal(err)
	}
	if p := h.tick().Pieces[1]; p.Status != PieceQueued {
		t.Fatalf("P2 should not start: %+v", p)
	}
	var found bool
	for _, n := range h.notes() {
		found = found || strings.Contains(n, "can't start: prepare the deliverables folders: ")
	}
	if !found {
		t.Fatalf("no note: %v", h.notes())
	}
	if _, err := os.Stat(filepath.Join(outside, "P1", "keep.txt")); err != nil {
		t.Fatalf("file outside the worktree was touched: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "P1", "brief.md")); err == nil {
		t.Fatal("copied through the link")
	}
}

func TestEngine_FailedCollectKeepsTheOldCopy(t *testing.T) {
	h, wt := startedWithDone(t)
	out := filepath.Join(wt, ".crew", "deliverables")
	writeFile(t, filepath.Join(out, "a.md"), "old")
	h.tick()
	old := h.piece("P1").Deliverable
	if old == nil {
		t.Fatal("first collect failed")
	}
	writeFile(t, filepath.Join(out, "a.md"), "new, longer")
	writeFile(t, filepath.Join(out, "b.md"), "unreadable")
	if err := os.Chmod(filepath.Join(out, "b.md"), 0); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.piece("P1").Deliverable; got == nil || !slices.Equal(got.Files, old.Files) || got.Print != old.Print {
		t.Fatalf("state changed: %+v", got)
	}
	dir := h.store.DeliverablesDir("P1")
	if b, err := os.ReadFile(filepath.Join(dir, "a.md")); err != nil || string(b) != "old" {
		t.Fatalf("old copy lost: %q %v", b, err)
	}
	ents, _ := os.ReadDir(filepath.Dir(dir))
	if len(ents) != 1 {
		t.Fatalf("leftover temporary folder: %v", ents)
	}
	var found bool
	for _, n := range h.notes() {
		found = found || strings.Contains(n, "can't collect the deliverable")
	}
	if !found {
		t.Fatalf("no note: %v", h.notes())
	}
}
