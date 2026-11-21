package mission

import (
	"fmt"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*$`)

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
	errs = append(errs, checkIDs(m.Pieces)...)
	for _, p := range m.Pieces {
		errs = append(errs, checkPiece(p, in)...)
	}
	errs = append(errs, checkWaits(m.Pieces)...)
	return errs
}

func label(p Piece) string { return "piece " + p.ID }

func checkIDs(pieces []Piece) []error {
	var errs []error
	seen := map[string]bool{}
	for i, p := range pieces {
		switch {
		case p.ID == "":
			errs = append(errs, fmt.Errorf("piece %d has no id", i+1))
		case !idPattern.MatchString(p.ID):
			errs = append(errs, fmt.Errorf("%s: id must be letters, digits, '-' or '_' and start with a letter or digit", label(p)))
		case seen[p.ID]:
			errs = append(errs, fmt.Errorf("%s: id is used more than once", label(p)))
		}
		seen[p.ID] = true
	}
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
			errs = append(errs, fmt.Errorf("%s: station %q runs on harness %q, which can't apply deny rules", label(p), st, name))
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

func checkWaits(pieces []Piece) []error {
	index := map[string]int{}
	for i, p := range pieces {
		if _, ok := index[p.ID]; !ok {
			index[p.ID] = i
		}
	}
	var errs []error
	for _, p := range pieces {
		for _, w := range p.WaitsFor {
			if w == p.ID {
				errs = append(errs, fmt.Errorf("%s: waits for itself", label(p)))
			} else if _, ok := index[w]; !ok {
				errs = append(errs, fmt.Errorf("%s: waits_for %q, which is not a piece", label(p), w))
			}
		}
	}
	for _, ring := range waitCircles(pieces, index) {
		errs = append(errs, fmt.Errorf("%s wait on each other", joinIDs(ring)))
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
