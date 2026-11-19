package factory

import (
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// Mermaid draws the factory as a left-to-right flowchart. Solid arrows are
// plain routes; dashed arrows are capped routes (labelled ≤N or ≤*) and drift
// redirects. Approval stations are hexagons. Node ids are prefixed (s_, t_)
// so station names can never collide with Mermaid keywords such as "end".
func Mermaid(f source.Factory) string {
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	fmt.Fprintf(&b, "  start(( )) --> %s\n", stationID(f.Start))

	names := f.StationNames()
	for _, n := range names {
		s := f.Stations[n]
		label := fmt.Sprintf(`"%s<br/><small>%s</small>"`, n, s.Agent)
		if s.Approve {
			fmt.Fprintf(&b, "  %s{{%s}}\n", stationID(n), label)
		} else {
			fmt.Fprintf(&b, "  %s(%s)\n", stationID(n), label)
		}
	}
	for _, t := range []string{source.TargetDone, source.TargetFail} {
		if targeted(f, t) {
			fmt.Fprintf(&b, "  t_%s([\"%s\"])\n", t, t)
		}
	}
	for _, n := range names {
		writeEdges(&b, f, n)
	}
	return b.String()
}

func writeEdges(b *strings.Builder, f source.Factory, n string) {
	s := f.Stations[n]
	for _, rn := range s.RouteNames() {
		r := s.Routes[rn]
		if r.Max.Set {
			fmt.Fprintf(b, "  %s -.->|\"%s ≤%s\"| %s\n", stationID(n), rn, r.Max, nodeID(f, r.To))
		} else {
			fmt.Fprintf(b, "  %s -->|\"%s\"| %s\n", stationID(n), rn, nodeID(f, r.To))
		}
	}
	if s.OnDrift != "" {
		fmt.Fprintf(b, "  %s -.->|\"drift\"| %s\n", stationID(n), stationID(s.OnDrift))
	}
}

func targeted(f source.Factory, t string) bool {
	for _, s := range f.Stations {
		for _, r := range s.Routes {
			if r.To == t {
				return true
			}
		}
	}
	return false
}

func stationID(n string) string { return "s_" + n }

func nodeID(f source.Factory, t string) string {
	if _, ok := f.Stations[t]; ok {
		return stationID(t)
	}
	return "t_" + t
}

// DiagramDoc is the Markdown file compile writes next to the build output.
func DiagramDoc(f source.Factory) string {
	return fmt.Sprintf("# %s\n\n%s\n\n```mermaid\n%s```\n", f.Name, f.Description, Mermaid(f))
}
