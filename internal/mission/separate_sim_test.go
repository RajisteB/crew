package mission

import (
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/source"
)

// planSim grows a mission the way crew does: a plan, then delegated pieces
// split by ApplySubPlan, top-level pieces appended later, and pieces starting
// in between, each change separated against what has started.
type planSim struct {
	r   *rand.Rand
	in  CheckInput
	m   Mission
	s   State
	seq int
}

var simPool = []string{"a/**", "a/x/**", "a/y/**", "a/x/1/**", "b/**", "b/x/**", "c/**", "a/x", "b/*.md"}

func simInput() CheckInput {
	station := source.Station{Agent: "a"}
	fac := func(name string) source.Factory {
		return source.Factory{Name: name, Start: "build", Harness: "claude-code", Stations: map[string]source.Station{"build": station}}
	}
	return CheckInput{
		Domains: []source.Domain{
			{Name: "d1", Factories: []string{"f1"}, Paths: []string{"a/**"}},
			{Name: "d2", Factories: []string{"f2"}},
			{Name: "d3", Factories: []string{"f3"}, Paths: []string{"b/**", "c/**"}},
		},
		Factories:           []source.Factory{fac("f1"), fac("f2"), fac("f3")},
		Registry:            &config.Registry{Harnesses: map[string]config.Harness{"claude-code": okHarness()}},
		OrchestratorHarness: "claude-code",
		Limits:              Limits{Pieces: lim(16), Depth: lim(2), Additions: lim(4), OrchestratorCalls: lim(3)},
	}
}

func (x *planSim) eff(p Piece) []string {
	d, _ := findDomain(x.in.Domains, p.Domain)
	return EffectivePaths(p, d)
}

func (x *planSim) randPaths() []string {
	switch x.r.IntN(4) {
	case 0:
		return nil // the domain's paths
	case 1:
		return []string{simPool[x.r.IntN(len(simPool))], simPool[x.r.IntN(len(simPool))]}
	}
	return []string{simPool[x.r.IntN(len(simPool))]}
}

// topPiece is a top-level piece that may wait for any piece already planned.
func (x *planSim) topPiece(id string) Piece {
	d := x.in.Domains[x.r.IntN(len(x.in.Domains))]
	p := Piece{ID: id, Domain: d.Name, Brief: "b " + id, Paths: x.randPaths()}
	if x.r.IntN(3) == 0 {
		p.Delegate = true
	} else {
		p.Factory = d.Factories[0]
	}
	for _, e := range x.m.Pieces {
		if x.r.IntN(5) == 0 {
			p.WaitsFor = append(p.WaitsFor, e.ID)
		}
	}
	return p
}

// narrow is paths inside the parent's effective paths, most of the time.
func (x *planSim) narrow(parent []string) []string {
	if len(parent) == 0 || x.r.IntN(8) == 0 {
		return x.randPaths()
	}
	if x.r.IntN(4) == 0 {
		return nil // the parent's paths
	}
	base := parent[x.r.IntN(len(parent))]
	if pre, ok := strings.CutSuffix(base, "/**"); ok && x.r.IntN(3) > 0 {
		return []string{pre + "/" + []string{"x", "y", "z"}[x.r.IntN(3)] + "/**"}
	}
	return []string{base}
}

// split plans the delegated piece id, as the scoped orchestrator would.
func (x *planSim) split(id string) {
	parent, _ := x.m.Piece(id)
	d, _ := findDomain(x.in.Domains, parent.Domain)
	sp := SubPlan{Reason: "split"}
	for i := range 1 + x.r.IntN(3) {
		p := Piece{ID: "s" + strconv.Itoa(i), Brief: "sub", Paths: x.narrow(x.eff(parent))}
		if Allows(x.in.Limits.Depth, Level(id)+1) && x.r.IntN(4) == 0 {
			p.Delegate = true
		} else {
			p.Factory = d.Factories[0]
		}
		for j := range i {
			if x.r.IntN(3) == 0 {
				p.WaitsFor = append(p.WaitsFor, "s"+strconv.Itoa(j))
			}
		}
		sp.Pieces = append(sp.Pieces, p)
	}
	x.m, x.s, _ = ApplySubPlan(x.m, x.s, id, sp, x.in, planNow)
}

// appendTop adds a top-level piece afterwards, as an amendment or an add does.
func (x *planSim) appendTop() {
	x.seq++
	next := Mission{Request: x.m.Request, Pieces: append(x.m.Pieces[:len(x.m.Pieces):len(x.m.Pieces)], x.topPiece("N"+strconv.Itoa(x.seq))), Changes: x.m.Changes}
	if len(Check(next, x.in)) > 0 {
		return
	}
	x.m, _, _ = SeparatedWithLog(x.m, next, x.in.Domains, StartedPieces(x.s), planNow)
	x.s.Pieces = append(x.s.Pieces, PieceState{ID: next.Pieces[len(next.Pieces)-1].ID, Status: PieceQueued})
}

// step splits a queued delegated piece, appends a piece, or starts one.
func (x *planSim) step() {
	var queued, delegated []string
	for _, p := range x.s.Pieces {
		if mp, _ := x.m.Piece(p.ID); p.Status == PieceQueued && mp.Delegate {
			delegated = append(delegated, p.ID)
		} else if p.Status == PieceQueued {
			queued = append(queued, p.ID)
		}
	}
	switch k := x.r.IntN(3); {
	case k == 0 && len(delegated) > 0:
		x.split(delegated[x.r.IntN(len(delegated))])
	case k == 1:
		x.appendTop()
	case len(queued) > 0:
		x.s = Started(x.s, queued[x.r.IntN(len(queued))], "r", "b", "/wt")
	}
}

// unordered names a pair of overlapping pieces that hold paths, neither
// delegated, that nothing orders, unless both have a started ancestor-or-self.
func (x *planSim) unordered() string {
	started := StartedPieces(x.s)
	settledUp := func(id string) bool {
		for _, a := range append([]string{id}, Ancestors(id)...) {
			if started[a] {
				return true
			}
		}
		return false
	}
	ps := x.m.Pieces
	for i, a := range ps {
		for _, b := range ps[i+1:] {
			if a.Delegate || b.Delegate || (settledUp(a.ID) && settledUp(b.ID)) {
				continue
			}
			if _, _, ok := PathsOverlap(x.eff(a), x.eff(b)); ok && !reachable(ps, a.ID, b.ID) && !reachable(ps, b.ID, a.ID) {
				return a.ID + " and " + b.ID
			}
		}
	}
	return ""
}

func (x *planSim) dump() string {
	var b strings.Builder
	for _, p := range x.m.Pieces {
		ps, _ := x.s.Piece(p.ID)
		fmt.Fprintf(&b, "  %-8s %-9s %s del=%v paths=%v waits=%v apart=%v\n", p.ID, ps.Status, p.Domain, p.Delegate, p.Paths, p.WaitsFor, p.Apart)
	}
	return b.String()
}

func TestSeparateOverlaps_NeverMakesACircleWithSubPieces(t *testing.T) {
	rng := rand.New(rand.NewPCG(7, 1234))
	for round := range 400 {
		x := &planSim{r: rng, in: simInput()}
		x.m = Mission{Request: "the job"}
		for i := range 2 + rng.IntN(4) {
			x.m.Pieces = append(x.m.Pieces, x.topPiece("P"+strconv.Itoa(i+1)))
		}
		if len(CheckNew(x.m, x.in)) > 0 {
			continue
		}
		x.m, _, _ = SeparatedWithLog(Mission{}, x.m, x.in.Domains, nil, planNow)
		x.s = NewState("m-1", x.m, gh.RepoInfo{Slug: "acme/app", DefaultBranch: "main"})
		for step := range 25 {
			x.step()
			if errs := Check(x.m, x.in); len(errs) > 0 {
				t.Fatalf("round %d step %d: the mission fails Check: %v\n%s", round, step, errs, x.dump())
			}
			if hasCircle(x.m.Pieces) {
				t.Fatalf("round %d step %d: a circle\n%s", round, step, x.dump())
			}
			if pair := x.unordered(); pair != "" {
				t.Fatalf("round %d step %d: %s overlap and nothing orders them\n%s", round, step, pair, x.dump())
			}
		}
	}
}
