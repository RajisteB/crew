package mission

import (
	"slices"
	"testing"
)

func pathsHarness(t *testing.T, changed ...string) (*harness, string) {
	t.Helper()
	p := twoPieces()[0]
	p.Paths = []string{"api/**"}
	h := newHarness(t, p)
	runID := h.runPiece("P1")
	h.runs.setRun(runID, doneState(), false)
	h.runs.summary = doneState()
	h.git.changed = changed
	return h, runID
}

func TestEngine_OutsidePathsEndToEnd(t *testing.T) {
	h, _ := pathsHarness(t, "api/a.go", "go.mod")
	h.orch.decisions = []OrchDecision{{Action: "escalate", Note: "go.mod too?", Reason: "dependency"}}
	h.tick()
	p := h.piece("P1")
	if h.log.has("CreatePR") || p.Wait == nil || p.Wait.Kind != WaitPaths || !slices.Equal(p.Outside, []string{"go.mod"}) {
		t.Fatalf("piece = %+v calls %v", p, h.log.calls)
	}
	if err := h.store.Drop("answer", Answer{Wait: p.Wait.ID, Do: "accept", Words: "go.mod is fine"}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if !h.log.has("CreatePR") {
		t.Fatalf("calls %v piece %+v", h.log.calls, h.piece("P1"))
	}
	if got := h.piece("P1").Allowed; !slices.Equal(got, []string{"go.mod"}) {
		t.Fatalf("allowed = %v", got)
	}
	if len(h.orch.sums) != 1 || h.orch.sums[0].Failure != "out-of-paths" || !slices.Equal(h.orch.sums[0].Outside, []string{"go.mod"}) {
		t.Fatalf("summaries = %+v", h.orch.sums)
	}
}

func TestEngine_ChangedFilesFailureDecidesNothing(t *testing.T) {
	h, _ := pathsHarness(t, "go.mod")
	h.fail.failNext("ChangedFiles", 1)
	h.tick()
	if h.log.has("CreatePR") || h.log.has("Orch.Decide") || len(h.events(EvNote)) != 1 {
		t.Fatalf("calls %v notes %+v", h.log.calls, h.events(EvNote))
	}
}

func TestEngine_PieceWithoutPathsIsNotListed(t *testing.T) {
	h := newHarness(t, twoPieces()[0])
	h.runs.setRun(h.runPiece("P1"), doneState(), false)
	h.git.changed = []string{"go.mod"}
	h.tick()
	if h.log.has("ChangedFiles") || !h.log.has("CreatePR") {
		t.Fatalf("calls %v", h.log.calls)
	}
}
