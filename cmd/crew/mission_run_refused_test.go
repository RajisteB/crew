package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

// A run a real mission started is refused by crew run say: the wiring of
// cmd/crew/missionrun.go stamps it, and the guard reads the stamp.
func TestMission_EndToEnd_ARunTheMissionStartedIsRefusedByCrewRun(t *testing.T) {
	r := newE2ERig(t)
	r.confirmRepo()
	r.launchPieces(e2ePiece("P1"))
	st := r.tickUntil("P1 started", func(s mission.State) bool { return piece(t, s, "P1").RunID != "" })
	runID := piece(t, st, "P1").RunID
	var out, errb bytes.Buffer
	code := runCmd([]string{"say", runID, "hi", "--project", r.repo, "--dir", r.crewDir}, strings.NewReader(""), &out, &errb)
	want := "run " + runID + " belongs to " + mission.RunOwner(r.id, "P1")
	if code != 1 || !strings.Contains(errb.String(), want) {
		t.Fatalf("exit %d, want 1 and %q:\n%s%s", code, want, out.String(), errb.String())
	}
}
