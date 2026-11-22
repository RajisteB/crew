package mission

import "slices"

// Lanes: the columns a Jira board shows.
const (
	LaneToDo       = "to-do"
	LaneInProgress = "in-progress"
	LaneInReview   = "in-review"
	LaneDone       = "done"
)

// Lane is the board column a piece belongs in.
func Lane(p PieceState) string {
	switch p.Status {
	case PieceQueued:
		return LaneToDo
	case PieceWaiting:
		if p.Wait != nil && p.Wait.Back == PieceQueued {
			return LaneToDo
		}
		return LaneInProgress
	case PiecePROpen, PieceDelivered:
		return LaneInReview
	case PieceComplete, PieceCancelled:
		return LaneDone
	}
	return LaneInProgress
}

func epicLane(s State) string {
	switch s.Status {
	case StatusRunning, StatusStopped:
		return LaneInProgress
	case StatusDone:
		return LaneDone
	}
	return LaneToDo
}

// TrackerOp is one write Jira still needs. Kind: "create-epic" | "create" |
// "sprint" | "move" | "flag" | "attach" | "move-epic" | "comment".
type TrackerOp struct {
	Kind, Piece string // Piece is "" for the epic
	Lane, File  string
	Text        string
	On          bool
	Seq         int      // comment: the event it comes from
	Pieces      []string // sprint: the pieces to place
}

// TrackerOps returns the writes still owed, in the order to send them. evs are
// the events after s.Jira.Seq. sprints says whether the repo's board uses
// sprints. It performs no I/O.
func TrackerOps(m Mission, s State, evs []Event, sprints bool) []TrackerOp {
	if s.Jira.Epic == "" {
		return []TrackerOp{{Kind: "create-epic"}}
	}
	var ops []TrackerOp
	for _, p := range s.Pieces {
		if p.Jira.Issue == "" {
			ops = append(ops, TrackerOp{Kind: "create", Piece: p.ID})
		}
	}
	ops = append(ops, sprintOp(s, sprints)...)
	for _, p := range s.Pieces {
		if p.Jira.Issue != "" {
			ops = append(ops, pieceOps(p)...)
		}
	}
	if l := epicLane(s); l != s.Jira.EpicLane {
		ops = append(ops, TrackerOp{Kind: "move-epic", Lane: l})
	}
	for _, ev := range evs {
		if piece, text, ok := TrackerComment(ev, s); ok {
			ops = append(ops, TrackerOp{Kind: "comment", Piece: piece, Text: text, Seq: ev.Seq})
		}
	}
	return ops
}

func sprintOp(s State, sprints bool) []TrackerOp {
	if !sprints {
		return nil
	}
	var ids []string
	for _, p := range s.Pieces {
		if p.Jira.Issue != "" && !p.Jira.Sprint {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	return []TrackerOp{{Kind: "sprint", Pieces: ids}}
}

func pieceOps(p PieceState) []TrackerOp {
	var ops []TrackerOp
	if l := Lane(p); l != p.Jira.Lane {
		ops = append(ops, TrackerOp{Kind: "move", Piece: p.ID, Lane: l})
	}
	if on := p.Wait != nil; on != p.Jira.Flagged {
		ops = append(ops, TrackerOp{Kind: "flag", Piece: p.ID, On: on})
	}
	if d := p.Deliverable; d != nil {
		for _, f := range d.Files {
			if !slices.Contains(d.Attached, f) {
				ops = append(ops, TrackerOp{Kind: "attach", Piece: p.ID, File: f})
			}
		}
	}
	return ops
}

// Owed counts the writes in ops that Jira is missing. A sprint op is left out: placing
// issues in a sprint is tried on every pass, and a board with no active sprint is not Jira being behind.
func Owed(ops []TrackerOp) int {
	n := 0
	for _, op := range ops {
		if op.Kind != "sprint" {
			n++
		}
	}
	return n
}
