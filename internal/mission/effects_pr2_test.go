package mission

import (
	"context"
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/source"
)

// seqGit answers ChangedFiles from lists, one per call, then repeats the last.
type seqGit struct {
	*fakeGit
	lists [][]string
	n     int
}

func (g *seqGit) ChangedFiles(ctx context.Context, worktree, base string) ([]string, error) {
	_, err := g.fakeGit.ChangedFiles(ctx, worktree, base)
	list := g.lists[min(g.n, len(g.lists)-1)]
	g.n++
	return slices.Clone(list), err
}

func TestEngine_MergeSecondReadSeesBlocker(t *testing.T) {
	h := prHarness(t, principal.AutoOn, passing())
	h.e.Domains["design"] = source.Domain{Name: "design", Paths: []string{"brand/**"}}
	sg := &seqGit{fakeGit: h.git, lists: [][]string{{"app/x.go"}, {"app/x.go", "brand/logo.svg"}}}
	h.e.Git = sg
	h.tick()
	if sg.n != 2 {
		t.Fatalf("ChangedFiles read %d times, want 2", sg.n)
	}
	if h.log.has("MergePR") {
		t.Fatalf("merged: %v", h.log.calls)
	}
	if p := h.piece("P1"); p.Status != PiecePROpen {
		t.Fatalf("piece = %+v", p)
	}
}
