package mission

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

func pathsAnswerState(more int, files ...string) State {
	s := exState()
	p := &s.Pieces[0]
	p.Status, p.Outside, p.OutsideMore = PieceWaiting, files, more
	p.Wait = &Wait{ID: "w1", Kind: WaitPaths, Question: "q", Since: t0, Back: PieceRunning}
	return s
}

func TestAnswer_AcceptOutsideFiles(t *testing.T) {
	_, s, as, step, err := answer(pathsAnswerState(0, "go.mod", "Makefile"), Answer{Do: "accept", Words: "fine"})
	if err != nil || step != nil {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	p := s.Pieces[0]
	if !slices.Equal(p.Allowed, []string{"go.mod", "Makefile"}) || p.Outside != nil || p.Wait != nil || p.Status != PieceRunning {
		t.Fatalf("piece = %+v", p)
	}
	var found bool
	for _, a := range as {
		if r, ok := a.(Record); ok && r.Event.Kind == EvDecision && r.Event.By == "principal" && r.Event.Msg == "accepted 2 files outside the paths" {
			found = true
		}
	}
	if !found {
		t.Fatalf("actions = %+v", as)
	}
}

func TestAnswer_AcceptRefusedWhenThereAreMore(t *testing.T) {
	in := pathsAnswerState(5, "go.mod")
	_, s, _, _, err := answer(in, Answer{Do: "accept", Words: "fine"})
	if err == nil || !strings.Contains(err.Error(), "goto, cancel") {
		t.Fatalf("err = %v", err)
	}
	if !reflect.DeepEqual(s, in) {
		t.Fatal("state changed on error")
	}
}

func TestAnswer_GotoOnAPathsWait(t *testing.T) {
	_, s, _, step, err := answer(pathsAnswerState(0, "go.mod"), Answer{Do: "goto", Station: "build", Note: "keep to the api", Words: "redo"})
	if err != nil || step == nil || step.Kind != "goto" || step.Station != "build" {
		t.Fatalf("step = %+v, err = %v", step, err)
	}
	if !strings.HasPrefix(step.Note, "These files are outside this piece's paths: go.mod") || !strings.HasSuffix(step.Note, "keep to the api") {
		t.Fatalf("note = %q", step.Note)
	}
	if p := s.Pieces[0]; p.Status != PieceRunning || p.Wait != nil {
		t.Fatalf("piece = %+v", p)
	}
}

func TestAnswer_CancelOnAPathsWait(t *testing.T) {
	_, s, _, _, err := answer(pathsAnswerState(0, "go.mod"), Answer{Do: "cancel", Words: "drop"})
	if err != nil || s.Pieces[0].Status != PieceCancelled {
		t.Fatalf("status = %s, err = %v", s.Pieces[0].Status, err)
	}
}

func TestAnswer_AcceptStillRefusedOnOtherWaits(t *testing.T) {
	_, _, _, _, err := answer(waiting(WaitQuestion, PieceRunning), Answer{Do: "accept", Words: "x"})
	if err == nil {
		t.Fatal("accept on a question wait was allowed")
	}
}
