package main

import (
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionStatus_OnHoldAndPathsWait(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Hold = &mission.Hold{Kind: "steps", Count: 60, Max: "60", Since: time.Now()}
		s.Pieces[0].Status = mission.PieceWaiting
		s.Pieces[0].Outside = []string{"a.txt", "b\x1b[31m.txt", "c.txt", "d.txt"}
		s.Pieces[0].OutsideMore = 5
		s.Pieces[0].Wait = &mission.Wait{ID: "w1", Kind: mission.WaitPaths, Question: "P1 changed 9 files outside its paths: a.txt, b, c.txt and 6 more", Since: time.Now()}
	})
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if want := "On hold: 60 of 60 station steps used. Raise limits.steps in missions.yaml; the engine reads it again within a minute."; len(lines) < 2 || lines[1] != want {
		t.Errorf("second line of status is not %q:\n%s", want, out)
	}
	for _, want := range []string{
		"  w1  P1  paths  P1 changed 9 files outside its paths: a.txt, b, c.txt and 6 more  waiting ",
		"\n    a.txt\n    b?[31m.txt\n    c.txt\n    d.txt\n    and 5 more (too many to accept: send the piece back or cancel it)\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("status missing %q:\n%s", want, out)
		}
	}
}

func TestMissionStatus_NoHoldLineWithoutHold(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	out, _ := r.cmd("status", id)
	if strings.Contains(out, "On hold") {
		t.Errorf("unexpected hold line:\n%s", out)
	}
}
