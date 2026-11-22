package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionPlan_ShowsPathsAndNotes(t *testing.T) {
	r := newMissionRig(t)
	web := "---\nname: web\ndescription: Web work.\nfactories: [ship-change]\npaths: [\"web/**\"]\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(r.crewDir, "domains", "web.md"), []byte(web), 0o644); err != nil {
		t.Fatal(err)
	}
	piece := func(id string, paths ...string) mission.Piece {
		return mission.Piece{ID: id, Domain: "web", Factory: "ship-change", Brief: "Do " + id + ".", Paths: paths}
	}
	r.stdin = string(planFile("Build.",
		piece("P1", "web/ui/**"),
		piece("P2", "web/**", "deploy/**"),
		designPiece("P3")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatalf("plan: %v\n%s", err, out)
	}
	for _, want := range []string{
		"PATHS",
		"web/ui/**",
		"web/**, deploy/**",
		"P2 may change files outside the web domain: deploy/**",
		"P3 has no paths, so crew can't check it against other pieces or check what it changes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "P1 may change") || strings.Contains(out, "P1 has no paths") {
		t.Errorf("P1 should have no note:\n%s", out)
	}
}
