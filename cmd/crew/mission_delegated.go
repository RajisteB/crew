package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/source"
)

// factoryCell is the plan table's FACTORY column: a delegated piece has no
// factory of its own.
func factoryCell(p mission.Piece) string {
	if p.Delegate {
		return "(delegated)"
	}
	return p.Factory
}

// printDelegated prints one line for each delegated piece: who splits it and
// what its pieces will be held to.
func printDelegated(out io.Writer, m mission.Mission, domains []source.Domain) {
	for _, p := range m.Pieces {
		if !p.Delegate {
			continue
		}
		paths := "are not held to any paths"
		if eff := mission.EffectivePaths(p, domainNamed(domains, p.Domain)); len(eff) > 0 {
			paths = "keep to " + strings.Join(eff, ", ")
		}
		fmt.Fprintf(out, "%s is delegated: the %s orchestrator splits it once it is ready. Its pieces stay in %s, use %s's factories and %s.\n",
			p.ID, p.Domain, p.Domain, p.Domain, paths)
	}
}
