package mission

import (
	"context"
	"errors"
)

func (o *fakeOrch) Plan(_ context.Context, _ string, r PlanRequest) (SubPlan, error) {
	o.log.add("Orch.Plan %s", r.Piece.ID)
	o.planReqs = append(o.planReqs, r)
	if o.planErr != nil {
		return SubPlan{}, o.planErr
	}
	if len(o.plans) == 0 {
		return SubPlan{}, errors.New("no plan queued")
	}
	sp := o.plans[0]
	o.plans = o.plans[1:]
	return sp, nil
}

// Plan cancels the tick's context mid-call, the way a stop signal does.
func (o cancelOrch) Plan(context.Context, string, PlanRequest) (SubPlan, error) {
	o.cancel()
	return SubPlan{}, errors.New("run interrupted")
}
