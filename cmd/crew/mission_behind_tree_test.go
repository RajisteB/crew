package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/source"
)

// A delegated piece is planned whatever other missions hold, so plan and
// start print no "starts after" line for it; its sub-pieces are kept behind.
func TestPrintOthersOverlap_SkipsADelegatedPiece(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build.", pathPiece("P1", "api/**")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatalf("first plan: %v\n%s", err, out)
	}
	first := missionIDRe.FindString(out)
	r.edit(first, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceRunning
	})
	unlock, err := r.store(first).Lock() // a live engine drives it
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	m := mission.Mission{Request: "Build more.", Pieces: []mission.Piece{
		{ID: "P1", Domain: "design", Delegate: true, Brief: "b", Paths: []string{"api/**"}},
		pathPiece("P2", "api/v2/**"),
	}}
	st := mission.NewState("20261122-zz99", m, gh.RepoInfo{Slug: "acme/app", DefaultBranch: "main"})
	var buf bytes.Buffer
	printOthersOverlap(&buf, r.root(), st, m, []source.Domain{{Name: "design"}})
	if got := buf.String(); strings.Contains(got, "P1 starts after") || !strings.Contains(got, "P2 starts after P1 of mission "+first) {
		t.Fatalf("printed:\n%s", got)
	}
}
