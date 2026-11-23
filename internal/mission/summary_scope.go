package mission

import (
	"fmt"
	"slices"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// Scope is the delegated piece a scoped orchestrator answers for.
type Scope struct {
	Parent   Piece
	Above    []Piece  // the delegated pieces above Parent, nearest first
	Paths    []string // Parent's effective paths
	Briefing string   // the domain's body
}

// Scoped returns s for the orchestrator scoped to the delegated piece parent.
// s is not modified.
func (s Summary) Scoped(m Mission, parent string, d source.Domain) Summary {
	p, _ := m.Piece(parent)
	sc := &Scope{Parent: p, Paths: slices.Clone(EffectivePaths(p, d)), Briefing: d.Body}
	for _, id := range Ancestors(parent) {
		if above, ok := m.Piece(id); ok {
			sc.Above = append(sc.Above, above)
		}
	}
	s.Scope = sc
	return s
}

const (
	scopedIntro = `You are the orchestrator for piece %[1]s (the %[2]s domain) of a crew mission. %[1]s was handed to you whole and split into pieces; each is one factory run in its own git worktree. One of them is stuck, and you decide what happens next.

Your reply is one decision. The actions you may take:
`
	scopedRepeatRule = `An action this piece already got for the same kind of stop (and, for goto, the same station) is listed under "Earlier decisions on this piece" in the data. Repeating it is refused and the question goes up, so choose escalate rather than repeat. A decision that can't be used goes up too.`
	scopedHelp       = `Fields by action: retry {reason}; goto {station, note, reason}; cancel {reason};
rebrief {target, brief, reason}; add {piece {domain, factory, brief, paths, waits_for}, quote, reason}
where quote is words copied from the request; escalate {note, reason} where note is your question for the level above.`
)

func (s Summary) introText() string {
	if s.Scope == nil {
		return taskIntro
	}
	return fmt.Sprintf(scopedIntro, s.Scope.Parent.ID, s.Scope.Parent.Domain)
}

func (s Summary) repeatText() string {
	if s.Scope == nil {
		return repeatRule
	}
	return scopedRepeatRule
}

func (s Summary) helpText() string {
	if s.Scope == nil {
		return actionHelp
	}
	return scopedHelp
}

// meaningOf says what an action does for this summary's orchestrator.
func (s Summary) meaningOf(action string) (string, bool) {
	if s.Scope != nil {
		id := s.Scope.Parent.ID
		switch action {
		case "escalate":
			return "pass it up, to the orchestrator above you and then the principal; put the question in the note.", true
		case "rebrief":
			return "rewrite the brief of a piece under " + id + " that hasn't started.", true
		case "add":
			return "add a piece under " + id + ", quoting the words of the request it serves; crew names it.", true
		}
	}
	meaning, ok := actionMeaning[action]
	return meaning, ok
}

// scopeSections writes the data a scoped orchestrator has beyond the main one's.
func (s Summary) scopeSections(b *strings.Builder) {
	sc := s.Scope
	if sc == nil {
		return
	}
	section(b, "Your scope: "+sc.Parent.ID, fmt.Sprintf("brief: %s\npaths: %s", sc.Parent.Brief, strings.Join(sc.Paths, ", ")))
	if len(sc.Above) > 0 {
		lines := make([]string, len(sc.Above))
		for i, p := range sc.Above {
			lines[i] = fmt.Sprintf("%s: %s", p.ID, p.Brief)
		}
		section(b, "Above "+sc.Parent.ID, strings.Join(lines, "\n"))
	}
	section(b, "Domain briefing", sc.Briefing)
}
