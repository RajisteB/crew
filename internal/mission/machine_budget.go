package mission

import (
	"strconv"
	"time"
)

const (
	holdSteps      = "steps"
	msgStepsBudget = "station steps for this mission"
	msgCallsBudget = "orchestrator calls for this piece"
)

// Hold says the mission takes on no new work because a budget ran out.
type Hold struct {
	Kind  string    `json:"kind"` // "steps"
	Count int       `json:"count"`
	Max   string    `json:"max"`
	Since time.Time `json:"since"`
}

// StepsUsed is the mission's station steps so far, summed over its pieces.
func StepsUsed(s State) int {
	n := 0
	for _, p := range s.Pieces {
		n += p.Steps
	}
	return n
}

// countSteps lifts each piece's Steps to what its run reports; a count never goes down.
func (d *decider) countSteps() {
	for i := range d.s.Pieces {
		p := &d.s.Pieces[i]
		if ro, ok := d.o.Runs[p.ID]; ok {
			p.Steps = max(p.Steps, ro.State.Step)
		}
	}
}

// overBudget says the mission has used its station steps. A limit that was
// never set is no limit.
func (d *decider) overBudget() bool {
	return d.lim.Steps.Set && !Allows(d.lim.Steps, StepsUsed(d.s)+1)
}

// applyBudget puts the mission on hold, or lifts the hold, and while it is on
// hold drops the actions that take on new work or send a piece back. Nothing Decide chose that
// would be dropped changed the state, so the state stays as it was.
// A mission that is done is on hold no more.
func (d *decider) applyBudget() {
	if d.s.Status == StatusDone {
		d.s.Hold = nil
		return
	}
	over := d.overBudget()
	switch {
	case over && d.s.Hold == nil:
		d.startHold()
	case over:
		d.keepHold()
	case d.s.Hold != nil:
		d.s.Hold = nil
		d.record(EvNote, "", "step budget raised to "+d.lim.Steps.String()+": work goes on")
	}
	if d.s.Hold == nil {
		return
	}
	kept := d.actions[:0:0]
	for _, a := range d.actions {
		switch a.(type) {
		case StartPiece, SendBack, AskOrchestrator, DriveRun, ResolveConflict, PlanPieces:
		default:
			kept = append(kept, a)
		}
	}
	d.actions = kept
}

func (d *decider) startHold() {
	n, limit := StepsUsed(d.s), d.lim.Steps.String()
	d.s.Hold = &Hold{Kind: holdSteps, Count: n, Max: limit, Since: d.o.Now}
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvBudgetHit, By: "engine", Msg: msgStepsBudget, Count: n, Max: limit}})
	d.notify(stepBudgetText(n, limit))
}

// keepHold keeps the hold's numbers current while the mission stays over
// budget: Count follows the steps used (runs already going carry on), and a
// limit raised too little is noted once.
func (d *decider) keepHold() {
	h := *d.s.Hold
	h.Count = StepsUsed(d.s)
	if limit := d.lim.Steps.String(); limit != h.Max {
		h.Max = limit
		d.record(EvNote, "", "step budget raised to "+limit+", "+strconv.Itoa(h.Count)+" used: still on hold")
	}
	d.s.Hold = &h
}

func stepBudgetText(n int, limit string) string {
	return "step budget reached (" + strconv.Itoa(n) + " of " + limit + "): no new piece starts and nothing is sent back until limits.steps is raised above " + strconv.Itoa(n) + " in missions.yaml"
}

// callBudgetHit records that a piece used up its orchestrator calls.
func (d *decider) callBudgetHit(id string, calls int) {
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvBudgetHit, Piece: id, By: "engine",
		Msg: msgCallsBudget, Count: calls, Max: d.lim.OrchestratorCalls.String()}})
}
