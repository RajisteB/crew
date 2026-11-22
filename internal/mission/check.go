package mission

import (
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

// ErrNoDeny matches (errors.Is) the check error for a station whose harness
// can't apply deny rules.
var ErrNoDeny = errors.New("harness can't apply deny rules")

// noDenyError keeps its own text and unwraps to ErrNoDeny.
type noDenyError struct{ msg string }

func (e noDenyError) Error() string { return e.msg }
func (e noDenyError) Unwrap() error { return ErrNoDeny }

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

// reservedID can't be a piece id: the epic's Jira label ends in it
// (TrackerLabel), so a piece with that id would share the epic's label.
const reservedID = "epic"

// CheckInput is what a plan is checked against.
type CheckInput struct {
	Domains   []source.Domain
	Factories []source.Factory
	Registry  *config.Registry
	Limits    Limits
}

// Check returns every problem with the plan, pieces in file order.
func Check(m Mission, in CheckInput) []error {
	var errs []error
	if strings.TrimSpace(m.Request) == "" {
		errs = append(errs, fmt.Errorf("the request is empty"))
	}
	if !Allows(in.Limits.Pieces, len(m.Pieces)) {
		errs = append(errs, fmt.Errorf("%d pieces is more than the limit of %d", len(m.Pieces), in.Limits.Pieces.N))
	}
	index := pieceIndex(m.Pieces)
	seen := map[string]bool{}
	for i, p := range m.Pieces {
		errs = append(errs, checkID(i, p, seen)...)
		errs = append(errs, checkPiece(p, in)...)
		errs = append(errs, checkWaits(p, index)...)
	}
	for _, ring := range waitCircles(m.Pieces, index) {
		errs = append(errs, fmt.Errorf("%s wait on each other", joinIDs(ring)))
	}
	return errs
}

func label(p Piece) string { return "piece " + p.ID }

func checkID(i int, p Piece, seen map[string]bool) []error {
	var errs []error
	switch {
	case p.ID == "":
		errs = append(errs, fmt.Errorf("piece %d has no id", i+1))
	case !idPattern.MatchString(p.ID):
		errs = append(errs, fmt.Errorf("%s: id must be letters, digits, '-' or '_' and start with a letter or digit", label(p)))
	case strings.EqualFold(p.ID, reservedID):
		errs = append(errs, fmt.Errorf("piece id %q is reserved", reservedID))
	case seen[p.ID]:
		errs = append(errs, fmt.Errorf("%s: id is used more than once", label(p)))
	}
	seen[p.ID] = true
	return errs
}

func checkPiece(p Piece, in CheckInput) []error {
	var errs []error
	if strings.TrimSpace(p.Brief) == "" {
		errs = append(errs, fmt.Errorf("%s: brief is empty", label(p)))
	}
	dom, domOK := findDomain(in.Domains, p.Domain)
	if !domOK {
		errs = append(errs, fmt.Errorf("%s: unknown domain %q", label(p), p.Domain))
	}
	errs = append(errs, checkRoute(p, dom, domOK, in)...)
	return errs
}

// checkRoute checks the delegate/factory choice and, for a factory, that it
// exists, belongs to the domain, and is safe to run.
func checkRoute(p Piece, dom source.Domain, domOK bool, in CheckInput) []error {
	switch {
	case p.Delegate && p.Factory != "":
		return []error{fmt.Errorf("%s: set either factory or delegate, not both", label(p))}
	case p.Delegate:
		return []error{fmt.Errorf("%s: delegation isn't available yet", label(p))}
	case p.Factory == "":
		return []error{fmt.Errorf("%s: set a factory or delegate: true", label(p))}
	}
	f, ok := findFactory(in.Factories, p.Factory)
	if !ok {
		return []error{fmt.Errorf("%s: unknown factory %q", label(p), p.Factory)}
	}
	var errs []error
	if domOK && !contains(dom.Factories, p.Factory) {
		errs = append(errs, fmt.Errorf("%s: factory %q is not one of domain %q's factories", label(p), p.Factory, p.Domain))
	}
	errs = append(errs, checkDeny(p, f, in.Registry)...)
	return append(errs, checkWatch(p, f)...)
}

func checkDeny(p Piece, f source.Factory, reg *config.Registry) []error {
	var errs []error
	for _, st := range f.StationNames() {
		name := f.HarnessFor(st)
		var h config.Harness
		if reg != nil {
			h = reg.Harnesses[name]
		}
		if !run.CanDeny(h) {
			errs = append(errs, noDenyError{fmt.Sprintf("%s: station %q runs on harness %q, which can't apply deny rules", label(p), st, name)})
		}
	}
	return errs
}

func checkWatch(p Piece, f source.Factory) []error {
	var errs []error
	for _, root := range f.WatchRoots() {
		clean := path.Clean(filepath.ToSlash(root))
		if filepath.IsAbs(root) || strings.HasPrefix(root, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
			errs = append(errs, fmt.Errorf("%s: factory %q watches %q, which is outside the repo", label(p), f.Name, root))
		}
	}
	return errs
}

func pieceIndex(pieces []Piece) map[string]int {
	index := map[string]int{}
	for i, p := range pieces {
		if _, ok := index[p.ID]; !ok {
			index[p.ID] = i
		}
	}
	return index
}

func checkWaits(p Piece, index map[string]int) []error {
	var errs []error
	for _, w := range p.WaitsFor {
		if w == p.ID {
			errs = append(errs, fmt.Errorf("%s: waits for itself", label(p)))
		} else if _, ok := index[w]; !ok {
			errs = append(errs, fmt.Errorf("%s: waits_for %q, which is not a piece", label(p), w))
		}
	}
	return errs
}

func joinIDs(ids []string) string {
	if len(ids) == 1 {
		return ids[0]
	}
	return strings.Join(ids[:len(ids)-1], ", ") + " and " + ids[len(ids)-1]
}

func findDomain(ds []source.Domain, name string) (source.Domain, bool) {
	for _, d := range ds {
		if d.Name == name {
			return d, true
		}
	}
	return source.Domain{}, false
}

func findFactory(fs []source.Factory, name string) (source.Factory, bool) {
	for _, f := range fs {
		if f.Name == name {
			return f, true
		}
	}
	return source.Factory{}, false
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
