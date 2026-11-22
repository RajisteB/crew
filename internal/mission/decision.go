package mission

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	orchestratorBy   = "orchestrator"
	invalidAction    = "invalid"
	decisionTag      = "decision"
	invalidPrefix    = "the orchestrator's decision couldn't be used: "
	maxCheckMessages = 3
	maxReasonRunes   = 500
	maxNameRunes     = 100
)

// OrchDecision is the orchestrator's reply.
type OrchDecision struct {
	Action  string `json:"action"`  // "retry" | "goto" | "cancel" | "rebrief" | "add" | "escalate"
	Station string `json:"station"` // goto
	Target  string `json:"target"`  // rebrief: the piece to re-brief
	Brief   string `json:"brief"`   // rebrief
	Piece   *Piece `json:"piece"`   // add
	Quote   string `json:"quote"`   // add: the part of the request it serves
	Note    string `json:"note"`    // goto: for the station; escalate: the question
	Reason  string `json:"reason"`
}

// cutDecision limits the model's free text before anything is stored: state,
// events and notifications hold short messages only. Brief and Piece are plan
// content and stay whole.
func cutDecision(d OrchDecision) OrchDecision {
	d.Reason = cutText(d.Reason, maxReasonRunes)
	d.Note = cutText(d.Note, maxReasonRunes)
	d.Action = cutText(d.Action, maxNameRunes)
	d.Station = cutText(d.Station, maxNameRunes)
	d.Target = cutText(d.Target, maxNameRunes)
	d.Quote = cutText(d.Quote, maxNameRunes)
	return d
}

// tooLong names the first of action, station or target that is over its cap.
// Such a value can never be valid, so it is refused before any lookup.
func tooLong(d OrchDecision) string {
	for _, f := range []struct{ name, v string }{{"action", d.Action}, {"station", d.Station}, {"target", d.Target}} {
		if utf8.RuneCountInString(f.v) > maxNameRunes {
			return fmt.Sprintf("the %s is too long (over %d characters)", f.name, maxNameRunes)
		}
	}
	return ""
}

// cutText cuts s to n runes and makes it valid UTF-8.
func cutText(s string, n int) string {
	return cutRunes(strings.ToValidUTF8(s, "\uFFFD"), n)
}

// RunStep is what the engine should do to a piece's run.
type RunStep struct{ Piece, Kind, Station, Note string } // Kind: "retry" | "goto" | "approve" | "stop"

// ParseDecision decodes the last fenced block tagged `decision`.
func ParseDecision(reply string) (OrchDecision, error) {
	body, ok := lastTaggedBlock(reply, decisionTag)
	if !ok {
		return OrchDecision{}, errors.New("the reply has no fenced block tagged `decision`")
	}
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var d OrchDecision
	if err := dec.Decode(&d); err != nil {
		return OrchDecision{}, fmt.Errorf("the decision block is not one valid JSON object: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return OrchDecision{}, errors.New("the decision block has content after the JSON object")
	}
	return d, nil
}

// lastTaggedBlock returns the body of the last closed fenced block whose info
// string is exactly tag. Blocks with other tags are skipped whole, so a fence
// inside them is not mistaken for ours.
func lastTaggedBlock(reply, tag string) (string, bool) {
	var found string
	var ok bool
	var open int // length of the opening fence; 0 outside a block
	var info string
	var body []string
	for _, line := range strings.Split(reply, "\n") {
		t := strings.TrimSpace(line)
		n := len(t) - len(strings.TrimLeft(t, "`"))
		switch {
		case open == 0 && n >= 3:
			open, info, body = n, strings.TrimSpace(t[n:]), nil
		case open > 0 && n >= open && n == len(t):
			if info == tag {
				found, ok = strings.Join(body, "\n"), true
			}
			open = 0
		case open > 0:
			body = append(body, line)
		}
	}
	return found, ok
}

// applier is a decider working on one piece's decision.
type applier struct {
	decider
	in      CheckInput
	by      string
	piece   string
	failure string
	quote   string  // the model's full quote: checked uncut
	next    Mission // the plan with an added piece, when the action is add
}

// ApplyDecision carries out the orchestrator's decision. A decision that
// can't be used, or repeats an earlier one for the same stop, becomes a
// question for the principal instead. m and s are never modified.
func ApplyDecision(m Mission, s State, piece, failure string, d OrchDecision, by string, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep) {
	a := &applier{decider: decider{m: m, s: s.Clone(), o: Obs{Now: now}}, in: in, by: by, piece: piece, failure: failure, quote: d.Quote}
	long := tooLong(d)
	d = cutDecision(d) // validation below sees full values only through long and quote
	if a.ps(piece) == nil {
		return m, a.s, nil, nil
	}
	why, action := a.rejection(d)
	if long != "" {
		why, action = long, invalidAction
	}
	if why != "" {
		a.reject(d, action, why)
		return m, a.s, a.actions, nil
	}
	a.accept(d, d.Action, d.Reason)
	return a.carryOut(d)
}

func (a *applier) carryOut(d OrchDecision) (Mission, State, []Action, *RunStep) {
	var step *RunStep
	switch d.Action {
	case "retry":
		step = &RunStep{Piece: a.piece, Kind: "retry"}
	case "goto":
		a.resumeFromPR()
		step = &RunStep{Piece: a.piece, Kind: "goto", Station: d.Station, Note: d.Note}
	case "cancel":
		step = a.cancelPiece(a.piece, a.by)
	case "rebrief":
		a.rebrief(d)
	case "add":
		a.addPiece(d)
	case "escalate":
		a.escalate(d.Note)
	}
	return a.m, a.s, a.actions, step
}

// rejection says why d can't be used ("" when it can) and which action to
// log for it.
func (a *applier) rejection(d OrchDecision) (why, action string) {
	switch {
	case d.Action == "" && d.Reason != "":
		return d.Reason, invalidAction
	case d.Action == "":
		return "the decision names no action", invalidAction
	case !slices.Contains(allowedFor(a.failure), d.Action):
		return fmt.Sprintf("%q is not an action it may take", d.Action), invalidAction
	case a.repeats(d):
		return fmt.Sprintf("it repeats an earlier %s for the same stop", d.Action), d.Action
	}
	return a.fieldProblem(d), invalidAction
}

// repeats reports whether the piece already got this action and station for
// this failure.
func (a *applier) repeats(d OrchDecision) bool {
	for _, e := range a.ps(a.piece).Decisions {
		if e.Failure == a.failure && e.Action == d.Action && e.Station == d.Station {
			return true
		}
	}
	return false
}

// fieldProblem checks what the action needs; "" means it is usable.
func (a *applier) fieldProblem(d OrchDecision) string {
	switch d.Action {
	case "goto":
		if !stationExists(a.in, a.m, a.piece, d.Station) {
			return fmt.Sprintf("station %q is not in the piece's factory", d.Station)
		}
	case "rebrief":
		return a.rebriefProblem(d)
	case "add":
		return a.addProblem(d)
	case "escalate":
		if strings.TrimSpace(d.Note) == "" {
			return "escalate needs a note holding the question"
		}
	}
	return ""
}

func (a *applier) rebriefProblem(d OrchDecision) string {
	target, ok := a.s.Piece(d.Target)
	switch {
	case !ok:
		return fmt.Sprintf("rebrief target %q is not a piece", d.Target)
	case target.Status != PieceQueued:
		return fmt.Sprintf("piece %s is %s; only a queued piece can be re-briefed", d.Target, target.Status)
	case strings.TrimSpace(d.Brief) == "":
		return "rebrief needs a brief"
	}
	return ""
}

func (a *applier) addProblem(d OrchDecision) string {
	switch {
	case d.Piece == nil:
		return "add needs a piece"
	case strings.TrimSpace(a.quote) == "" || !strings.Contains(a.m.Request, a.quote):
		return "the quote is not in the request"
	case !Allows(a.in.Limits.Additions, a.s.Additions+1):
		return "the orchestrator has used up its piece additions"
	}
	next := a.m.WithChange(a.change("add piece "+d.Piece.ID, d.Reason))
	next.Pieces = append(next.Pieces, copyPiece(*d.Piece))
	if errs := Check(next, a.in); len(errs) > 0 {
		return "the plan with the new piece fails its checks: " + firstMessages(errs)
	}
	a.next = next
	return ""
}

func copyPiece(p Piece) Piece {
	p.Paths = slices.Clone(p.Paths)
	p.WaitsFor = slices.Clone(p.WaitsFor)
	return p
}

func firstMessages(errs []error) string {
	var parts []string
	for _, e := range errs[:min(len(errs), maxCheckMessages)] {
		parts = append(parts, e.Error())
	}
	return strings.Join(parts, "; ")
}

func (a *applier) change(what, why string) Change {
	return Change{Time: a.o.Now, By: orchestratorBy, What: what, Why: why}
}

// accept logs a usable decision on the piece.
func (a *applier) accept(d OrchDecision, action, reason string) {
	p := a.ps(a.piece)
	p.Decisions = append(p.Decisions, Decision{Time: a.o.Now, By: a.by, Failure: a.failure,
		Action: action, Station: d.Station, Note: d.Note, Reason: reason})
	msg := strings.TrimSpace(action + " " + d.Station + ": " + reason)
	a.add(Record{Event: Event{Time: a.o.Now, Kind: EvDecision, Piece: a.piece, By: a.by, Msg: msg}})
}

// reject logs a decision that can't be used and asks the principal instead.
func (a *applier) reject(d OrchDecision, action, why string) {
	a.accept(d, action, why)
	a.escalate(invalidPrefix + why)
}

// escalate puts a question to the principal and holds the piece until it is
// answered.
func (a *applier) escalate(question string) {
	a.openWait(a.piece, WaitQuestion, question, a.backAfterQuestion())
	a.add(Record{Event: Event{Time: a.o.Now, Kind: EvEscalated, Piece: a.piece, By: a.by, Msg: question}})
	a.notify(waitText(*a.ps(a.piece)))
}

func (a *applier) rebrief(d OrchDecision) {
	a.m = a.m.WithChange(a.change("rebrief "+d.Target, d.Reason))
	for i := range a.m.Pieces {
		if a.m.Pieces[i].ID == d.Target {
			a.m.Pieces[i].Brief = d.Brief
		}
	}
	a.add(Record{Event: Event{Time: a.o.Now, Kind: EvRebrief, Piece: d.Target, By: a.by, Msg: "brief changed"}})
}

func (a *applier) addPiece(d OrchDecision) {
	a.m = a.next
	a.s.Pieces = append(a.s.Pieces, PieceState{ID: d.Piece.ID, Status: PieceQueued, Added: true})
	a.s.Additions++
	a.add(Record{Event: Event{Time: a.o.Now, Kind: EvPieceAdded, Piece: d.Piece.ID, By: a.by, Msg: "added by the orchestrator"}})
	a.notify("added piece " + d.Piece.ID)
}

// cancelPiece cancels a piece and clears its wait. It returns a stop step
// when a run exists and queues a cleanup when a worktree exists.
func (d *decider) cancelPiece(id, by string) *RunStep {
	p := d.ps(id)
	var step *RunStep
	if p.RunID != "" {
		step = &RunStep{Piece: id, Kind: "stop"}
	}
	msg := "cancelled by " + by
	if p.PR != 0 && p.Status != PieceDelivered { // a delivered piece's PR is merged
		msg += fmt.Sprintf("; PR #%d stays open", p.PR)
	}
	p.Status, p.Wait = PieceCancelled, nil
	d.add(Record{Event: Event{Time: d.o.Now, Kind: EvPieceCancelled, Piece: id, By: by, Msg: msg}})
	if p.Worktree != "" {
		d.add(Cleanup{Piece: id})
	}
	return step
}

// stationExists reports whether the piece's factory has the station.
func stationExists(in CheckInput, m Mission, piece, station string) bool {
	p, ok := m.Piece(piece)
	if !ok {
		return false
	}
	f, ok := findFactory(in.Factories, p.Factory)
	if !ok {
		return false
	}
	_, ok = f.Stations[station]
	return ok
}
