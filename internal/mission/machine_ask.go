package mission

import "fmt"

// askPassing is ask for a stop whose question an orchestrator is passing up
// (passed is that sentence, e.g. "scoped orchestrator P3 asked: which
// colour?", or ""). When the calls are used up, the wait on the principal
// ends with it, so the question is not lost.
func (d *decider) askPassing(id, failure, passed string) {
	p := d.ps(id)
	if Allows(d.lim.OrchestratorCalls, p.Calls+1) {
		d.add(AskOrchestrator{Piece: id, Failure: failure})
		return
	}
	d.callBudgetHit(id, p.Calls)
	if failure == reasonOutOfPaths {
		d.wait(id, WaitPaths, withPassed(pathsQuestion(*p), passed), PieceRunning)
		return
	}
	q := fmt.Sprintf("%s hit the orchestrator call limit after a %s failure", id, failure)
	back := PieceRunning
	if p.Status == PiecePROpen {
		back = "" // the piece stays pr-open and its pull request is still watched
	}
	d.wait(id, WaitLimit, withPassed(q, passed), back)
}

func withPassed(q, passed string) string {
	if passed == "" {
		return q
	}
	return q + ". " + passed
}
