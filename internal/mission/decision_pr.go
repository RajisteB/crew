package mission

import "slices"

// allowedFor is what the orchestrator may choose for this failure: every
// action, minus "retry" for "conflict" and "checks-failed" (the run is done;
// there is no station to run again).
func allowedFor(failure string) []string {
	if failure == reasonOutOfPaths {
		return []string{"goto", "cancel", "escalate"}
	}
	if failure == reasonConflict || failure == reasonChecksFailed {
		return slices.DeleteFunc(slices.Clone(allowedActions), func(a string) bool { return a == "retry" })
	}
	return slices.Clone(allowedActions)
}

// ForPR adapts a summary to a pull request failure. s is not modified.
func (s Summary) ForPR(failure, problem string) Summary {
	s.Failure, s.Reason, s.Allowed = failure, problem, allowedFor(failure)
	return s
}

// backAfterQuestion is the status a piece returns to once a question is
// answered: none for a piece whose pull request is open, which stays so.
func (a *applier) backAfterQuestion() PieceStatus {
	if a.ps(a.piece).Status == PiecePROpen {
		return ""
	}
	return PieceRunning
}

// resumeFromPR puts a piece whose pull request is open back to running: the
// decision sends it to a station.
func (a *applier) resumeFromPR() {
	if p := a.ps(a.piece); p.Status == PiecePROpen {
		p.Status, p.Wait = PieceRunning, nil
	}
}

// onOpenPR reports whether w is a question or limit wait on a piece whose
// pull request is open.
func onOpenPR(p *PieceState, w Wait) bool {
	return p.Status == PiecePROpen && (w.Kind == WaitQuestion || w.Kind == WaitLimit)
}

// answerOnPR applies a retry or goto answer on such a piece. done is false for
// any other answer, which is handled as usual.
func answerOnPR(d *decider, id string, a Answer) (step *RunStep, done bool) {
	p := d.ps(id)
	switch a.Do {
	case answerRetry:
		if p.SentBack == nil {
			p.SentBack = map[string]int{}
		}
		p.SentBack[reasonConflict], p.SentBack[reasonChecksFailed] = 0, 0
		return nil, true
	case answerGoto:
		p.Status = PieceRunning
		return &RunStep{Piece: id, Kind: "goto", Station: a.Station, Note: a.Note}, true
	}
	return nil, false
}
