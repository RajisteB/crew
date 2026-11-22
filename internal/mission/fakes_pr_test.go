package mission

import (
	"context"
	"errors"
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
	if f, ok := g.at[worktree]; ok {
		return slices.Clone(f.changed), g.fail.hit("ChangedFiles")
	}
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
	if err := g.fail.hit("MergePR"); err != nil {
		return err
	}
	g.setState(number, "MERGED")
	return nil
}

// queueGH accepts a merge without merging, as a merge queue does; with
// stateErr set it also can't read the pull request after the merge call.
type queueGH struct {
	*fakeGH
	stateErr bool
	merged   bool
}

func (g *queueGH) MergePR(_ context.Context, number int, method, headSHA string) error {
	g.log.add("MergePR %d %s %s", number, method, headSHA)
	g.merged = true
	return nil
}

func (g *queueGH) PRState(ctx context.Context, number int) (gh.PR, error) {
	if g.stateErr && g.merged {
		g.log.add("PRState %d", number)
		return gh.PR{}, errors.New("PRState failed")
	}
	return g.fakeGH.PRState(ctx, number)
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
