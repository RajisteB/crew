package mission

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func delegatingInput(depth int) CheckInput {
	in := baseInput()
	in.OrchestratorHarness = "claude-code"
	in.Limits.Depth = lim(depth)
	in.Domains[2].Paths = []string{"design/**"}
	return in
}

// treePlan is acme() plus a delegated design piece P3 with two sub-pieces.
func treePlan() Mission {
	m := acme()
	m.Pieces = append(m.Pieces,
		Piece{ID: "P3", Domain: "design", Delegate: true, Brief: "the visuals", Paths: []string{"design/**"}},
		Piece{ID: "P3.1", Domain: "design", Factory: "visuals", Brief: "icons", Paths: []string{"design/icons/**"}},
		Piece{ID: "P3.2", Domain: "design", Factory: "visuals", Brief: "banner", WaitsFor: []string{"P3.1"}},
	)
	return m
}

func edited(edit func(*Mission)) Mission {
	m := treePlan()
	edit(&m)
	return m
}

func TestCheck_DelegatedPieceIsAccepted(t *testing.T) {
	if errs := Check(treePlan(), delegatingInput(2)); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}

func TestCheck_DelegationPastTheDepthLimit(t *testing.T) {
	wantError(t, Check(treePlan(), delegatingInput(0)), "piece P3: delegates at depth 1, and limits.delegation_depth is 0")
	m := edited(func(m *Mission) {
		m.Pieces[6] = Piece{ID: "P3.1", Domain: "design", Delegate: true, Brief: "b", Paths: []string{"design/b/**"}}
	})
	wantError(t, Check(m, delegatingInput(1)), "piece P3.1: delegates at depth 2, and limits.delegation_depth is 1")
	in := delegatingInput(0)
	in.Limits.Depth = source.Limit{Unlimited: true}
	if errs := Check(m, in); len(errs) != 0 {
		t.Fatalf("unlimited: %v", errs)
	}
}

func TestCheck_DelegationNeedsADenyingOrchestratorHarness(t *testing.T) {
	for _, name := range []string{"plain", "", "unknown"} {
		in := delegatingInput(2)
		in.OrchestratorHarness = name
		errs := Check(treePlan(), in)
		var found bool
		for _, err := range errs {
			if errors.Is(err, ErrNoDeny) && strings.Contains(err.Error(), "piece P3: the orchestrator's harness") &&
				strings.Contains(err.Error(), "can't apply deny rules, so nothing can be delegated") {
				found = true
			}
		}
		if !found {
			t.Errorf("harness %q: errs = %v", name, errs)
		}
	}
	in := baseInput() // no orchestrator harness, no delegated piece
	if errs := Check(acme(), in); len(errs) != 0 {
		t.Fatalf("plan without delegation: %v", errs)
	}
}

func TestCheck_SubPieceRules(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Mission)
		want string
	}{
		{"parent missing", func(m *Mission) { m.Pieces = append(m.Pieces[:5], m.Pieces[6:]...) },
			"piece P3.1: its parent P3 is not in the plan (to drop P3, remove the pieces under it too)"},
		{"parent not delegated", func(m *Mission) { m.Pieces[5].Delegate, m.Pieces[5].Factory = false, "visuals" },
			"piece P3.1: its parent P3 is not delegated"},
		{"domain", func(m *Mission) { m.Pieces[6].Domain = "product"; m.Pieces[6].Factory = "feature" },
			`piece P3.1: domain "product" is not its parent's domain "design"`},
		{"outside", func(m *Mission) { m.Pieces[6].Paths = []string{"site/**"} },
			`piece P3.1: paths: "site/**" is outside the paths of its parent P3`},
		{"waits outside", func(m *Mission) { m.Pieces[7].WaitsFor = []string{"P1"} },
			`piece P3.2: waits_for "P1", which is not under P3; a sub-piece waits only for pieces under its own parent`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantError(t, Check(edited(c.edit), delegatingInput(2)), c.want)
		})
	}
}

func TestCheck_SubPieceWithNoPathsUnderAParentWithPaths(t *testing.T) {
	in := delegatingInput(2)
	in.Domains[2].Paths = nil // P3.2 has no paths of its own and its domain has none
	wantError(t, Check(treePlan(), in), "piece P3.2: has no paths, and its parent P3 has; a sub-piece keeps inside its parent's paths")
}

func TestCheck_SubPieceUsesDomainPathsWhenParentHasNone(t *testing.T) {
	m := edited(func(m *Mission) { m.Pieces[5].Paths = nil })
	if errs := Check(m, delegatingInput(2)); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
	m.Pieces[7].Paths = nil // no paths of its own under a parent using its domain's
	if errs := Check(m, delegatingInput(2)); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}

func TestCheck_SubPieceIDForm(t *testing.T) {
	for _, id := range []string{"P3.0", "P3.x", "P3.", ".1", "P3..1", "P3.01"} {
		m := edited(func(m *Mission) { m.Pieces[6].ID = id })
		wantError(t, Check(m, delegatingInput(2)), "id must be")
	}
	m := edited(func(m *Mission) {
		m.Pieces[6].ID = "P3.1.2"
		m.Pieces[7].ID, m.Pieces[7].WaitsFor = "P3.3", nil
		m.Pieces = append(m.Pieces, Piece{ID: "P3.1", Domain: "design", Delegate: true, Brief: "b", Paths: []string{"design/icons/**"}})
	})
	for _, err := range Check(m, delegatingInput(3)) {
		if strings.Contains(err.Error(), "id must be") {
			t.Errorf("P3.1.2 refused: %v", err)
		}
	}
	m = edited(func(m *Mission) { m.Pieces[5].ID = "Epic" })
	wantError(t, Check(m, delegatingInput(2)), "is reserved")
}

func TestCheck_CircleThroughADelegatedPiece(t *testing.T) {
	// the ancestor edge: P3 waits for P5, P5 waits for P3.1, and P3.1 inherits P3's wait for P5
	m := edited(func(m *Mission) {
		m.Pieces[5].WaitsFor = []string{"P5"}
		m.Pieces[3].WaitsFor = []string{"P3.1"}
	})
	wantError(t, Check(m, delegatingInput(2)), "wait on each other")
	// a direct circle between a delegated piece and an outside piece
	m = edited(func(m *Mission) {
		m.Pieces[5].WaitsFor = []string{"P5"}
		m.Pieces[3].WaitsFor = []string{"P3"}
	})
	wantError(t, Check(m, delegatingInput(2)), "wait on each other")
	// no circle without the cross waits
	if errs := Check(treePlan(), delegatingInput(2)); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}

// A sub-piece inherits the waits of every ancestor, not only its parent's.
func TestCheck_CircleThroughAGrandparentsWait(t *testing.T) {
	in := delegatingInput(3)
	m := acme()
	m.Pieces[3].WaitsFor = []string{"P3.1.1"} // P5 waits for the deepest sub-piece
	m.Pieces = append(m.Pieces,
		Piece{ID: "P3", Domain: "design", Delegate: true, Brief: "b", Paths: []string{"design/**"}, WaitsFor: []string{"P5"}},
		Piece{ID: "P3.1", Domain: "design", Delegate: true, Brief: "b", Paths: []string{"design/icons/**"}},
		Piece{ID: "P3.1.1", Domain: "design", Factory: "visuals", Brief: "b", Paths: []string{"design/icons/a/**"}},
	)
	errs := Check(m, in)
	wantError(t, errs, "P5 and P3.1.1 wait on each other")
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
}

// A delegated piece finishes after its children, so a child that waits for it
// is a circle. Check also refuses that wait by the sub-piece rule; the circle
// is named as well. No plan that passes every other rule makes a circle by
// the children edge alone (every way out of a sub-piece leads through its
// ancestors' waits, which the delegated piece already holds), so the edge is
// also tested directly.
func TestCheck_ChildrenEdgeMakesACircle(t *testing.T) {
	m := edited(func(m *Mission) { m.Pieces[6].WaitsFor = []string{"P3"} })
	errs := Check(m, delegatingInput(2))
	wantError(t, errs, "P3, P3.1 and P3.2 wait on each other")
	wantError(t, errs, `piece P3.1: waits_for "P3", which is not under P3`)
}

func TestTreeEdges_DelegatedPieceWaitsForItsChildren(t *testing.T) {
	m := treePlan()
	index := pieceIndex(m.Pieces)
	if got := treeEdges(m.Pieces, index, index["P3"]); !slices.Equal(got, []int{index["P3.1"], index["P3.2"]}) {
		t.Fatalf("edges of P3 = %v", got)
	}
	if got := treeEdges(m.Pieces, index, index["P3.1"]); len(got) != 0 {
		t.Fatalf("edges of P3.1 = %v", got)
	}
}

func TestCheck_ParentWithNoPathsLeavesSubPiecesFree(t *testing.T) {
	in := delegatingInput(2)
	in.Domains[2].Paths = nil
	m := edited(func(m *Mission) { m.Pieces[5].Paths = nil })
	// P3.1 has paths of its own, P3.2 has none: both are accepted
	if errs := Check(m, in); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}

func TestCheck_PiecesLimitCountsSubPieces(t *testing.T) {
	in := delegatingInput(2)
	in.Limits.Pieces = lim(7)
	wantError(t, Check(treePlan(), in), "8 pieces is more than the limit of 7")
}

func TestCheckNew_RefusesSubPieceIDs(t *testing.T) {
	errs := CheckNew(treePlan(), delegatingInput(2))
	wantError(t, errs, `piece P3.1: an id with a "." names a sub-piece, and crew names those when it plans a delegated piece`)
	wantError(t, errs, `piece P3.2: an id with a "."`)
	if errs := CheckNew(acme(), baseInput()); len(errs) != 0 {
		t.Fatalf("errs = %v", errs)
	}
}
