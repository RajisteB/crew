package mission

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

const (
	maxTitleRunes    = 70
	maxPRNoteRunes   = 1000
	deliverablesRule = "If your piece produces work that isn't a change to the repo (research, a brief, a plan), write it as files in .crew/deliverables/ in this directory. Git ignores that folder: don't commit those files and don't move them. crew collects them when you finish."
	inputCommitRule  = "Commit your work on this branch. Don't push, and don't open or merge a pull request."
)

// GitOps is the git side of the engine. gitx.Repo satisfies it.
type GitOps interface {
	Fetch(ctx context.Context, branch string) error
	AddWorktree(ctx context.Context, path, branch, from string) error
	RemoveWorktree(ctx context.Context, path string, force bool) error
	Dirty(ctx context.Context, worktree string) ([]string, error)
	Ahead(ctx context.Context, worktree, base string) (int, error)
	Push(ctx context.Context, worktree, branch string) error
	Head(ctx context.Context, worktree string) (string, error)
	ChangedFiles(ctx context.Context, worktree, base string) ([]string, error)
	MergeInto(ctx context.Context, worktree, from string) (conflicts []string, err error)
}

// GHOps is the GitHub side of the engine. gh.Client satisfies it.
type GHOps interface {
	CreatePR(ctx context.Context, head, base, title, body string) (gh.PR, error)
	PRState(ctx context.Context, number int) (gh.PR, error)
	FindPR(ctx context.Context, head string) (gh.PR, bool, error)
	MergePR(ctx context.Context, number int, method, headSHA string) error
	FailedLog(ctx context.Context, check gh.Check) (string, error)
}

// SwitchReader reads the auto-merge switches of a repo. principal.Store
// satisfies it.
type SwitchReader interface {
	Switches(repo string) (map[string]principal.SwitchRecord, error)
}

// Tracker is where the principal watches a mission. jira.Board satisfies it.
type Tracker interface {
	Create(ctx context.Context, kind, label, summary, body, parent string) (key string, err error)
	Move(ctx context.Context, key, lane string) error
	Comment(ctx context.Context, key, text string) error
	Flag(ctx context.Context, key string, on bool) error
	Attach(ctx context.Context, key, path string) error
	Done(ctx context.Context, key string) (bool, error)
	Sprint(ctx context.Context, keys []string) (placed bool, err error)
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
	// Plan splits a delegated piece into the pieces that do its work.
	Plan(ctx context.Context, repoRoot string, r PlanRequest) (SubPlan, error)
}

// PieceInput is the input a piece's run starts with: the piece's brief, the
// whole request for context, the domain briefing and the commit rule.
func PieceInput(id string, m Mission, p Piece, d source.Domain, earlier []Earlier) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Your piece: %s (mission %s)\n\n%s\n\n", p.ID, id, strings.TrimSpace(p.Brief))
	for _, a := range Ancestors(p.ID) {
		if ap, ok := m.Piece(a); ok {
			fmt.Fprintf(&b, "## The piece yours is part of: %s\n\n%s\n\n", a, strings.TrimSpace(ap.Brief))
		}
	}
	fmt.Fprintf(&b, "## The whole request (for context; do only your piece)\n\n%s\n\n", m.Request)
	if changes := m.PrincipalChanges(); len(changes) > 0 {
		fmt.Fprintf(&b, "## %s\n\n%s\n\n", principalChangesTitle, bulletList(changes))
	}
	fmt.Fprintf(&b, "## Domain briefing: %s\n\n%s\n\n", p.Domain, strings.TrimSpace(d.Body))
	if len(earlier) > 0 {
		fmt.Fprintf(&b, "## Deliverables from earlier pieces\n\n%s\n\n", earlierList(earlier))
	}
	fmt.Fprintf(&b, "## Deliverables\n\n%s\n\n", deliverablesRule)
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

// earlierList is one line per earlier piece, naming its files in the worktree.
func earlierList(earlier []Earlier) string {
	lines := make([]string, len(earlier))
	for i, e := range earlier {
		paths := make([]string, len(e.Files))
		for j, f := range e.Files {
			paths[j] = path.Join(run.CrewDir, scratchIn, e.Piece, f)
		}
		lines[i] = fmt.Sprintf("%s: %s", e.Piece, strings.Join(paths, ", "))
	}
	return bulletList(lines)
}

// bulletList is one "- " line per item.
func bulletList(items []string) string {
	lines := make([]string, len(items))
	for i, it := range items {
		lines[i] = "- " + strings.TrimSpace(it)
	}
	return strings.Join(lines, "\n")
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}
