package mission

import "slices"

// Reducers for what Jira shows. Each returns a new State.

// EpicCreated records the epic's key. Jira creates an issue in to-do.
func EpicCreated(s State, key string) State {
	c := s.Clone()
	c.Jira.Epic, c.Jira.EpicLane = key, LaneToDo
	return c
}

// IssueCreated records a piece's issue key; Jira creates it in to-do.
func IssueCreated(s State, piece, key string) State {
	return update(s, piece, func(p *PieceState) { p.Jira.Issue, p.Jira.Lane = key, LaneToDo })
}

// LaneSet records the lane Jira now shows, confirmed. piece "" is the epic.
func LaneSet(s State, piece, lane string) State {
	if piece == "" {
		c := s.Clone()
		c.Jira.EpicLane = lane
		return c
	}
	return update(s, piece, func(p *PieceState) { p.Jira.Lane, p.Jira.Unconfirmed = lane, false })
}

// LaneGivenUp records the lane of a move Jira refused for good, marked
// unconfirmed: crew stops asking, but won't trust the board's Done for it.
// piece "" is the epic, which needs no mark.
func LaneGivenUp(s State, piece, lane string) State {
	if piece == "" {
		return LaneSet(s, "", lane)
	}
	return update(s, piece, func(p *PieceState) { p.Jira.Lane, p.Jira.Unconfirmed = lane, true })
}

// FlagSet records whether Jira shows the piece's issue flagged.
func FlagSet(s State, piece string, on bool) State {
	return update(s, piece, func(p *PieceState) { p.Jira.Flagged = on })
}

// SprintSet records that these pieces' issues are in the sprint.
func SprintSet(s State, pieces []string) State {
	c := s.Clone()
	for i := range c.Pieces {
		if slices.Contains(pieces, c.Pieces[i].ID) {
			c.Pieces[i].Jira.Sprint = true
		}
	}
	return c
}

// FileAttached records that the file is on the piece's issue.
func FileAttached(s State, piece, file string) State {
	return update(s, piece, func(p *PieceState) {
		if p.Deliverable != nil && !slices.Contains(p.Deliverable.Attached, file) {
			p.Deliverable.Attached = append(p.Deliverable.Attached, file)
		}
	})
}

// CommentedThrough records that comments are sent up to this event. It never
// moves the cursor back.
func CommentedThrough(s State, seq int) State {
	c := s.Clone()
	c.Jira.Seq = max(c.Jira.Seq, seq)
	return c
}
