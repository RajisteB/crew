package main

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/source"
)

// printPieceTable prints the plan's pieces with the paths each may change,
// then a note for each piece whose paths can't be checked or reach outside its
// domain.
func printPieceTable(out io.Writer, m mission.Mission, domains []source.Domain) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PIECE\tDOMAIN\tFACTORY\tWAITS FOR\tPATHS")
	for _, p := range m.Pieces {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.Domain, p.Factory,
			listOrDash(p.WaitsFor), listOrDash(mission.EffectivePaths(p, domainNamed(domains, p.Domain))))
	}
	tw.Flush()
	for _, p := range m.Pieces {
		d := domainNamed(domains, p.Domain)
		if outside := mission.OutsideDomain(p, d); len(outside) > 0 {
			fmt.Fprintf(out, "%s may change files outside the %s domain: %s\n", p.ID, p.Domain, strings.Join(outside, ", "))
		}
		if mission.EffectivePaths(p, d) == nil {
			fmt.Fprintf(out, "%s has no paths, so crew can't check it against other pieces or check what it changes\n", p.ID)
		}
	}
}

func listOrDash(items []string) string {
	if len(items) == 0 {
		return "-"
	}
	return strings.Join(items, ", ")
}

func domainNamed(domains []source.Domain, name string) source.Domain {
	for _, d := range domains {
		if d.Name == name {
			return d
		}
	}
	return source.Domain{}
}
