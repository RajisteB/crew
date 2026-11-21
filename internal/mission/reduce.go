package mission

import "github.com/crewfile-dev/crew/internal/gh"

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
	})
}

// Resumed records one restart of a run that had failed.
func Resumed(s State, piece string) State {
	return update(s, piece, func(p *PieceState) { p.Resumes++ })
}

// SentBackDone records that the piece was sent back for this reason.
func SentBackDone(s State, piece, reason string) State {
	return update(s, piece, func(p *PieceState) {
		if p.SentBack == nil {
			p.SentBack = map[string]int{}
		}
		p.SentBack[reason]++
	})
}

// PROpened records the piece's pull request.
func PROpened(s State, piece string, pr gh.PR) State {
	return update(s, piece, func(p *PieceState) {
		p.PR, p.PRURL, p.Status = pr.Number, pr.URL, PiecePROpen
	})
}

// Asked records one orchestrator call for the piece.
func Asked(s State, piece string) State {
	return update(s, piece, func(p *PieceState) { p.Calls++ })
}
