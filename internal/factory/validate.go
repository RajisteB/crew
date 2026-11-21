// Package factory checks and draws factory graphs. It knows nothing about
// harnesses or agents; verify adds those checks on top.
package factory

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// Issue is one finding. Info issues are notes; everything else is an error.
type Issue struct {
	Info bool
	Msg  string
}

var stationName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Validate checks the graph on its own: names, targets, reachability and
// loop limits.
func Validate(f source.Factory) []Issue {
	var out []Issue
	errf := func(format string, a ...any) { out = append(out, Issue{Msg: fmt.Sprintf(format, a...)}) }
	infof := func(format string, a ...any) { out = append(out, Issue{Info: true, Msg: fmt.Sprintf(format, a...)}) }

	switch _, ok := f.Stations[f.Start]; {
	case f.Start == "":
		errf("start is required")
	case !ok:
		errf("start %q is not a station", f.Start)
	}
	if _, ok := f.Stations[f.Rework]; f.Rework != "" && !ok {
		errf("rework %q is not a station", f.Rework)
	}
	for _, n := range f.StationNames() {
		checkStation(f, n, errf, infof)
	}
	if _, ok := f.Stations[f.Start]; ok {
		reach := reachable(f)
		for _, n := range f.StationNames() {
			if !reach[n] {
				errf("station %s can't be reached from start", n)
			}
		}
	}
	if cyc := uncappedCycle(f); cyc != "" {
		errf("loop with no limit: %s; add max (a number or \"*\") to one of these routes", cyc)
	}
	return out
}

func checkStation(f source.Factory, n string, errf, infof func(string, ...any)) {
	s := f.Stations[n]
	if n == source.TargetDone || n == source.TargetFail {
		errf("station name %q is reserved", n)
	} else if !stationName.MatchString(n) {
		errf("station name %q must be lowercase letters, digits and dashes", n)
	}
	if len(s.Routes) == 0 {
		errf("station %s has no routes", n)
	}
	if s.OnDrift != "" {
		if _, ok := f.Stations[s.OnDrift]; !ok {
			errf("%s: on_drift %q is not a station", n, s.OnDrift)
		}
	}
	for _, rn := range s.RouteNames() {
		r := s.Routes[rn]
		if !isTarget(f, r.To) {
			errf("%s.%s goes to %q, which is not a station (or done/fail)", n, rn, r.To)
			continue
		}
		if rn == source.RouteFail && r.To != source.TargetFail {
			errf("%s: route \"fail\" is reserved and always ends the run; remove it or point it at fail", n)
		}
		switch {
		case r.Max.Unlimited:
			infof("%s.%s loops without limit (\"*\")", n, rn)
		case r.Max.Set && !IsBackEdge(f, n, rn):
			infof("max on %s.%s has no effect: this route can't loop", n, rn)
		}
	}
}

func isTarget(f source.Factory, t string) bool {
	_, ok := f.Stations[t]
	return ok || t == source.TargetDone || t == source.TargetFail
}

// IsBackEdge reports whether taking route from station `from` can lead back
// to `from`, i.e. the route is part of a loop.
func IsBackEdge(f source.Factory, from, route string) bool {
	t := f.Stations[from].Routes[route].To
	if _, ok := f.Stations[t]; !ok {
		return false
	}
	seen := map[string]bool{}
	stack := []string{t}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if n == from {
			return true
		}
		if seen[n] {
			continue
		}
		seen[n] = true
		for _, r := range f.Stations[n].Routes {
			if _, ok := f.Stations[r.To]; ok {
				stack = append(stack, r.To)
			}
		}
	}
	return false
}

// reachable follows routes and drift edges from start.
func reachable(f source.Factory) map[string]bool {
	seen := map[string]bool{}
	stack := []string{f.Start}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[n] {
			continue
		}
		seen[n] = true
		s := f.Stations[n]
		for _, r := range s.Routes {
			if _, ok := f.Stations[r.To]; ok {
				stack = append(stack, r.To)
			}
		}
		if _, ok := f.Stations[s.OnDrift]; ok {
			stack = append(stack, s.OnDrift)
		}
	}
	return seen
}

// uncappedCycle drops every route that has a max and looks for a cycle in
// what is left. Every loop needs at least one capped route, so any cycle here
// is an error. Returns "a →r→ b →s→ a", or "".
func uncappedCycle(f source.Factory) string {
	const (
		unvisited = iota
		onStack
		done
	)
	state := map[string]int{}
	var path []hop
	var found string

	var visit func(n string)
	visit = func(n string) {
		state[n] = onStack
		s := f.Stations[n]
		for _, rn := range s.RouteNames() {
			r := s.Routes[rn]
			if _, ok := f.Stations[r.To]; !ok || r.Max.Set || found != "" {
				continue
			}
			path = append(path, hop{n, rn})
			switch state[r.To] {
			case onStack:
				found = formatCycle(path, r.To)
			case unvisited:
				visit(r.To)
			}
			path = path[:len(path)-1]
		}
		state[n] = done
	}
	for _, n := range f.StationNames() {
		if state[n] == unvisited && found == "" {
			visit(n)
		}
	}
	return found
}

type hop struct{ station, route string }

func formatCycle(path []hop, back string) string {
	start := 0
	for i, h := range path {
		if h.station == back {
			start = i
			break
		}
	}
	var b strings.Builder
	for _, h := range path[start:] {
		fmt.Fprintf(&b, "%s →%s→ ", h.station, h.route)
	}
	b.WriteString(back)
	return b.String()
}
