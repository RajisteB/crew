package source

import (
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

// Reserved route targets and the route every station may return.
const (
	TargetDone = "done"
	TargetFail = "fail"
	RouteFail  = "fail"
	// DefaultTimeout applies when neither the station nor the factory sets one.
	DefaultTimeout = 60 * time.Minute
)

// Factory is factories/<name>.md: the frontmatter is the station graph, the
// body is the briefing every station receives.
type Factory struct {
	Name        string             `yaml:"name"`
	Description string             `yaml:"description"`
	Start       string             `yaml:"start"`
	Rework      string             `yaml:"rework"`  // station that handles rework; defaults to start
	Harness     string             `yaml:"harness"` // default station harness for crew run
	Watch       []string           `yaml:"watch"`   // drift roots relative to the project; default ["."]
	Timeout     string             `yaml:"timeout"` // Go duration
	Stations    map[string]Station `yaml:"stations"`

	Body string `yaml:"-"`
	Path string `yaml:"-"`
}

// ReworkStation is the station rework goes to: Rework, or Start when unset.
func (f Factory) ReworkStation() string {
	if f.Rework != "" {
		return f.Rework
	}
	return f.Start
}

type Station struct {
	Agent   string           `yaml:"agent"`
	Harness string           `yaml:"harness"`
	Approve bool             `yaml:"approve"`
	OnDrift string           `yaml:"on_drift"`
	Timeout string           `yaml:"timeout"`
	Routes  map[string]Route `yaml:"routes"`
}

// Route is where a station's result sends the work. Written either as
// `name: target` or `name: {to: target, max: 3}` (max may be "*").
type Route struct {
	To  string
	Max Limit
}

// Limit caps how many times a route may be taken in one run.
type Limit struct {
	Set       bool // max was written
	Unlimited bool // max: "*"
	N         int
}

func (l Limit) String() string {
	switch {
	case !l.Set:
		return ""
	case l.Unlimited:
		return "*"
	}
	return strconv.Itoa(l.N)
}

var errLimit = errors.New(`max must be a positive number or "*"`)

func (l *Limit) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode {
		return errLimit
	}
	if n.Value == "*" {
		*l = Limit{Set: true, Unlimited: true}
		return nil
	}
	v, err := strconv.Atoi(n.Value)
	if err != nil || v < 1 || n.Tag == "!!str" {
		return errLimit
	}
	*l = Limit{Set: true, N: v}
	return nil
}

func (r *Route) UnmarshalYAML(n *yaml.Node) error {
	switch n.Kind {
	case yaml.ScalarNode:
		*r = Route{To: n.Value}
		return nil
	case yaml.MappingNode:
		// Decoding a sub-node drops the parent's strict mode, so check keys here.
		for i := 0; i < len(n.Content); i += 2 {
			if k := n.Content[i].Value; k != "to" && k != "max" {
				return fmt.Errorf("route: unknown key %q (allowed: to, max)", k)
			}
		}
		var raw struct {
			To  string `yaml:"to"`
			Max Limit  `yaml:"max"`
		}
		if err := n.Decode(&raw); err != nil {
			return err
		}
		*r = Route{To: raw.To, Max: raw.Max}
		return nil
	}
	return errors.New("route must be a station name or {to: station, max: N}")
}

// LoadFactories parses every factories/*.md, reporting failures per file.
func LoadFactories(root string) ([]Factory, []error) {
	var out []Factory
	var errs []error
	files, err := listMarkdown(filepath.Join(root, FactoriesDir))
	if err != nil {
		return nil, []error{err}
	}
	for _, f := range files {
		fac, err := ParseFactory(f)
		if err != nil {
			errs = append(errs, &FileError{rel(root, f), err})
			continue
		}
		fac.Path = rel(root, f)
		out = append(out, fac)
	}
	return out, errs
}

// ParseFactory reads one factory file. Path is left as given.
func ParseFactory(path string) (Factory, error) {
	var f Factory
	body, err := parse(path, &f)
	if err != nil {
		return Factory{}, err
	}
	f.Body, f.Path = body, path
	return f, nil
}

// StationNames lists the start station first, then the rest sorted.
func (f Factory) StationNames() []string {
	var rest []string
	for n := range f.Stations {
		if n != f.Start {
			rest = append(rest, n)
		}
	}
	sort.Strings(rest)
	if _, ok := f.Stations[f.Start]; ok {
		return append([]string{f.Start}, rest...)
	}
	return rest
}

// RouteNames lists a station's routes sorted.
func (s Station) RouteNames() []string {
	out := make([]string, 0, len(s.Routes))
	for n := range s.Routes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// HarnessFor is the harness a station runs on under crew run.
func (f Factory) HarnessFor(station string) string {
	if h := f.Stations[station].Harness; h != "" {
		return h
	}
	return f.Harness
}

func (f Factory) TimeoutFor(station string) (time.Duration, error) {
	v := f.Stations[station].Timeout
	if v == "" {
		v = f.Timeout
	}
	if v == "" {
		return DefaultTimeout, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return 0, fmt.Errorf("timeout %q is not a positive duration like 30m or 2h", v)
	}
	return d, nil
}

// WatchRoots are the folders snapshotted for drift guards.
func (f Factory) WatchRoots() []string {
	if len(f.Watch) == 0 {
		return []string{"."}
	}
	return append([]string(nil), f.Watch...)
}
