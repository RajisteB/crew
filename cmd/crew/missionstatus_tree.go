package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/crewfile-dev/crew/internal/mission"
)

// toBePlanned is what status shows for a delegated piece that nothing keeps
// back: it holds no run slot, so the run-slot text never applies to it.
const toBePlanned = "to be planned"

// readyTextFor is the text for a queued piece nothing in the plan keeps back:
// ready (see readyText), except that a delegated piece is waiting to be
// planned and only a hold changes that.
func readyTextFor(m mission.Mission, id, ready string) string {
	if p, ok := m.Piece(id); ok && p.Delegate && ready != onHoldText {
		return toBePlanned
	}
	return ready
}

// summaryOrder is the order in which a delegated piece's counts are listed.
var summaryOrder = []mission.PieceStatus{
	mission.PieceComplete, mission.PieceDelivered, mission.PieceRunning, mission.PiecePROpen, mission.PieceWaiting,
	mission.PieceDelegated, mission.PieceQueued, mission.PieceCancelled,
}

// printDelegatedPieces lists each delegated piece with the status of the
// pieces directly under it.
func printDelegatedPieces(out io.Writer, st mission.State) {
	var lines []string
	for _, p := range st.Pieces {
		if p.Status == mission.PieceDelegated {
			lines = append(lines, p.ID+"  "+childCounts(st, p.ID))
		}
	}
	section(out, "Delegated", lines)
}

// childCounts reads like "3 pieces: 1 complete, 1 running, 1 queued". It
// counts the pieces directly under id in the state, as the engine does.
func childCounts(st mission.State, id string) string {
	counts := map[mission.PieceStatus]int{}
	var kids []mission.PieceState
	for _, c := range st.Pieces {
		if mission.ParentID(c.ID) == id {
			kids = append(kids, c)
			counts[c.Status]++
		}
	}
	var parts []string
	for _, s := range summaryOrder {
		if n := counts[s]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s))
		}
	}
	noun := "pieces"
	if len(kids) == 1 {
		noun = "piece"
	}
	return fmt.Sprintf("%d %s: %s", len(kids), noun, strings.Join(parts, ", "))
}
