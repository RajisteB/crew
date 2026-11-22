package mission

import (
	"fmt"
	"slices"
	"strings"
	"time"
)

const (
	principalBy     = "principal"
	maxAnswerRunes  = 500
	answerRetry     = "retry"
	answerGoto      = "goto"
	answerCancel    = "cancel"
	answerApprove   = "approve"
	noChoicesAnswer = "none"
)

// Answer is the principal's reply to a wait.
type Answer struct {
	Wait    string `json:"wait"`
	Do      string `json:"do"` // "retry" | "goto" | "cancel" | "approve"
	Station string `json:"station"`
	Note    string `json:"note"`
	Words   string `json:"words"` // what the principal said, word for word
}

// ApplyAnswer applies the principal's answer to the piece holding the wait.
// On an error it changes nothing and returns its inputs.
func ApplyAnswer(m Mission, s State, a Answer, in CheckInput, now time.Time) (Mission, State, []Action, *RunStep, error) {
	id, w, ok := findWait(s, a.Wait)
	if !ok {
		return m, s, nil, nil, fmt.Errorf("no piece is waiting on %q", a.Wait)
	}
	if err := checkAnswer(m, in, id, w, a); err != nil {
		return m, s, nil, nil, err
	}
	d := &decider{m: m, s: s.Clone(), o: Obs{Now: now}}
	p := d.ps(id)
	p.Wait = nil
	if w.Back != "" {
		p.Status = w.Back
	}
	p.Decisions = append(p.Decisions, Decision{Time: now, By: principalBy, Action: a.Do, Station: a.Station, Note: a.Note})
	d.add(Record{Event: Event{Time: now, Kind: EvAnswered, Piece: id, By: principalBy, Msg: cutRunes(a.Words, maxAnswerRunes)}})
	return m, d.s, d.actions, answerStep(d, id, w, a), nil
}

// answerStep applies what the principal chose and returns the run step.
func answerStep(d *decider, id string, w Wait, a Answer) *RunStep {
	if onOpenPR(d.ps(id), w) {
		if step, done := answerOnPR(d, id, a); done {
			return step
		}
	}
	switch a.Do {
	case answerApprove:
		return &RunStep{Piece: id, Kind: "approve", Station: a.Station, Note: a.Note}
	case answerGoto:
		return &RunStep{Piece: id, Kind: "goto", Station: a.Station, Note: a.Note}
	case answerCancel:
		return d.cancelPiece(id, principalBy)
	}
	if w.Kind == WaitPRClosed {
		d.ps(id).Status = PiecePROpen
		return nil
	}
	return &RunStep{Piece: id, Kind: "retry"}
}

func findWait(s State, id string) (string, Wait, bool) {
	for _, p := range s.Pieces {
		if p.Wait != nil && p.Wait.ID == id {
			return p.ID, *p.Wait, true
		}
	}
	return "", Wait{}, false
}

// checkAnswer says whether the answer fits the wait.
func checkAnswer(m Mission, in CheckInput, piece string, w Wait, a Answer) error {
	choices := answerChoices(w)
	if !slices.Contains(choices, a.Do) {
		list := noChoicesAnswer
		if len(choices) > 0 {
			list = strings.Join(choices, ", ")
		}
		return fmt.Errorf("wait %s (%s) can't be answered with %q; the choices are: %s", w.ID, w.Kind, a.Do, list)
	}
	needsStation := a.Do == answerGoto || (a.Do == answerApprove && a.Station != "")
	if needsStation && !stationExists(in, m, piece, a.Station) {
		return fmt.Errorf("wait %s: station %q is not in the factory of piece %s", w.ID, a.Station, piece)
	}
	return nil
}

// answerChoices are the answers that fit a wait.
func answerChoices(w Wait) []string {
	switch {
	case w.Kind == WaitApproval:
		return []string{answerApprove, answerCancel}
	case w.Kind == WaitQuestion && w.Back == PieceQueued:
		return []string{answerCancel}
	case w.Kind == WaitQuestion || w.Kind == WaitLimit:
		return []string{answerRetry, answerGoto, answerCancel}
	case w.Kind == WaitPRClosed:
		return []string{answerRetry, answerCancel}
	case w.Kind == WaitMerge:
		return []string{answerCancel}
	}
	return nil
}

// cutRunes cuts s to at most n runes, on a rune boundary.
func cutRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
