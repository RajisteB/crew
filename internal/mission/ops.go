package mission

import (
	"context"
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

const (
	maxTitleRunes   = 70
	maxPRNoteRunes  = 1000
	inputCommitRule = "Commit your work on this branch. Don't push, and don't open or merge a pull request."
)

// GitOps is the git side of the engine. gitx.Repo satisfies it.
type GitOps interface {
	Fetch(branch string) error
	AddWorktree(path, branch, from string) error
	RemoveWorktree(path string, force bool) error
	Dirty(worktree string) ([]string, error)
	Ahead(worktree, base string) (int, error)
	Push(worktree, branch string) error
}

// GHOps is the GitHub side of the engine. gh.Client satisfies it.
type GHOps interface {
	CreatePR(ctx context.Context, head, base, title, body string) (gh.PR, error)
	PRState(ctx context.Context, number int) (gh.PR, error)
	FindPR(ctx context.Context, head string) (gh.PR, bool, error)
}

// RunOps is the factory-run side of the engine.
type RunOps interface {
	Start(p Piece, input, worktree string) (runID string, err error)
	Observe(runID string) (RunObs, error)
	// Drive starts driving the run; it returns nil when a driver is already live.
	Drive(ctx context.Context, runID, worktree string) error
	Step(ctx context.Context, runID, worktree string, st RunStep) error
	Stop(runID string) error
	// Summary returns the run's state, its factory, its last step and the
	// folder holding every step's output.
	Summary(runID string) (run.State, source.Factory, run.StepRecord, string, error)
}

// Orchestrator is the headless agent that decides what happens to a stuck piece.
type Orchestrator interface {
	Decide(ctx context.Context, repoRoot string, s Summary) (OrchDecision, error)
}

// PieceInput is the input a piece's run starts with: the piece's brief, the
// whole request for context, the domain briefing and the commit rule.
func PieceInput(id string, m Mission, p Piece, d source.Domain) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Your piece: %s (mission %s)\n\n%s\n\n", p.ID, id, strings.TrimSpace(p.Brief))
	fmt.Fprintf(&b, "## The whole request (for context; do only your piece)\n\n%s\n\n", m.Request)
	fmt.Fprintf(&b, "## Domain briefing: %s\n\n%s\n\n", p.Domain, strings.TrimSpace(d.Body))
	fmt.Fprintf(&b, "## Commit\n\n%s\n", inputCommitRule)
	return b.String()
}

// PRText is the title and body of a piece's pull request. They may be
// public, so they carry names, the brief's first line, the request and the
// last step's summary, nothing else.
func PRText(id string, m Mission, p Piece, last run.StepRecord) (title, body string) {
	title = cutRunes(firstLine(p.Brief), maxTitleRunes)
	if title == "" {
		title = p.ID
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Mission: %s\nPiece: %s\nDomain: %s\nFactory: %s\n\nRequest:\n\n", id, p.ID, p.Domain, p.Factory)
	for _, line := range strings.Split(strings.TrimSpace(m.Request), "\n") {
		b.WriteString(strings.TrimRight("> "+line, " ") + "\n")
	}
	if s := strings.TrimSpace(last.Verdict.Summary); s != "" {
		fmt.Fprintf(&b, "\nLast step summary:\n\n%s\n", cutRunes(s, maxPRNoteRunes))
	}
	return title, b.String()
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
