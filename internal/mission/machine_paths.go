package mission

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	reasonOutOfPaths = "out-of-paths"
	maxOutsideFiles  = 200
	maxOutsideShown  = 20  // files named in a note
	questionShown    = 3   // files named in a question
	maxNameShown     = 120 // runes of a file name shown
)

// outsideStops reports whether the done piece changed files outside its paths
// that nobody accepted. If so it holds them in the piece and puts the piece to
// the orchestrator. Otherwise it clears them and reports false.
func (d *decider) outsideStops(id string, g GitObs) bool {
	p := d.ps(id)
	fresh := slices.DeleteFunc(slices.Clone(g.Outside), func(f string) bool { return slices.Contains(p.Allowed, f) })
	if len(fresh) == 0 {
		p.Outside, p.OutsideMore = nil, 0
		return false
	}
	shown := fresh[:min(len(fresh), maxOutsideFiles)]
	p.Outside, p.OutsideMore = shown, len(fresh)-len(shown)
	d.ask(id, reasonOutOfPaths)
	return true
}

// pathsQuestion is what the principal is asked about a piece's outside files.
func pathsQuestion(p PieceState) string {
	total := len(p.Outside) + p.OutsideMore
	first := shownNames(p.Outside[:min(len(p.Outside), questionShown)])
	return fmt.Sprintf("%s changed %d files outside its paths: %s and %d more",
		p.ID, total, strings.Join(first, ", "), total-len(first))
}

// outsideNote is what a station is told when the piece goes back for this.
func outsideNote(p PieceState) string {
	shown := p.Outside[:min(len(p.Outside), maxOutsideShown)]
	more := len(p.Outside) - len(shown) + p.OutsideMore
	shown = shownNames(shown)
	return fmt.Sprintf("These files are outside this piece's paths: %s (and %d more). Undo those changes or move the work into the piece's paths.",
		strings.Join(shown, ", "), more)
}

// withNote puts the outside-files note before the sender's own.
func withNote(p PieceState, note string) string {
	if len(p.Outside) == 0 && p.OutsideMore == 0 {
		return note
	}
	return strings.TrimSpace(outsideNote(p) + " " + note)
}

// gotoNote is the note of a goto the orchestrator chose.
func (a *applier) gotoNote(note string) string {
	if a.failure != reasonOutOfPaths {
		return note
	}
	return withNote(*a.ps(a.piece), note)
}

// escalationWait is the kind and question of the wait an escalation opens.
func (a *applier) escalationWait(question string) (string, string) {
	if a.failure != reasonOutOfPaths {
		return WaitQuestion, question
	}
	return WaitPaths, pathsQuestion(*a.ps(a.piece))
}

// ForPaths adapts a summary to a piece stopped for files outside its paths.
func (s Summary) ForPaths(ps PieceState) Summary {
	s.Failure, s.Allowed = reasonOutOfPaths, allowedFor(reasonOutOfPaths)
	s.Reason = "the piece changed files outside its paths"
	s.Outside, s.OutsideMore = slices.Clone(ps.Outside), ps.OutsideMore
	return s
}

func outsideText(s Summary) string {
	t := strings.Join(shownNames(s.Outside), "\n")
	if s.OutsideMore > 0 {
		t += fmt.Sprintf("\nand %d more", s.OutsideMore)
	}
	return t
}

// pathsChoices are the answers to a paths wait: files can't be accepted
// unseen, so a list that was cut short can't be accepted.
func pathsChoices(p PieceState) []string {
	if p.OutsideMore > 0 {
		return []string{answerGoto, answerCancel}
	}
	return []string{answerAccept, answerGoto, answerCancel}
}

// answerPaths applies an accept or goto answer to a paths wait. done is false
// for any other answer, which is handled as usual.
func answerPaths(d *decider, id string, a Answer) (step *RunStep, done bool) {
	p := d.ps(id)
	switch a.Do {
	case answerAccept:
		n := len(p.Outside)
		for _, f := range p.Outside {
			if !slices.Contains(p.Allowed, f) {
				p.Allowed = append(p.Allowed, f)
			}
		}
		p.Outside = nil
		d.add(Record{Event: Event{Time: d.o.Now, Kind: EvDecision, Piece: id, By: principalBy,
			Msg: fmt.Sprintf("accepted %d files outside the paths", n)}})
		return nil, true
	case answerGoto:
		return &RunStep{Piece: id, Kind: "goto", Station: a.Station, Note: withNote(*p, a.Note)}, true
	}
	return nil, false
}

// shownName is a file name made fit to show: git allows any bytes in a name,
// and an agent controls them. Characters that safeName refuses become "?" and
// the name is cut. The name kept in state is not changed.
func shownName(name string) string {
	clean := strings.Map(func(r rune) rune {
		if r == utf8.RuneError || unsafeRune(r) {
			return '?'
		}
		return r
	}, name)
	return cutRunes(clean, maxNameShown)
}

func shownNames(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = shownName(n)
	}
	return out
}

// ShownName is shownName for code outside the package: file names from a
// piece's worktree are cleaned the same way wherever they are shown.
func ShownName(name string) string { return shownName(name) }
