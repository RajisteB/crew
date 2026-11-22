package mission

import (
	"reflect"
	"testing"
)

func TestCreated_StartsInToDo(t *testing.T) {
	s := State{Status: StatusPlanned, Pieces: []PieceState{{ID: "P1", Status: PieceQueued}}}
	s = IssueCreated(EpicCreated(s, "ACME-1"), "P1", "ACME-2")
	if s.Jira.Epic != "ACME-1" || s.Pieces[0].Jira.Issue != "ACME-2" {
		t.Fatalf("keys not recorded: %+v", s)
	}
	if ops := TrackerOps(Mission{}, s, nil, false); len(ops) != 0 {
		t.Fatalf("got %v", ops)
	}
}

func TestReducers_NeverChangeTheirInput(t *testing.T) {
	base := State{Pieces: []PieceState{{ID: "P1", Deliverable: &Deliverable{Files: []string{"a"}}}}}
	base.Jira.Seq = 5
	steps := map[string]func(State) State{
		"EpicCreated":    func(s State) State { return EpicCreated(s, "K") },
		"IssueCreated":   func(s State) State { return IssueCreated(s, "P1", "K") },
		"LaneSet":        func(s State) State { return LaneSet(s, "P1", LaneDone) },
		"LaneSetEpic":    func(s State) State { return LaneSet(s, "", LaneDone) },
		"FlagSet":        func(s State) State { return FlagSet(s, "P1", true) },
		"SprintSet":      func(s State) State { return SprintSet(s, []string{"P1"}) },
		"FileAttached":   func(s State) State { return FileAttached(s, "P1", "a") },
		"CommentedThrou": func(s State) State { return CommentedThrough(s, 9) },
	}
	for name, f := range steps {
		before := base.Clone()
		f(base)
		if !reflect.DeepEqual(base, before) {
			t.Errorf("%s changed its input", name)
		}
	}
	if got := CommentedThrough(base, 3).Jira.Seq; got != 5 {
		t.Errorf("cursor moved back to %d", got)
	}
	if got := CommentedThrough(base, 9).Jira.Seq; got != 9 {
		t.Errorf("cursor = %d", got)
	}
	got := FileAttached(FileAttached(base, "P1", "a"), "P1", "b")
	if !reflect.DeepEqual(got.Pieces[0].Deliverable.Attached, []string{"a", "b"}) {
		t.Errorf("attached %v", got.Pieces[0].Deliverable.Attached)
	}
}

func TestClone_CopiesDeliverable(t *testing.T) {
	s := State{Pieces: []PieceState{{ID: "P1", Deliverable: &Deliverable{Files: make([]string, 1, 4), Attached: make([]string, 1, 4)}}}}
	c := s.Clone()
	c.Pieces[0].Deliverable.Files = append(c.Pieces[0].Deliverable.Files[:1], "x")
	c.Pieces[0].Deliverable.Attached[0] = "y"
	d := s.Pieces[0].Deliverable
	if len(d.Files) != 1 || d.Files[0] != "" || d.Attached[0] != "" {
		t.Errorf("original changed: %+v", d)
	}
}
