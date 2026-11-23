package mission

import (
	"fmt"
	"slices"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

// The engine calls one reducer after the matching effect succeeded. Each
// returns a new State and never modifies its argument.

// update returns a copy of s with fn applied to the piece with this id.
func update(s State, piece string, fn func(*PieceState)) State {
	c := s.Clone()
	for i := range c.Pieces {
		if c.Pieces[i].ID == piece {
			fn(&c.Pieces[i])
		}
	}
	return c
}

// Started records that the piece's run exists and is going.
func Started(s State, piece, runID, branch, worktree string) State {
	return update(s, piece, func(p *PieceState) {
		p.Status = PieceRunning
		p.RunID, p.Branch, p.Worktree = runID, branch, worktree
		p.Behind = nil
	})
}

// DeliverableCollected records a fresh, unaccepted deliverable for the piece.
func DeliverableCollected(s State, piece string, files []string, print string, now time.Time) State {
	return update(s, piece, func(p *PieceState) {
		p.Deliverable = &Deliverable{Files: slices.Clone(files), Print: print, Posted: now}
		// Forget the recorded lane: the next sync moves the issue again, and
		// a Done is read only after that move to In Review.
		p.Jira.Lane, p.Jira.Unconfirmed = "", false
	})
}

// Resumed records one restart of a run that had failed.
func Resumed(s State, piece string) State {
	return update(s, piece, func(p *PieceState) { p.Resumes++; p.Up = 0 })
}

// maxProblemRunes caps the stored reason a piece was sent back.
const maxProblemRunes = 300

// SentBackDone records that the piece was sent back for this reason, and puts
// it back to running with no wait. problem names what was wrong.
func SentBackDone(s State, piece, reason, problem string) State {
	return update(s, piece, func(p *PieceState) {
		if p.SentBack == nil {
			p.SentBack = map[string]int{}
		}
		p.SentBack[reason]++
		p.Status, p.Wait = PieceRunning, nil
		p.GreenSince = time.Time{}
		p.Problem = string([]rune(problem)[:min(len([]rune(problem)), maxProblemRunes)])
	})
}

// PROpened records the piece's pull request and the head crew pushed to it.
// It leaves Problem alone: a piece sent back passes through here again before
// the orchestrator is asked about it.
func PROpened(s State, piece string, pr gh.PR, head string, now time.Time) State {
	return update(s, piece, func(p *PieceState) {
		p.PR, p.PRURL, p.Status = pr.Number, pr.URL, PiecePROpen
		p.Head, p.PRSince = head, now
		p.GreenSince = time.Time{}
	})
}

// BranchUpdated records a new head crew pushed to the piece's pull request.
func BranchUpdated(s State, piece, head string, now time.Time) State {
	return update(s, piece, func(p *PieceState) {
		p.Head, p.PRSince = head, now
		p.GreenSince = time.Time{}
	})
}

// Merged records that the piece's pull request was merged. The piece is
// complete, or delivered when its deliverable is not yet accepted.
func Merged(s State, piece string) State {
	return update(s, piece, func(p *PieceState) {
		p.Status, p.Wait, p.Up = PieceComplete, nil, 0
		if unaccepted(*p) {
			p.Status = PieceDelivered
		}
	})
}

// Asked records one orchestrator call for the piece.
func Asked(s State, piece string) State {
	return update(s, piece, func(p *PieceState) { p.Calls++ })
}

// CleanedUp records that the piece's worktree was removed.
func CleanedUp(s State, piece string) State {
	return update(s, piece, func(p *PieceState) { p.Worktree = "" })
}

// PRWaits makes a pr-open piece's merge wait say question: it opens a merge
// wait when the piece holds no wait, and replaces the question of a merge
// wait it already holds. opened reports whether a new wait was opened. A piece
// holding another kind of wait, or not pr-open, is returned unchanged.
func PRWaits(s State, piece, question string, now time.Time) (next State, opened bool) {
	cur, ok := s.Piece(piece)
	if !ok || cur.Status != PiecePROpen {
		return s, false
	}
	switch {
	case cur.Wait == nil:
		id := fmt.Sprintf("w%d", s.NextWait)
		next = update(s, piece, func(p *PieceState) {
			p.Wait = &Wait{ID: id, Kind: WaitMerge, Question: question, Since: now}
		})
		next.NextWait++
		return next, true
	case cur.Wait.Kind == WaitMerge && cur.Wait.Question != question:
		return update(s, piece, func(p *PieceState) { p.Wait.Question = question }), false
	}
	return s, false
}
