package mission

import (
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/source"
)

// gitFacts is what fakeGit answers for one worktree.
type gitFacts struct {
	dirty   []string
	ahead   int
	changed []string
}

// newHarnessIn builds a mission in the repo root given, so two missions can
// share one repo. It holds the mission lock until the test ends, as a live
// engine does, so other missions count it as driven; see undrive.
func newHarnessIn(t *testing.T, root string, pieces ...Piece) *harness {
	t.Helper()
	if len(pieces) == 0 {
		pieces = twoPieces()
	}
	m := Mission{Request: "Launch referrals.\nPay $10 per friend.", Pieces: pieces}
	store, st, err := New(root, m, gh.RepoInfo{Slug: "acme/app", DefaultBranch: "main"}, testNow)
	if err != nil {
		t.Fatal(err)
	}
	st.Status = StatusRunning
	if err := store.SaveState(st); err != nil {
		t.Fatal(err)
	}
	log, fail := &callLog{}, &failures{}
	h := &harness{t: t, log: log, fail: fail, store: store, id: st.MissionID,
		git:   &fakeGit{log: log, fail: fail, ahead: 1},
		gh:    &fakeGH{log: log, fail: fail, prs: map[int]gh.PR{}, existing: map[string]gh.PR{}},
		runs:  &fakeRuns{log: log, fail: fail, obs: map[string]RunObs{}, factory: exFactory()},
		orch:  &fakeOrch{log: log},
		notif: &fakeNotifier{log: log},
	}
	release, err := store.Lock()
	if err != nil {
		t.Fatal(err)
	}
	h.release = release
	t.Cleanup(release)
	h.e = &Engine{
		Store: store, RepoRoot: root, Checks: exInput(),
		Settings: Settings{Version: 1, Approval: "off", Limits: Limits{
			Concurrency: lim(3), Depth: lim(1), Pieces: lim(10), Steps: lim(100),
			OrchestratorCalls: lim(3), Additions: lim(2)}},
		Domains: map[string]source.Domain{"product": {Name: "product", Body: "Product rules."}},
		Git:     h.git, GH: h.gh, Runs: h.runs, Orch: h.orch, Notifier: h.notif,
		Now: func() time.Time { return testNow },
	}
	return h
}
