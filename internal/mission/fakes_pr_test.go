package mission

import (
	"context"
	"slices"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

func (g *fakeGit) Head(_ context.Context, worktree string) (string, error) {
	g.log.add("Head %s", worktree)
	if err := g.fail.hit("Head"); err != nil {
		return "", err
	}
	if g.head == "" {
		return "h0", nil
	}
	return g.head, nil
}

func (g *fakeGit) ChangedFiles(_ context.Context, worktree, base string) ([]string, error) {
	g.log.add("ChangedFiles %s %s", worktree, base)
	return slices.Clone(g.changed), g.fail.hit("ChangedFiles")
}

func (g *fakeGit) MergeInto(_ context.Context, worktree, from string) ([]string, error) {
	g.log.add("MergeInto %s %s", worktree, from)
	if err := g.fail.hit("MergeInto"); err != nil {
		return nil, err
	}
	if len(g.conflicts) == 0 && g.mergedHead != "" {
		g.head = g.mergedHead
	}
	return slices.Clone(g.conflicts), nil
}

func (g *fakeGH) MergePR(_ context.Context, number int, method, headSHA string) error {
	g.log.add("MergePR %d %s %s", number, method, headSHA)
	return g.fail.hit("MergePR")
}

func (g *fakeGH) FailedLog(_ context.Context, check gh.Check) (string, error) {
	g.log.add("FailedLog %s", check.Name)
	if err := g.fail.hit("FailedLog"); err != nil {
		return "", err
	}
	return g.logs[check.Name], nil
}

type switchRead struct {
	recs map[string]principal.SwitchRecord
	err  error
}

// fakeSwitches answers each read from reads in order, then repeats the last.
type fakeSwitches struct {
	log   *callLog
	reads []switchRead
	n     int
}

func (f *fakeSwitches) Switches(repo string) (map[string]principal.SwitchRecord, error) {
	f.log.add("Switches %s", repo)
	r := f.reads[min(f.n, len(f.reads)-1)]
	f.n++
	return r.recs, r.err
}

func modes(domainModes map[string]principal.AutoMode) switchRead {
	recs := map[string]principal.SwitchRecord{}
	for d, m := range domainModes {
		recs[d] = principal.SwitchRecord{Repo: "acme/app", Domain: d, Mode: m}
	}
	return switchRead{recs: recs}
}
