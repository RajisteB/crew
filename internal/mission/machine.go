package mission

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
)

const (
	maxDirtyPaths  = 20
	maxResumes     = 2
	maxSummaryLen  = 200
	reasonUncommit = "uncommitted"
	// reasons a piece is sent back from its pull request
	reasonConflict     = "conflict"
	reasonChecksFailed = "checks-failed"
)

// RunObs is what the engine saw of a piece's run.
type RunObs struct {
	State run.State
	Live  bool // a process is driving the run
}

// GitObs is what the engine saw in a done piece's worktree.
type GitObs struct {
	Ahead int      // commits ahead of the base branch
	Dirty []string // uncommitted paths
}

// Obs is everything Decide may look at besides the mission and its state.
type Obs struct {
	Now  time.Time
	Runs map[string]RunObs // piece id -> its run; absent when the piece has none
	Git  map[string]GitObs // present for pieces whose run is done
	// Delivered is what a done run left in .crew/deliverables; present for
	// pieces whose run is done.
	Delivered map[string]Found
	PRs       map[string]gh.PR   // present for pr-open pieces
	Auto      map[string]AutoObs // per pr-open piece; a piece that is absent follows AutoOff
	// Accept has one entry per piece with an unaccepted Deliverable.
	Accept map[string]AcceptObs
	// Elsewhere is the number of runs the repo's other missions hold.
	// ElsewhereUnknown means they could not be counted: nothing is started.
	Elsewhere        int
	ElsewhereUnknown bool
	// Behind has an entry for each queued piece of this mission whose paths
	// overlap a piece another mission in the repo holds.
	Behind map[string]Blocker
}

// Action is something for the engine to carry out.
type Action interface{ isAction() }

type StartPiece struct{ Piece string }
type DriveRun struct{ Piece string } // the run should be going and nothing drives it
// SendBack sends a piece back to a station, then drives it. An empty Station
// means the factory's rework station: the engine fills it in.
type SendBack struct{ Piece, Station, Reason, Note string }
type OpenPR struct{ Piece string }

// CollectDeliverable copies what the piece's station left in its deliverables
// folder into the mission folder and records it.
type CollectDeliverable struct{ Piece string }
type AskOrchestrator struct{ Piece, Failure string } // Failure: "limit-hit" | "failed" | "uncommitted" | "conflict" | "checks-failed"
type Notify struct{ Title, Body string }
type Cleanup struct {
	Piece string
	Force bool
}
type Record struct{ Event Event }

// Merge merges the piece's pull request at this head. Checked is true when
// checks passed, false for without-checks.
type Merge struct {
	Piece, Head string
	Checked     bool
}

// ResolveConflict merges the base into the piece's branch, or sends the piece back.
type ResolveConflict struct{ Piece string }

func (StartPiece) isAction()         {}
func (DriveRun) isAction()           {}
func (SendBack) isAction()           {}
func (OpenPR) isAction()             {}
func (CollectDeliverable) isAction() {}
func (AskOrchestrator) isAction()    {}
func (Notify) isAction()             {}
func (Cleanup) isAction()            {}
func (Record) isAction()             {}
func (Merge) isAction()              {}
func (ResolveConflict) isAction()    {}

// decider is the working copy of one Decide call. It only ever holds a clone
// of the caller's state.
type decider struct {
	m       Mission
	s       State
	o       Obs
	lim     Limits
	actions []Action
}

// Decide looks at the mission, its state and what the engine observed, and
// returns the new state plus the actions to carry out. It performs no I/O and
// never records an effect as done: the engine runs each action and calls the
// matching reducer. s is never modified.
func Decide(m Mission, s State, o Obs, lim Limits, remind time.Duration) (State, []Action) {
	d := &decider{m: m, s: s.Clone(), o: o, lim: lim}
	if d.s.Status != StatusRunning {
		return d.s, nil
	}
	for _, id := range d.idsWith(PieceRunning, PiecePROpen) {
		switch d.status(id) {
		case PieceRunning:
			d.running(id)
		case PiecePROpen:
			d.prOpen(id)
		}
	}
	d.deliverables()
	for _, id := range d.idsWith(PieceQueued) {
		d.queued(id)
	}
	d.remind(remind)
	d.finish()
	return d.s, d.actions
}

func (d *decider) idsWith(statuses ...PieceStatus) []string {
	var ids []string
	for _, p := range d.s.Pieces {
		for _, st := range statuses {
			if p.Status == st {
				ids = append(ids, p.ID)
			}
		}
	}
	return ids
}

func (d *decider) status(id string) PieceStatus {
	p, _ := d.s.Piece(id)
	return p.Status
}

// ps is the working copy's piece: changes through it stick.
func (d *decider) ps(id string) *PieceState {
	for i := range d.s.Pieces {
		if d.s.Pieces[i].ID == id {
			return &d.s.Pieces[i]
		}
	}
	return nil
}

func (d *decider) add(a ...Action) { d.actions = append(d.actions, a...) }

func (d *decider) record(kind, piece, msg string) {
	d.add(Record{Event: Event{Time: d.o.Now, Kind: kind, Piece: piece, By: "engine", Msg: msg}})
}

func (d *decider) notify(body string) {
	d.add(Notify{Title: "crew mission " + d.s.MissionID, Body: body})
}

// openWait gives the piece a new wait, replacing any it holds. A back status
// moves the piece to waiting; without one the piece keeps its status. It
// records and notifies nothing.
func (d *decider) openWait(id, kind, question string, back PieceStatus) {
	p := d.ps(id)
	p.Wait = &Wait{ID: fmt.Sprintf("w%d", d.s.NextWait), Kind: kind, Question: question, Since: d.o.Now, Back: back}
	d.s.NextWait++
	if back != "" {
		p.Status = PieceWaiting
	}
}

// wait opens a wait, records that the mission is waiting on the principal and
// notifies them.
func (d *decider) wait(id, kind, question string, back PieceStatus) {
	d.openWait(id, kind, question, back)
	d.record(EvWaitingOnPrincipal, id, kind+": "+question)
	d.notify(waitText(*d.ps(id)))
}

// waitText is what a notification about the piece's wait says.
func waitText(p PieceState) string {
	if p.Wait.Kind == WaitMerge {
		return p.Wait.Question
	}
	return fmt.Sprintf("%s: %s", p.ID, p.Wait.Question)
}

func (d *decider) complete(id string) {
	p := d.ps(id)
	p.Status, p.Wait = PieceComplete, nil
	d.record(EvPieceComplete, id, "")
}

func (d *decider) running(id string) {
	ro, ok := d.o.Runs[id]
	if !ok {
		d.add(DriveRun{Piece: id})
		return
	}
	switch ro.State.Status {
	case run.StatusRunning, run.StatusInterrupted:
		if !ro.Live {
			d.add(DriveRun{Piece: id})
		}
	case run.StatusAwaitingApproval:
		d.wait(id, WaitApproval, approvalQuestion(ro.State), PieceRunning)
	case run.StatusFailed:
		d.failed(id, ro.State)
	case run.StatusDone:
		d.done(id)
	}
}

func approvalQuestion(rs run.State) string {
	q := "approval needed at " + rs.Current
	if rs.Pending != nil && rs.Pending.Verdict.Summary != "" {
		q = rs.Pending.Verdict.Summary
	}
	if len(q) > maxSummaryLen {
		q = q[:maxSummaryLen]
	}
	return q
}

func (d *decider) failed(id string, rs run.State) {
	switch {
	case rs.Retryable && d.ps(id).Resumes < maxResumes:
		d.add(DriveRun{Piece: id})
	case rs.Retryable:
		d.ask(id, "failed")
	case rs.LimitRoute != "":
		d.ask(id, "limit-hit")
	default:
		d.ask(id, "failed")
	}
}

// ask hands a failure to the orchestrator, or to the principal when the
// piece has used up its orchestrator calls.
func (d *decider) ask(id, failure string) {
	p := d.ps(id)
	if !Allows(d.lim.OrchestratorCalls, p.Calls+1) {
		q := fmt.Sprintf("%s hit the orchestrator call limit after a %s failure", id, failure)
		back := PieceRunning
		if p.Status == PiecePROpen {
			back = "" // the piece stays pr-open and its pull request is still watched
		}
		d.wait(id, WaitLimit, q, back)
		return
	}
	d.add(AskOrchestrator{Piece: id, Failure: failure})
}

func (d *decider) done(id string) {
	g, ok := d.o.Git[id]
	switch {
	case !ok:
	case len(g.Dirty) > 0 && d.ps(id).SentBack[reasonUncommit] < 1:
		d.add(SendBack{Piece: id, Reason: reasonUncommit, Note: uncommittedNote(g.Dirty)})
	case len(g.Dirty) > 0:
		d.ask(id, reasonUncommit)
	case d.collects(id):
	case g.Ahead == 0:
		d.dropsTakenBack(id)
		if d.finished(id) {
			d.add(Cleanup{Piece: id})
		}
	default:
		d.dropsTakenBack(id)
		d.add(OpenPR{Piece: id})
	}
}

func uncommittedNote(dirty []string) string {
	shown := dirty
	if len(shown) > maxDirtyPaths {
		shown = shown[:maxDirtyPaths]
	}
	note := "These files have uncommitted changes: " + strings.Join(shown, ", ")
	if extra := len(dirty) - len(shown); extra > 0 {
		note += fmt.Sprintf(" (and %d more)", extra)
	}
	return note + ". Commit your work."
}

// remind notifies for each wait that has gone unanswered for remind, doubling
// each time. A remind of zero or less means no reminders.
func (d *decider) remind(remind time.Duration) {
	if remind <= 0 {
		return
	}
	for i := range d.s.Pieces {
		p := &d.s.Pieces[i]
		if p.Wait == nil {
			continue
		}
		if due, ok := reminderDelay(remind, p.Wait.Reminded); ok && d.o.Now.Sub(p.Wait.Since) >= due {
			p.Wait.Reminded++
			d.notify(waitText(*p))
		}
	}
}

// reminderDelay is remind << n, or false when that would overflow.
func reminderDelay(remind time.Duration, n int) (time.Duration, bool) {
	if n < 0 || n >= 62 || remind > time.Duration(math.MaxInt64)>>uint(n) {
		return 0, false
	}
	return remind << uint(n), true
}

// finish ends the mission once every piece is complete or cancelled.
func (d *decider) finish() {
	for _, p := range d.s.Pieces {
		if p.Status != PieceComplete && p.Status != PieceCancelled {
			return
		}
	}
	d.s.Status = StatusDone
	d.record(EvMissionDone, "", "")
	d.notify("mission " + d.s.MissionID + " is done")
}
