package main

import (
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/mission"
)

// printHold prints the line that says the mission is on hold for its step
// budget. A mission that is not on hold, or is done, prints nothing.
func printHold(out io.Writer, st mission.State) {
	if st.Hold == nil || st.Status == mission.StatusDone {
		return
	}
	fmt.Fprintf(out, "On hold: %d of %s station steps used. Raise limits.steps above %d in missions.yaml; the engine reads it again within a minute.\n", st.Hold.Count, st.Hold.Max, st.Hold.Count)
}

// pathsFileLines are the files of a paths wait, one per line. `accept` takes
// all of them while the question names only three, so the principal sees every
// file. Names come from an agent's worktree and are cleaned before they are shown.
func pathsFileLines(p mission.PieceState) []string {
	if p.Wait == nil || p.Wait.Kind != mission.WaitPaths {
		return nil
	}
	lines := make([]string, 0, len(p.Outside)+1)
	for _, f := range p.Outside {
		lines = append(lines, "  "+mission.ShownName(f))
	}
	if p.OutsideMore > 0 {
		lines = append(lines, fmt.Sprintf("  and %d more (too many to accept: send the piece back or cancel it)", p.OutsideMore))
	}
	return lines
}
