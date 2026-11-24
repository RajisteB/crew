package mission

import (
	"cmp"
	"errors"
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

const (
	defaultMergeMethod = "squash"
	maxConflictFiles   = 20
	maxLoggedChecks    = 3
)

// switchSnapshot is one read of the auto-merge switches.
type switchSnapshot struct {
	recs map[string]principal.SwitchRecord
	err  error
}

// readSwitches reads the switches from disk. With no reader every domain is off.
func (t *tick) readSwitches() switchSnapshot {
	if t.e.Switches == nil {
		return switchSnapshot{}
	}
	recs, err := t.e.Switches.Switches(t.s.Repo)
	return switchSnapshot{recs: recs, err: err}
}

// observeAuto sets Obs.Auto for a pr-open piece: the switches are read once
// per tick, the changed files once per piece. A read that fails is a note and
// leaves the piece out, which is the same as off.
func (t *tick) observeAuto(ps PieceState, pr gh.PR) {
	p, ok := t.m.Piece(ps.ID)
	if !ok || t.e.Switches == nil || ps.Worktree == "" || pr.State != "OPEN" {
		return
	}
	if t.snapshot().err != nil {
		t.noteSwitchError(ps.ID, t.snapshot().err)
		return
	}
	files, err := t.e.Git.ChangedFiles(t.ctx, ps.Worktree, "origin/"+t.s.Base)
	if err != nil {
		t.note(ps.ID, "can't list the files PR #%d changes: %v; auto-merge is off until it can", ps.PR, err)
		return
	}
	t.obs.Auto[ps.ID] = EffectiveMode(p, files, t.e.Domains, t.snapshot().recs)
}

func (t *tick) noteSwitchError(piece string, err error) {
	if errors.Is(err, principal.ErrTampered) {
		t.note(piece, "automerge.json was edited outside crew, so auto-merge is off everywhere; delete it and its .sum, then set the switches again with crew automerge")
		return
	}
	t.note(piece, "can't read the auto-merge switch: %v; auto-merge is off until it can be read", err)
}

// freshMode reads the switches and the changed files again, right before a merge.
func (t *tick) freshMode(p Piece, ps PieceState) (AutoObs, bool) {
	if t.e.Switches == nil || ps.Worktree == "" {
		return AutoObs{}, false
	}
	snap := t.readSwitches()
	if snap.err != nil {
		return AutoObs{}, false
	}
	files, err := t.e.Git.ChangedFiles(t.ctx, ps.Worktree, "origin/"+t.s.Base)
	if err != nil {
		return AutoObs{}, false
	}
	return EffectiveMode(p, files, t.e.Domains, snap.recs), true
}

// merge is the only place crew merges a pull request. It reads the switch
// again first, and merges only the head crew pushed.
func (t *tick) merge(a Merge) error {
	p, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.PR == 0 {
		return nil
	}
	auto, ok := t.freshMode(p, ps)
	if !ok {
		t.note(a.Piece, "can't read the auto-merge switch or the changed files for PR #%d right now; not merging", ps.PR)
		return nil
	}
	if auto.Mode == principal.AutoOff || (auto.Mode == principal.AutoOn && !a.Checked) {
		t.note(a.Piece, "auto-merge for PR #%d was turned off; not merging", ps.PR)
		return nil
	}
	method := cmp.Or(t.e.MergeMethod, defaultMergeMethod)
	if err := t.e.GH.MergePR(t.ctx, ps.PR, method, a.Head); err != nil {
		t.note(a.Piece, "can't merge PR #%d: %v (the merge method is %s; set merge_method in missions.yaml)", ps.PR, err, method)
		return t.prWaits(a.Piece, fmt.Sprintf("PR #%d (%s): GitHub refused the merge; crew keeps trying, or merge it yourself", ps.PR, a.Piece))
	}
	if pr, err := t.e.GH.PRState(t.ctx, ps.PR); err != nil || pr.State != "MERGED" {
		t.note(a.Piece, "GitHub accepted the merge of PR #%d but it isn't merged yet (a merge queue, or GitHub's own auto-merge); crew completes the piece when it is", ps.PR)
		return nil
	}
	t.s = Merged(t.s, a.Piece)
	if err := t.save(); err != nil {
		return err
	}
	if a.Checked {
		t.emit(Event{Kind: EvChecksPassed, Piece: a.Piece, Msg: fmt.Sprintf("PR #%d", ps.PR)})
	}
	t.emit(Event{Kind: EvPRMerged, Piece: a.Piece,
		Msg: fmt.Sprintf("PR #%d merged by crew (auto-merge %s for %s)", ps.PR, auto.Mode, p.Domain)})
	if now, _ := t.s.Piece(a.Piece); now.Status == PieceComplete {
		t.cleanup(Cleanup{Piece: a.Piece, Force: true, Merged: true})
	}
	return nil
}

// resolveConflict merges the base branch into the piece's branch, or sends
// the piece back to its factory when the two conflict.
func (t *tick) resolveConflict(a ResolveConflict) error {
	p, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.PR == 0 || ps.Worktree == "" {
		return nil
	}
	base, e := t.s.Base, t.e
	dirty, err := e.Git.Dirty(t.ctx, ps.Worktree)
	if err != nil {
		t.note(a.Piece, "can't check the worktree: %v", err)
		return nil
	}
	if len(dirty) > 0 {
		t.note(a.Piece, "can't merge origin/%s into the branch: uncommitted changes in %s", base, fileList(dirty))
		return nil
	}
	if err := e.Git.Fetch(t.ctx, base); err != nil {
		t.note(a.Piece, "can't merge origin/%s: fetch: %v", base, err)
		return nil
	}
	conflicts, err := e.Git.MergeInto(t.ctx, ps.Worktree, "origin/"+base)
	if err != nil {
		t.note(a.Piece, "can't merge origin/%s into the branch: %v", base, err)
		return nil
	}
	if len(conflicts) > 0 {
		return t.conflictSendBack(p, ps, conflicts)
	}
	return t.pushMerged(ps)
}

// pushMerged pushes the branch after origin/<base> merged into it cleanly.
func (t *tick) pushMerged(ps PieceState) error {
	head, err := t.e.Git.Head(t.ctx, ps.Worktree)
	if err != nil {
		t.note(ps.ID, "can't read the branch head: %v", err)
		return nil
	}
	if head == ps.Head {
		// crew already pushed this commit; GitHub has not caught up.
		t.note(ps.ID, "GitHub reports a conflict on PR #%d, but origin/%s merges into the branch cleanly; waiting for GitHub to check again", ps.PR, t.s.Base)
		if t.now.Sub(ps.PRSince) < checksPatience {
			return nil
		}
		return t.prWaits(ps.ID, fmt.Sprintf("PR #%d (%s): GitHub still reports a conflict after 30m, though %s merges into the branch cleanly; it is waiting on you", ps.PR, ps.ID, t.s.Base))
	}
	if ok, err := t.repoAllowsPush(ps.ID); !ok {
		return err
	}
	if err := t.e.Git.Push(t.ctx, ps.Worktree, ps.Branch); err != nil {
		t.note(ps.ID, "can't push %s: %v", ps.Branch, err)
		return nil
	}
	t.s = BranchUpdated(t.s, ps.ID, head, t.now)
	if err := t.save(); err != nil {
		return err
	}
	t.emit(Event{Kind: EvConflict, Piece: ps.ID, Msg: t.s.Base + " was merged into the branch; checks run again"})
	return nil
}

func (t *tick) conflictSendBack(p Piece, ps PieceState, conflicts []string) error {
	base, files := t.s.Base, fileList(conflicts)
	note := fmt.Sprintf("%s has moved and these files conflict: %s. Merge origin/%s into this branch, resolve the conflicts and commit.", base, files, base)
	if !t.reworkStep(p, ps, "", note) {
		return nil
	}
	t.s = SentBackDone(t.s, ps.ID, reasonConflict, fmt.Sprintf("conflicts with %s in: %s", base, files))
	if err := t.save(); err != nil {
		return err
	}
	t.emit(Event{Kind: EvConflict, Piece: ps.ID, Msg: files})
	return nil
}

// fileList joins at most maxConflictFiles names and counts the rest.
func fileList(files []string) string {
	shown := files[:min(len(files), maxConflictFiles)]
	list := strings.Join(shown, ", ")
	if extra := len(files) - len(shown); extra > 0 {
		list += fmt.Sprintf(" (and %d more)", extra)
	}
	return list
}

// ---------- failed checks ----------

// failedCheckList is the checks of the pull request observed this tick that failed.
func (t *tick) failedCheckList(piece string) []gh.Check {
	var out []gh.Check
	for _, c := range t.obs.PRs[piece].Checks {
		if c.State == gh.CheckFail {
			out = append(out, c)
		}
	}
	return out
}

// checksProblem is the stored reason for a checks-failed send back: names
// only. A tick that did not observe the pull request keeps the stored one.
func (t *tick) checksProblem(piece string, ps PieceState) string {
	failed := t.failedCheckList(piece)
	if len(failed) == 0 {
		return ps.Problem
	}
	names := make([]string, len(failed))
	for i, c := range failed {
		names[i] = c.Name
	}
	return "failed checks: " + strings.Join(names, ", ")
}

// withFailedLogs adds the end of the first failed checks' logs to note. A log
// that can't be read is skipped. Logs go into the note only.
func (t *tick) withFailedLogs(piece, note string) string {
	var b strings.Builder
	b.WriteString(note)
	failed := t.failedCheckList(piece)
	for _, c := range failed[:min(len(failed), maxLoggedChecks)] {
		log, err := t.e.GH.FailedLog(t.ctx, c)
		if err != nil || strings.TrimSpace(log) == "" {
			continue
		}
		fmt.Fprintf(&b, "\n\n--- %s ---\n%s", c.Name, strings.TrimSpace(log))
	}
	return b.String()
}

// forPR gives the orchestrator's summary the pull request failure and its problem.
func (t *tick) forPR(sum Summary, failure string, ps PieceState) Summary {
	switch failure {
	case reasonChecksFailed:
		return sum.ForPR(failure, t.checksProblem(ps.ID, ps))
	case reasonConflict:
		problem := "conflicts with " + t.s.Base
		if strings.HasPrefix(ps.Problem, "conflicts with") {
			problem = ps.Problem
		}
		return sum.ForPR(failure, problem)
	}
	return sum
}

// prWaits makes the piece's merge wait say question. A wait it opens is
// recorded and notified like any other; a wait that only changes its text is
// saved and nothing more.
func (t *tick) prWaits(piece, question string) error {
	next, opened := PRWaits(t.s, piece, question, t.now)
	if !opened && !t.changed(next, piece) {
		return nil
	}
	t.s = next
	if err := t.save(); err != nil {
		return err
	}
	if opened {
		t.emit(Event{Kind: EvWaitingOnPrincipal, Piece: piece, Msg: WaitMerge + ": " + question})
		t.notify(Notify{Title: "crew mission " + t.s.MissionID, Body: question})
	}
	return nil
}

// changed reports whether next differs from the state in the piece's wait text.
func (t *tick) changed(next State, piece string) bool {
	was, _ := t.s.Piece(piece)
	now, _ := next.Piece(piece)
	return was.Wait != nil && now.Wait != nil && was.Wait.Question != now.Wait.Question
}
