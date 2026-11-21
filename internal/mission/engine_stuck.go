package mission

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// stuckTicks is how many consecutive ticks the same failure may last before
// the principal is told, when the settings give no remind_after.
const stuckTicks = 5

const tickErrorPrefix = "tick failed: "

// streak is one failure (a note, or a tick error) in an unbroken run of ticks.
type streak struct {
	piece, text string
	since       time.Time
	ticks       int
	told        bool
}

// failureKey is how a failure is told apart from another.
func failureKey(piece, text string) string { return piece + "\x00" + text }

// trackFailures folds the failures of the tick that just ended into the
// engine's memory and tells the principal, once, about any that has lasted
// long enough. A failure missing from this tick ends its run. errs are the
// tick's own errors (not part of any piece).
func (e *Engine) trackFailures(notes map[string]struct{}, tickErr error) {
	now := e.now()
	seen := map[string]streak{}
	add := func(piece, text string) {
		k := failureKey(piece, text)
		s, ok := e.streaks[k]
		if !ok {
			s = streak{piece: piece, text: text, since: now}
		}
		s.ticks++
		seen[k] = s
	}
	for k := range notes {
		piece, text := splitFailureKey(k)
		add(piece, text)
	}
	if tickErr != nil {
		add("", tickErrorPrefix+cutRunes(tickErr.Error(), noteRunes))
	}
	for k, s := range seen {
		if !s.told && e.stuckLongEnough(s, now) {
			e.tellStuck(s)
			s.told = true
			seen[k] = s
		}
	}
	e.streaks = seen
}

func splitFailureKey(k string) (piece, text string) {
	piece, text, _ = strings.Cut(k, "\x00")
	return piece, text
}

func (e *Engine) stuckLongEnough(s streak, now time.Time) bool {
	if remind := e.Settings.Remind(); remind > 0 {
		return now.Sub(s.since) >= remind
	}
	return s.ticks >= stuckTicks
}

// tellStuck notifies the principal that a failure keeps repeating.
func (e *Engine) tellStuck(s streak) {
	id := filepath.Base(e.Store.Dir)
	what := s.text
	if s.piece != "" {
		what = s.piece + ": " + what
	}
	body := cutRunes(fmt.Sprintf("mission %s keeps failing: %s", id, what), noteRunes)
	if err := e.notifier().Notify("crew mission "+id, body); err != nil {
		fmt.Fprintf(e.out(), "mission %s: can't notify: %v\n", id, err)
		return
	}
	e.emit(Event{Kind: EvNotified, Piece: s.piece, Msg: body})
}
