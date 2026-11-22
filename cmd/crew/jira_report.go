package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/crewfile-dev/crew/internal/jira"
)

// reporter prints the Jira report and counts its problems.
type reporter struct {
	out        io.Writer
	problems   int
	sprintless bool // the board has no sprints (a Kanban board)
}

func (p *reporter) line(format string, args ...any) { fmt.Fprintf(p.out, "  "+format+"\n", args...) }

func (p *reporter) problem(format string, args ...any) {
	p.problems++
	p.line("problem: "+format, args...)
}

// callProblem reports a failed call: the credentials are rejected, or the
// site can't be used. The text never holds a credential.
func (p *reporter) callProblem(what string, err error) {
	var je *jira.Error
	if errors.As(err, &je) && (je.Status == 401 || je.Status == 403) {
		p.problem("the site rejects the credentials (HTTP %d)", je.Status)
		return
	}
	p.problem("can't %s: %v", what, err)
}

// jiraReport checks one repo's Jira settings against the site and prints what
// it finds. It returns how many problems it found, and whether the board has
// no sprints at all.
func jiraReport(ctx context.Context, c *jira.Client, slug string, r jira.Repo, out io.Writer) (problems int, sprintless bool) {
	p := &reporter{out: out}
	fmt.Fprintf(out, "Jira for %s: project %s on %s\n", slug, r.Project, r.Site)
	_, name, err := c.Myself(ctx)
	if err != nil {
		p.callProblem("read the account", err)
		return p.problems, false
	}
	p.line("account: %s", name)
	if !p.workflow(ctx, c, r) {
		return p.problems, false
	}
	p.boardLine(ctx, c, r)
	p.flagLine(ctx, c)
	return p.problems, p.sprintless
}

// workflow prints the issue types and statuses and checks them. It reports
// false when the project could not be read at all.
func (p *reporter) workflow(ctx context.Context, c *jira.Client, r jira.Repo) bool {
	byType, err := c.Statuses(ctx, r.Project)
	var je *jira.Error
	if errors.As(err, &je) && je.Status == 404 {
		p.problem("the project %s isn't found", r.Project)
		return false
	}
	if err != nil {
		p.callProblem("read the project", err)
		return false
	}
	s := r.Statuses
	p.line("issue types: %s, %s", r.IssueType, r.EpicType)
	p.line("statuses: %s, %s, %s, %s", s.ToDo, s.InProgress, s.InReview, s.Done)
	p.checkType(r, byType, "issue type", r.IssueType, true)
	if r.EpicType != r.IssueType {
		p.checkType(r, byType, "epic type", r.EpicType, false)
	}
	return true
}

type statusWant struct{ key, name string }

// checkType checks that an issue type exists and has the statuses crew moves
// it through; the epic type needs no in-review status.
func (p *reporter) checkType(r jira.Repo, byType map[string][]string, what, typ string, review bool) {
	have, ok := byType[typ]
	if !ok {
		p.problem("the %s %q doesn't exist in %s", what, typ, r.Project)
		return
	}
	s := r.Statuses
	want := []statusWant{{"to_do", s.ToDo}, {"in_progress", s.InProgress}, {"in_review", s.InReview}, {"done", s.Done}}
	if !review {
		want = slices.DeleteFunc(want, func(w statusWant) bool { return w.key == "in_review" })
	}
	for _, w := range want {
		if !slices.ContainsFunc(have, func(h string) bool { return strings.EqualFold(h, w.name) }) {
			p.problem("status %q is not in %s's workflow for %s; add it in Jira or set statuses.%s in jira.yaml", w.name, r.Project, typ, w.key)
		}
	}
}

func (p *reporter) boardLine(ctx context.Context, c *jira.Client, r jira.Repo) {
	if r.Board == 0 {
		p.line("board: none: issues are not put in a sprint")
		return
	}
	_, name, found, err := c.ActiveSprint(ctx, r.Board)
	var je *jira.Error
	switch {
	case errors.As(err, &je) && je.Status == 404:
		p.problem("the board %d isn't found", r.Board)
	case errors.As(err, &je) && je.Status == 400: // Jira Cloud's answer for a board with no sprints
		p.sprintless = true
		p.line("board: %d has no sprints: issues are not put in a sprint", r.Board)
	case err != nil:
		p.callProblem("read the board", err)
	case found:
		p.line("board: %d, active sprint %q", r.Board, name)
	default:
		p.line("board: %d, no active sprint: new issues go to the backlog and won't show on the board until you start a sprint", r.Board)
	}
}

func (p *reporter) flagLine(ctx context.Context, c *jira.Client) {
	_, found, err := c.FlagField(ctx)
	switch {
	case err != nil:
		p.callProblem("read the fields", err)
	case found:
		p.line("flag: the Flagged field is there")
	default:
		p.line("flag: no Flagged field: a waiting piece gets a comment but no flag")
	}
}
