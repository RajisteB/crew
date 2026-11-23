package mission

import "fmt"

// planPieces asks the scoped orchestrator to split a delegated piece. Every
// call that completes counts toward the piece's orchestrator calls, whether
// its reply was usable or not; the reply is applied or refused by the state
// machine. The factories the request lists come from the engine's check input.
func (t *tick) planPieces(a PlanPieces) error {
	p, ps, ok := t.pieceMission(a.Piece)
	if !ok || !p.Delegate || ps.Status != PieceQueued {
		return nil
	}
	req := BuildPlanRequest(t.s.MissionID, t.m, ps, t.e.Domains[p.Domain], t.in.Factories, t.lim)
	sp, err := t.callPlanner(req)
	if t.ctx.Err() != nil {
		return nil // stopped during the call: nothing is counted; the next engine plans again
	}
	asked := Asked(t.s, a.Piece)
	m2, s2, acts := t.m, asked, []Action(nil)
	if err != nil {
		s2, acts = PlanRefused(asked, a.Piece, err.Error(), t.now)
	} else {
		m2, s2, acts = ApplySubPlan(t.m, asked, a.Piece, sp, t.in, t.now)
	}
	t.adopt(m2, s2)
	if err := t.save(); err != nil {
		return err
	}
	return t.perform(acts)
}

func (t *tick) callPlanner(req PlanRequest) (SubPlan, error) {
	if t.e.Orch == nil {
		return SubPlan{}, fmt.Errorf("no orchestrator is configured")
	}
	return t.e.Orch.Plan(t.ctx, t.e.RepoRoot, req)
}
