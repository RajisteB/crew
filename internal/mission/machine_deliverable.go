package mission

import (
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/principal"
)

const (
	maxQuestionFiles = 3
	byEngine         = "engine"
)

// AcceptObs is what the engine saw about a posted, unaccepted deliverable.
type AcceptObs struct {
	Mode      principal.AutoMode // the switch of the piece's own domain; off when it can't be read
	MovedDone bool               // the piece's Jira issue is in the Done status
}

// unaccepted reports whether the piece has a posted deliverable nobody accepted.
func unaccepted(p PieceState) bool { return p.Deliverable != nil && !p.Deliverable.Accepted }

// finished is where a piece's work ends: a piece with no unaccepted
// deliverable completes, any other becomes delivered, with no wait, and keeps
// its worktree. It reports whether the piece completed.
func (d *decider) finished(id string) bool {
	if p := d.ps(id); unaccepted(*p) {
		p.Status, p.Wait, p.Up = PieceDelivered, nil, 0
		return false
	}
	d.complete(id)
	return true
}

// deliverables accepts posted deliverables by the switch or on the board, then
// completes delivered pieces whose deliverable is accepted and opens the wait
// of those still unaccepted.
func (d *decider) deliverables() {
	for _, id := range d.deliverableIDs() {
		p := d.ps(id)
		if unaccepted(*p) {
			d.autoAccept(id)
		}
		if p.Status != PieceDelivered {
			continue
		}
		switch {
		case !unaccepted(*p):
			d.complete(id)
			// A delivered piece with a pull request has it merged.
			d.add(Cleanup{Piece: id, Force: true, Merged: p.PR != 0})
		case p.Wait == nil:
			d.wait(id, WaitDeliverable, deliverableQuestion(d.s.MissionID, *p), "")
		}
	}
}

// deliverableIDs are the pieces still in play that hold an unaccepted
// deliverable, or are delivered.
func (d *decider) deliverableIDs() []string {
	var ids []string
	for _, p := range d.s.Pieces {
		if p.Status == PieceComplete || p.Status == PieceCancelled {
			continue
		}
		if unaccepted(p) || p.Status == PieceDelivered {
			ids = append(ids, p.ID)
		}
	}
	return ids
}

// autoAccept accepts the deliverable when the piece's domain has auto-merge
// on, or when the principal moved its issue to Done.
func (d *decider) autoAccept(id string) {
	obs := d.o.Accept[id]
	switch {
	case obs.Mode == principal.AutoOn || obs.Mode == principal.AutoWithoutChecks:
		piece, _ := d.m.Piece(id)
		d.accept(id, byEngine, fmt.Sprintf("auto-merge is %s for %s", obs.Mode, piece.Domain))
	case obs.MovedDone:
		d.accept(id, principalBy, "its Jira issue was moved to Done")
	}
}

// accept marks the piece's deliverable accepted and records who did it. The
// piece's status and wait are not touched.
func (d *decider) accept(id, by, msg string) {
	d.ps(id).Deliverable.Accepted = true
	d.ps(id).Deliverable.By = by
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvDeliverableAccepted, Piece: id, By: by, Msg: msg}})
}

// deliverableQuestion is what the principal is asked about a delivered piece.
func deliverableQuestion(missionID string, p PieceState) string {
	files := p.Deliverable.Files
	shown := files[:min(len(files), maxQuestionFiles)]
	list := strings.Join(shown, ", ")
	if extra := len(files) - len(shown); extra > 0 {
		list += fmt.Sprintf(" and %d more", extra)
	}
	if p.Jira.Issue != "" {
		return fmt.Sprintf("deliverable to accept (%s): move %s to Done on the board, or answer %s", list, p.Jira.Issue, answerAccept)
	}
	return fmt.Sprintf("deliverable to accept (%s) in %s: answer %s", list, DeliverablesRel(missionID, p.ID), answerAccept)
}
