package mission

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
)

// idPattern is a top part, then any number of ".<number>" parts.
var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*(\.[1-9][0-9]*)*$`)

// CheckNew is Check for a plan that starts a mission: it also refuses every
// id with a ".", since crew names sub-pieces itself.
func CheckNew(m Mission, in CheckInput) []error {
	errs := Check(m, in)
	for _, p := range m.Pieces {
		if ParentID(p.ID) != "" {
			errs = append(errs, fmt.Errorf(`%s: an id with a "." names a sub-piece, and crew names those when it plans a delegated piece`, label(p)))
		}
	}
	return errs
}

// checkDelegation checks a delegated piece against the depth limit and the
// orchestrator's harness.
func checkDelegation(p Piece, in CheckInput) []error {
	var errs []error
	if level := Level(p.ID); !Allows(in.Limits.Depth, level) {
		errs = append(errs, fmt.Errorf("%s: delegates at depth %d, and limits.delegation_depth is %d", label(p), level, in.Limits.Depth.N))
	}
	var h config.Harness
	if in.Registry != nil {
		h = in.Registry.Harnesses[in.OrchestratorHarness]
	}
	if !run.CanDeny(h) {
		errs = append(errs, noDenyError{fmt.Sprintf(`%s: the orchestrator's harness %q can't apply deny rules, so nothing can be delegated`, label(p), in.OrchestratorHarness)})
	}
	return errs
}

// checkSubPiece holds a sub-piece inside its parent: the parent is in the
// plan and delegated, the domain is the same, the paths lie within the
// parent's, and it waits only for pieces under that parent.
func checkSubPiece(p Piece, pieces []Piece, index map[string]int, in CheckInput) []error {
	parentID := ParentID(p.ID)
	if parentID == "" {
		return nil
	}
	i, ok := index[parentID]
	if !ok {
		return []error{fmt.Errorf("%s: its parent %s is not in the plan (to drop %s, remove the pieces under it too)", label(p), parentID, parentID)}
	}
	parent := pieces[i]
	if !parent.Delegate {
		return []error{fmt.Errorf("%s: its parent %s is not delegated", label(p), parentID)}
	}
	var errs []error
	if p.Domain != parent.Domain {
		errs = append(errs, fmt.Errorf("%s: domain %q is not its parent's domain %q", label(p), p.Domain, parent.Domain))
	}
	errs = append(errs, checkSubPaths(p, parent, in)...)
	for _, w := range p.WaitsFor {
		if !Under(w, parentID) {
			errs = append(errs, fmt.Errorf("%s: waits_for %q, which is not under %s; a sub-piece waits only for pieces under its own parent", label(p), w, parentID))
		}
	}
	return errs
}

func checkSubPaths(p, parent Piece, in CheckInput) []error {
	dom, _ := findDomain(in.Domains, p.Domain)
	pdom, _ := findDomain(in.Domains, parent.Domain)
	outer := EffectivePaths(parent, pdom)
	if len(outer) == 0 {
		return nil
	}
	inner := EffectivePaths(p, dom)
	if len(inner) == 0 {
		return []error{fmt.Errorf("%s: has no paths, and its parent %s has; a sub-piece keeps inside its parent's paths", label(p), parent.ID)}
	}
	if outside, ok := PathsWithin(inner, outer); !ok {
		return []error{fmt.Errorf("%s: paths: %q is outside the paths of its parent %s", label(p), outside, parent.ID)}
	}
	return nil
}

// treeEdges are the extra waits the piece tree adds to piece i: every
// waits_for of each ancestor, and, for a delegated piece, each of its
// children (it finishes after them).
func treeEdges(pieces []Piece, index map[string]int, i int) []int {
	return treeEdgesWith(pieces, index, i, false)
}

// treeEdgesWith is treeEdges, and with withApart set the ancestors' apart_from
// links count as well as their waits_for.
func treeEdgesWith(pieces []Piece, index map[string]int, i int, withApart bool) []int {
	p := pieces[i]
	var out []int
	for _, a := range Ancestors(p.ID) {
		if j, ok := index[a]; ok {
			links := pieces[j].WaitsFor
			if withApart {
				links = slices.Concat(links, pieces[j].Apart)
			}
			for _, w := range links {
				if k, ok := index[w]; ok && k != i {
					out = append(out, k)
				}
			}
		}
	}
	if p.Delegate {
		for j, c := range pieces {
			if ParentID(c.ID) == p.ID && j != i {
				out = append(out, j)
			}
		}
	}
	return slices.Compact(out)
}
