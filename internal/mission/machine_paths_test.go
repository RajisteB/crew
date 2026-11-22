package mission

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

func outsideObs(ahead int, outside ...string) Obs {
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	o.Git["P1"] = GitObs{Ahead: ahead, Outside: outside}
	return o
}

func TestOutside_DeletionsAndMovesCount(t *testing.T) {
	got := Outside([]string{"api/**"}, []string{"api/a.go", "web/old.ts", "web/new.ts", "docs/gone.md"})
	if want := []string{"web/old.ts", "web/new.ts", "docs/gone.md"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestDecide_OutsideFilesStopThePiece(t *testing.T) {
	m := mis(nil, "P1")
	ns, as := Decide(m, st(m, PieceRunning), outsideObs(2, "go.mod"), limits(1, 3), 0)
	ask := of[AskOrchestrator](as)
	if len(ask) != 1 || ask[0] != (AskOrchestrator{Piece: "P1", Failure: "out-of-paths"}) || len(of[OpenPR](as)) != 0 {
		t.Fatalf("actions = %+v", as)
	}
	if p := piece(t, ns, "P1"); !slices.Equal(p.Outside, []string{"go.mod"}) || p.OutsideMore != 0 {
		t.Fatalf("piece = %+v", p)
	}
}

func TestDecide_NoOutsideFilesOpensThePR(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Outside, s.Pieces[0].OutsideMore = []string{"old"}, 3
	ns, as := Decide(m, s, outsideObs(2), limits(1, 3), 0)
	if len(of[OpenPR](as)) != 1 || len(of[AskOrchestrator](as)) != 0 {
		t.Fatalf("actions = %+v", as)
	}
	if p := piece(t, ns, "P1"); p.Outside != nil || p.OutsideMore != 0 {
		t.Fatalf("piece = %+v", p)
	}
}

func TestDecide_PieceWithNoPathsIsNotChecked(t *testing.T) {
	m := mis(nil, "P1")
	o := outsideObs(1)
	o.Git["P1"] = GitObs{Ahead: 1, Outside: nil}
	_, as := Decide(m, st(m, PieceRunning), o, limits(1, 3), 0)
	if len(of[OpenPR](as)) != 1 {
		t.Fatalf("actions = %+v", as)
	}
}

func TestDecide_NoCommitsAheadIsNotChecked(t *testing.T) {
	m := mis(nil, "P1")
	_, as := Decide(m, st(m, PieceRunning), outsideObs(0, "go.mod"), limits(1, 3), 0)
	if len(of[AskOrchestrator](as)) != 0 || len(of[OpenPR](as)) != 0 {
		t.Fatalf("actions = %+v", as)
	}
}

func TestDecide_OutsideIsCappedAndCounted(t *testing.T) {
	m := mis(nil, "P1")
	var files []string
	for i := range 250 {
		files = append(files, fmt.Sprintf("f%03d", i))
	}
	ns, _ := Decide(m, st(m, PieceRunning), outsideObs(1, files...), limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if len(p.Outside) != 200 || p.OutsideMore != 50 || p.Outside[199] != "f199" {
		t.Fatalf("outside %d more %d", len(p.Outside), p.OutsideMore)
	}
}

func TestDecide_CallLimitOpensAPathsWait(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Calls = 3
	ns, as := Decide(m, s, outsideObs(1, "a", "b", "c", "d", "e"), limits(1, 3), 0)
	p := piece(t, ns, "P1")
	if len(of[AskOrchestrator](as)) != 0 || p.Wait == nil || p.Wait.Kind != WaitPaths || p.Wait.Back != PieceRunning || p.Status != PieceWaiting {
		t.Fatalf("piece = %+v", p)
	}
	if want := "P1 changed 5 files outside its paths: a, b, c and 2 more"; p.Wait.Question != want {
		t.Fatalf("question = %q, want %q", p.Wait.Question, want)
	}
}

func TestDecide_AcceptedFilesStayAcceptedNewOnesAreAsked(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Allowed = []string{"go.mod"}
	_, as := Decide(m, s, outsideObs(1, "go.mod"), limits(1, 3), 0)
	if len(of[OpenPR](as)) != 1 || len(of[AskOrchestrator](as)) != 0 {
		t.Fatalf("actions = %+v", as)
	}
	ns, as := Decide(m, s, outsideObs(1, "go.mod", "Makefile"), limits(1, 3), 0)
	if len(of[AskOrchestrator](as)) != 1 || !slices.Equal(piece(t, ns, "P1").Outside, []string{"Makefile"}) {
		t.Fatalf("actions = %+v piece = %+v", as, piece(t, ns, "P1"))
	}
}

func TestAllowedFor_OutOfPaths(t *testing.T) {
	got := allowedFor("out-of-paths")
	if !slices.Equal(got, []string{"goto", "cancel", "escalate"}) {
		t.Fatalf("got %v", got)
	}
}

func outsideState(files ...string) State {
	s := exState()
	s.Pieces[0].Outside = files
	return s
}

func TestApplyDecision_EscalateOutOfPathsOpensAPathsWait(t *testing.T) {
	_, s, _, step := ApplyDecision(acme(), outsideState("go.mod", "Makefile"), "P1", "out-of-paths",
		OrchDecision{Action: "escalate", Note: "is go.mod fine?", Reason: "r"}, "orchestrator", exInput(), t0)
	p := s.Pieces[0]
	if step != nil || p.Wait == nil || p.Wait.Kind != WaitPaths || p.Wait.Back != PieceRunning {
		t.Fatalf("piece = %+v", p)
	}
	if !strings.HasPrefix(p.Wait.Question, "P1 changed 2 files outside its paths: go.mod, Makefile") {
		t.Fatalf("question = %q", p.Wait.Question)
	}
}

func TestApplyDecision_GotoCarriesTheFiles(t *testing.T) {
	_, _, _, step := ApplyDecision(acme(), outsideState("go.mod"), "P1", "out-of-paths",
		OrchDecision{Action: "goto", Station: "build", Note: "stay in app/api", Reason: "r"}, "orchestrator", exInput(), t0)
	if step == nil || step.Kind != "goto" {
		t.Fatalf("step = %+v", step)
	}
	if !strings.HasPrefix(step.Note, "These files are outside this piece's paths: go.mod (and 0 more). Undo those changes") || !strings.HasSuffix(step.Note, "stay in app/api") {
		t.Fatalf("note = %q", step.Note)
	}
}

func TestApplyDecision_RetryRefusedOutOfPaths(t *testing.T) {
	_, s, _, step := ApplyDecision(acme(), outsideState("go.mod"), "P1", "out-of-paths",
		OrchDecision{Action: "retry", Reason: "r"}, "orchestrator", exInput(), t0)
	if step != nil || s.Pieces[0].Wait == nil || s.Pieces[0].Wait.Kind != WaitPaths {
		t.Fatalf("piece = %+v", s.Pieces[0])
	}
}

func TestSummaryPrompt_ListsOutsideFiles(t *testing.T) {
	sum := Summary{Piece: Piece{ID: "P1"}}.ForPaths(PieceState{ID: "P1", Outside: []string{"go.mod", "Makefile"}, OutsideMore: 7})
	p := sum.Prompt()
	if !strings.Contains(p, "Files changed outside the piece's paths:") || !strings.Contains(p, "go.mod") ||
		!strings.Contains(p, "and 7 more") || sum.Failure != "out-of-paths" || slices.Contains(sum.Allowed, "retry") {
		t.Fatalf("prompt = %s", p)
	}
}
