package mission

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

// callLog records every fake call in order, so tests can assert sequences.
type callLog struct{ calls []string }

func (l *callLog) add(format string, args ...any) {
	l.calls = append(l.calls, fmt.Sprintf(format, args...))
}

// count is the number of calls starting with prefix.
func (l *callLog) count(prefix string) int {
	n := 0
	for _, c := range l.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

// indexes are the positions of the calls starting with prefix.
func (l *callLog) indexes(prefix string) []int {
	var out []int
	for i, c := range l.calls {
		if strings.HasPrefix(c, prefix) {
			out = append(out, i)
		}
	}
	return out
}

func (l *callLog) has(prefix string) bool { return l.count(prefix) > 0 }

// failures makes named operations fail a number of times.
type failures struct{ left map[string]int }

func (f *failures) failNext(op string, times int) {
	if f.left == nil {
		f.left = map[string]int{}
	}
	f.left[op] = times
}

func (f *failures) hit(op string) error {
	if f.left[op] > 0 {
		f.left[op]--
		return errors.New(op + " failed")
	}
	return nil
}

type fakeGit struct {
	log   *callLog
	fail  *failures
	dirty []string
	ahead int
}

func (g *fakeGit) Fetch(branch string) error {
	g.log.add("Fetch %s", branch)
	return g.fail.hit("Fetch")
}

func (g *fakeGit) AddWorktree(path, branch, from string) error {
	g.log.add("AddWorktree %s %s %s", path, branch, from)
	return g.fail.hit("AddWorktree")
}

func (g *fakeGit) RemoveWorktree(path string, force bool) error {
	g.log.add("RemoveWorktree %s force=%v", path, force)
	return g.fail.hit("RemoveWorktree")
}

func (g *fakeGit) Dirty(worktree string) ([]string, error) {
	g.log.add("Dirty %s", worktree)
	return slices.Clone(g.dirty), g.fail.hit("Dirty")
}

func (g *fakeGit) Ahead(worktree, base string) (int, error) {
	g.log.add("Ahead %s %s", worktree, base)
	return g.ahead, g.fail.hit("Ahead")
}

func (g *fakeGit) Push(worktree, branch string) error {
	g.log.add("Push %s %s", worktree, branch)
	return g.fail.hit("Push")
}

type fakeGH struct {
	log      *callLog
	fail     *failures
	prs      map[int]gh.PR
	existing map[string]gh.PR // by head branch, for FindPR
	next     int
}

func (g *fakeGH) CreatePR(_ context.Context, head, base, title, body string) (gh.PR, error) {
	g.log.add("CreatePR %s %s", head, base)
	if err := g.fail.hit("CreatePR"); err != nil {
		return gh.PR{}, err
	}
	g.next++
	pr := gh.PR{Number: g.next, State: "OPEN", URL: fmt.Sprintf("https://github.com/acme/app/pull/%d", g.next)}
	g.prs[pr.Number] = pr
	return pr, nil
}

func (g *fakeGH) PRState(_ context.Context, number int) (gh.PR, error) {
	g.log.add("PRState %d", number)
	if err := g.fail.hit("PRState"); err != nil {
		return gh.PR{}, err
	}
	return g.prs[number], nil
}

func (g *fakeGH) FindPR(_ context.Context, head string) (gh.PR, bool, error) {
	g.log.add("FindPR %s", head)
	if err := g.fail.hit("FindPR"); err != nil {
		return gh.PR{}, false, err
	}
	pr, ok := g.existing[head]
	return pr, ok, nil
}

func (g *fakeGH) setState(number int, state string) {
	pr := g.prs[number]
	pr.State = state
	g.prs[number] = pr
}

type fakeRuns struct {
	log       *callLog
	fail      *failures
	obs       map[string]RunObs
	summary   run.State
	factory   source.Factory
	last      run.StepRecord
	steps     []RunStep
	onObserve func()
	onDrive   func()
}

func (r *fakeRuns) Start(p Piece, input, worktree string) (string, error) {
	r.log.add("Start %s %s", p.ID, worktree)
	if err := r.fail.hit("Start"); err != nil {
		return "", err
	}
	return "run-" + p.ID, nil
}

func (r *fakeRuns) Observe(runID string) (RunObs, error) {
	r.log.add("Observe %s", runID)
	if r.onObserve != nil {
		r.onObserve()
	}
	if err := r.fail.hit("Observe"); err != nil {
		return RunObs{}, err
	}
	if ro, ok := r.obs[runID]; ok {
		return ro, nil
	}
	return RunObs{State: run.State{Status: run.StatusRunning}, Live: true}, nil
}

func (r *fakeRuns) Drive(_ context.Context, runID, worktree string) error {
	r.log.add("Drive %s", runID)
	if r.onDrive != nil {
		r.onDrive()
	}
	return r.fail.hit("Drive")
}

func (r *fakeRuns) Step(_ context.Context, runID, worktree string, st RunStep) error {
	r.log.add("Step %s %s %s", runID, st.Kind, st.Station)
	if err := r.fail.hit("Step"); err != nil {
		return err
	}
	r.steps = append(r.steps, st)
	return nil
}

func (r *fakeRuns) Stop(runID string) error {
	r.log.add("Stop %s", runID)
	return r.fail.hit("Stop")
}

func (r *fakeRuns) Summary(runID string) (run.State, source.Factory, run.StepRecord, string, error) {
	r.log.add("Summary %s", runID)
	if err := r.fail.hit("Summary"); err != nil {
		return run.State{}, source.Factory{}, run.StepRecord{}, "", err
	}
	return r.summary, r.factory, r.last, "/runs/" + runID + "/steps", nil
}

func (r *fakeRuns) setRun(runID string, rs run.State, live bool) {
	r.obs[runID] = RunObs{State: rs, Live: live}
}

type fakeOrch struct {
	log       *callLog
	decisions []OrchDecision
	err       error
	prompts   []string
}

func (o *fakeOrch) Decide(_ context.Context, _ string, s Summary) (OrchDecision, error) {
	o.log.add("Orch.Decide %s", s.Piece.ID)
	o.prompts = append(o.prompts, s.Prompt())
	if o.err != nil {
		return OrchDecision{}, o.err
	}
	if len(o.decisions) == 0 {
		return OrchDecision{}, errors.New("no decision queued")
	}
	d := o.decisions[0]
	o.decisions = o.decisions[1:]
	return d, nil
}

type fakeNotifier struct {
	log  *callLog
	fail error
	sent []string
}

func (n *fakeNotifier) Notify(title, body string) error {
	n.log.add("Notify %s", body)
	if n.fail != nil {
		return n.fail
	}
	n.sent = append(n.sent, title+": "+body)
	return nil
}

// harness is an Engine over fakes and a real mission folder in a temp dir.
type harness struct {
	t     *testing.T
	e     *Engine
	log   *callLog
	fail  *failures
	git   *fakeGit
	gh    *fakeGH
	runs  *fakeRuns
	orch  *fakeOrch
	notif *fakeNotifier
	store Store
	id    string
}

func twoPieces() []Piece {
	return []Piece{
		{ID: "P1", Domain: "product", Factory: "feature", Brief: "credits API\nmore detail"},
		{ID: "P2", Domain: "product", Factory: "feature", Brief: "invite screen", WaitsFor: []string{"P1"}},
	}
}

func newHarness(t *testing.T, pieces ...Piece) *harness {
	t.Helper()
	if len(pieces) == 0 {
		pieces = twoPieces()
	}
	root := t.TempDir()
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

func (h *harness) tick() State {
	h.t.Helper()
	st, err := h.e.Tick(context.Background())
	if err != nil {
		h.t.Fatalf("tick: %v", err)
	}
	return st
}

func (h *harness) state() State {
	h.t.Helper()
	st, err := h.store.LoadState()
	if err != nil {
		h.t.Fatal(err)
	}
	return st
}

func (h *harness) piece(id string) PieceState {
	h.t.Helper()
	p, ok := h.state().Piece(id)
	if !ok {
		h.t.Fatalf("no piece %s", id)
	}
	return p
}

// edit changes the saved state directly.
func (h *harness) edit(fn func(*State)) {
	h.t.Helper()
	st := h.state()
	fn(&st)
	if err := h.store.SaveState(st); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) kinds() []string {
	h.t.Helper()
	evs, err := h.store.EventsSince(0)
	if err != nil {
		h.t.Fatal(err)
	}
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func (h *harness) events(kind string) []Event {
	h.t.Helper()
	evs, _ := h.store.EventsSince(0)
	var out []Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// runPiece puts a piece in the running state with run id run-<id>.
func (h *harness) runPiece(id string) string {
	h.t.Helper()
	runID := "run-" + id
	wt := h.e.RepoRoot + "/.crew/worktrees/" + h.id + "-" + id
	h.edit(func(s *State) {
		for i := range s.Pieces {
			if s.Pieces[i].ID == id {
				s.Pieces[i] = PieceState{ID: id, Status: PieceRunning, RunID: runID,
					Branch: "crew/" + h.id + "/" + id, Worktree: wt}
			}
		}
	})
	return runID
}

func doneState() run.State { return run.State{Status: run.StatusDone} }

func sourceDomain(body string) source.Domain { return source.Domain{Name: "product", Body: body} }
