package mission

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crewfile-dev/crew/internal/run"
)

const noteRunes = 300

// pieceMission looks up the piece in the plan and in the state.
func (t *tick) pieceMission(id string) (Piece, PieceState, bool) {
	p, okP := t.m.Piece(id)
	ps, okS := t.s.Piece(id)
	return p, ps, okP && okS
}

// perform carries out each action in order. An effect that fails is a note
// and its reducer is not called; only a failure to save stops the tick. A
// failure to write an event is reported on Out and does not stop it.
func (t *tick) perform(actions []Action) error {
	for _, a := range actions {
		if t.halted {
			return nil
		}
		var err error
		switch a := a.(type) {
		case PlanPieces:
			err = t.planPieces(a)
		case StartPiece:
			err = t.startPiece(a)
		case DriveRun:
			err = t.driveRun(a)
		case SendBack:
			err = t.sendBack(a)
		case Merge:
			err = t.merge(a)
		case ResolveConflict:
			err = t.resolveConflict(a)
		case OpenPR:
			err = t.openPR(a)
		case CollectDeliverable:
			err = t.collectDeliverable(a)
		case AskOrchestrator:
			err = t.askOrchestrator(a)
		case Notify:
			t.notify(a)
		case Cleanup:
			t.cleanup(a)
		case Record:
			if t.unsaved {
				t.held = append(t.held, a.Event)
			} else {
				t.emit(a.Event)
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// freshWorktree fetches the base and makes a new worktree for the piece,
// clearing any left over. A step that fails is a note and returns false.
func (t *tick) freshWorktree(id, wt, branch, base string) bool {
	git := t.e.Git
	if err := git.Fetch(t.ctx, base); err != nil {
		t.note(id, "can't start: fetch %s: %v", base, err)
		return false
	}
	if err := git.RemoveWorktree(t.ctx, wt, true); err != nil {
		t.note(id, "can't start: clear leftover worktree: %v", err)
		return false
	}
	if err := git.AddWorktree(t.ctx, wt, branch, "origin/"+base); err != nil {
		t.note(id, "can't start: add worktree: %v", err)
		return false
	}
	return true
}

func (t *tick) startPiece(a StartPiece) error {
	p, _, ok := t.pieceMission(a.Piece)
	if !ok {
		return nil
	}
	e := t.e
	wt := filepath.Join(e.RepoRoot, run.CrewDir, "worktrees", t.s.MissionID+"-"+p.ID)
	branch := "crew/" + t.s.MissionID + "/" + p.ID
	base := t.s.Base
	release, ok := t.takeStartSlot()
	if !ok {
		return nil
	}
	defer release()
	if !t.startClear(p) || !t.freshWorktree(p.ID, wt, branch, base) {
		return nil
	}
	earlier, err := t.prepareScratch(p, wt)
	if err != nil {
		t.note(p.ID, "can't start: prepare the deliverables folders: %v", err)
		t.dropFailedStart(p.ID, wt)
		return nil
	}
	runID, err := e.Runs.Start(p, PieceInput(t.s.MissionID, t.m, p, e.Domains[p.Domain], earlier), wt)
	if err != nil {
		t.note(p.ID, "can't start: run: %v", err)
		t.dropFailedStart(p.ID, wt)
		return nil
	}
	t.s = Started(t.s, p.ID, runID, branch, wt)
	if err := t.save(); err != nil {
		return err
	}
	release()
	t.emit(Event{Kind: EvPieceStart, Piece: p.ID, Run: runID, Msg: branch})
	if err := e.Runs.Drive(t.ctx, runID, wt); err != nil {
		t.note(p.ID, "can't drive the run: %v", err)
	}
	return nil
}

// dropFailedStart removes the worktree of a piece that could not start.
func (t *tick) dropFailedStart(piece, wt string) {
	if err := t.e.Git.RemoveWorktree(t.ctx, wt, true); err != nil {
		t.note(piece, "can't clean up the worktree after a failed start: %v", err)
	}
}

func (t *tick) driveRun(a DriveRun) error {
	_, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.RunID == "" {
		return nil
	}
	if err := t.e.Runs.Drive(t.ctx, ps.RunID, ps.Worktree); err != nil {
		t.note(a.Piece, "can't drive the run: %v", err)
		return nil
	}
	// Re-attaching to a run whose driver died changes no mission state; only
	// restarting a failed run counts as a resume.
	if t.obs.Runs[a.Piece].State.Status == run.StatusFailed {
		t.s = Resumed(t.s, a.Piece)
		return t.save()
	}
	return nil
}

func (t *tick) sendBack(a SendBack) error {
	p, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.RunID == "" {
		return nil
	}
	note, problem := a.Note, ""
	if a.Reason == reasonChecksFailed {
		note, problem = t.withFailedLogs(a.Piece, note), t.checksProblem(a.Piece, ps)
	}
	if !t.reworkStep(p, ps, a.Station, note) {
		return nil
	}
	t.s = SentBackDone(t.s, a.Piece, a.Reason, problem)
	if err := t.save(); err != nil {
		return err
	}
	if a.Reason == reasonChecksFailed {
		t.emit(Event{Kind: EvChecksFailed, Piece: a.Piece, Msg: strings.TrimPrefix(problem, "failed checks: ")})
	}
	return nil
}

// reworkStep runs a goto step to the station, or to the factory's rework
// station when it is empty. A failure is a note and reports false.
func (t *tick) reworkStep(p Piece, ps PieceState, station, note string) bool {
	if station == "" {
		f, found := findFactory(t.in.Factories, p.Factory)
		if !found {
			t.note(p.ID, "can't send back: no factory %q", p.Factory)
			return false
		}
		station = f.ReworkStation()
	}
	step := RunStep{Piece: p.ID, Kind: "goto", Station: station, Note: note}
	if err := t.e.Runs.Step(t.ctx, ps.RunID, ps.Worktree, step); err != nil {
		t.note(p.ID, "can't send back to %s: %v", station, err)
		return false
	}
	return true
}

func (t *tick) openPR(a OpenPR) error {
	p, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.RunID == "" {
		return nil
	}
	e := t.e
	if ok, err := t.repoAllowsPush(a.Piece); !ok {
		return err
	}
	if err := e.Git.Push(t.ctx, ps.Worktree, ps.Branch); err != nil {
		t.note(a.Piece, "can't push %s: %v", ps.Branch, err)
		return nil
	}
	head, err := e.Git.Head(t.ctx, ps.Worktree)
	if err != nil {
		t.note(a.Piece, "can't read the branch head: %v", err)
		return nil
	}
	pr, found, err := e.GH.FindPR(t.ctx, ps.Branch)
	if err != nil {
		t.note(a.Piece, "can't look for an existing pull request: %v", err)
		return nil
	}
	if !found {
		_, _, last, _, err := e.Runs.Summary(ps.RunID)
		if err != nil {
			t.note(a.Piece, "can't read the run's last step: %v", err)
			return nil
		}
		title, body := PRText(t.s.MissionID, t.m, p, last)
		if pr, err = e.GH.CreatePR(t.ctx, ps.Branch, t.s.Base, title, body); err != nil {
			t.note(a.Piece, "can't open the pull request: %v", err)
			return nil
		}
	}
	t.s = PROpened(t.s, a.Piece, pr, head, t.now)
	if err := t.save(); err != nil {
		return err
	}
	t.emit(Event{Kind: EvPROpened, Piece: a.Piece, Run: ps.RunID, Msg: fmt.Sprintf("PR #%d", pr.Number)})
	return nil
}

func (t *tick) askOrchestrator(a AskOrchestrator) error {
	_, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.RunID == "" {
		return nil
	}
	rs, f, last, stepsDir, err := t.e.Runs.Summary(ps.RunID)
	if err != nil {
		t.note(a.Piece, "can't summarise the run for the orchestrator: %v", err)
		return nil
	}
	sum := BuildSummary(t.s.MissionID, t.m, ps, rs, f, last, stepsDir, t.lim, t.s.Additions, StepsUsed(t.s))
	sum = t.forPR(sum, a.Failure, ps)
	if a.Failure == reasonOutOfPaths {
		sum = sum.ForPaths(ps)
	}
	asker := AskerOf(ps)
	sum = t.scopeSummary(sum, asker)
	d, err := t.decide(sum)
	if t.ctx.Err() != nil {
		return nil // stopped during the call: not a decision, not a question; the next engine asks again
	}
	t.emitInOrder(Event{Kind: EvException, Piece: a.Piece, Run: ps.RunID, Msg: a.Failure})
	if err != nil {
		d = OrchDecision{Reason: err.Error()}
	}
	asked := Asked(t.s, a.Piece)
	m2, s2, acts, step := ApplyScopedDecision(t.m, asked, a.Piece, a.Failure, d, asker, t.in, t.now)
	if err := t.runStep(step); err != nil {
		t.note(a.Piece, "can't carry out the orchestrator's decision: %v", err)
		t.s = asked // the call was made and counts
		return t.save()
	}
	t.adopt(m2, s2)
	if err := t.save(); err != nil {
		return err
	}
	return t.perform(acts)
}

func (t *tick) decide(sum Summary) (OrchDecision, error) {
	if t.e.Orch == nil {
		return OrchDecision{}, fmt.Errorf("no orchestrator is configured")
	}
	return t.e.Orch.Decide(t.ctx, t.e.RepoRoot, sum)
}

// runStep carries out a run step the state machine asked for.
func (t *tick) runStep(step *RunStep) error {
	if step == nil {
		return nil
	}
	ps, ok := t.s.Piece(step.Piece)
	if !ok || ps.RunID == "" {
		return fmt.Errorf("piece %s has no run", step.Piece)
	}
	if step.Kind == "stop" {
		return t.e.Runs.Stop(ps.RunID)
	}
	return t.e.Runs.Step(t.ctx, ps.RunID, ps.Worktree, *step)
}

func (t *tick) notify(a Notify) {
	title := a.Title
	if title == "" {
		title = "crew"
	}
	if err := t.e.notifier().Notify(title, a.Body); err != nil {
		t.note("", "can't notify: %v", err)
		return
	}
	t.emitInOrder(Event{Kind: EvNotified, Msg: cutRunes(a.Body, noteRunes)})
}

func (t *tick) cleanup(a Cleanup) {
	ps, ok := t.s.Piece(a.Piece)
	if !ok {
		return
	}
	if !t.removeWorktree(ps, a.Force) || !a.Merged || ps.Branch == "" {
		return
	}
	if err := t.e.Git.DeleteBranch(t.ctx, ps.Branch); err != nil {
		t.note(ps.ID, "can't delete the local branch %s: %v", ps.Branch, err)
	}
}

// removeWorktree removes the piece's worktree and records that it is gone.
// A failure is a note; the worktree stays recorded so removal is retried
// when the loop starts again. It reports whether the worktree is gone.
func (t *tick) removeWorktree(ps PieceState, force bool) bool {
	if ps.Worktree == "" {
		return true
	}
	if err := t.e.Git.RemoveWorktree(t.ctx, ps.Worktree, force); err != nil {
		if force {
			t.note(ps.ID, "can't remove the worktree: %v", err)
		} else {
			t.note(ps.ID, "%s: worktree left at %s (uncommitted work); remove it by hand when done", ps.ID, ps.Worktree)
		}
		return false
	}
	t.s = CleanedUp(t.s, ps.ID)
	if err := t.save(); err != nil {
		fmt.Fprintf(t.e.out(), "mission %s: can't save state: %v\n", t.s.MissionID, err)
	}
	return true
}

// retryCleanups removes the worktrees of finished pieces that still have
// one. Complete pieces are merged or empty, so removal is forced; a
// cancelled piece's uncommitted work is left for the principal.
func (t *tick) retryCleanups() {
	for _, ps := range t.s.Pieces {
		switch ps.Status {
		case PieceComplete:
			t.removeWorktree(ps, true)
		case PieceCancelled:
			t.removeWorktree(ps, false)
		}
	}
}
