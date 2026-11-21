package mission

import (
	"slices"

	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/source"
)

// AutoObs is the auto-merge mode one pull request follows.
type AutoObs struct {
	Mode    principal.AutoMode
	Blocker string // a domain the pull request touches whose switch is off; "" otherwise
}

// EffectiveMode: the piece's own domain decides first. Every other domain that
// lists paths and has at least one of files inside them must be on too. A
// domain with no switch record is off.
func EffectiveMode(p Piece, files []string, domains map[string]source.Domain, switches map[string]principal.SwitchRecord) AutoObs {
	own := modeFor(switches, p.Domain)
	if own == principal.AutoOff {
		return AutoObs{Mode: principal.AutoOff}
	}
	modes := []principal.AutoMode{own}
	for _, name := range touchedDomains(p.Domain, files, domains) {
		m := modeFor(switches, name)
		if m == principal.AutoOff {
			return AutoObs{Mode: principal.AutoOff, Blocker: name}
		}
		modes = append(modes, m)
	}
	if slices.Contains(modes, principal.AutoOn) {
		return AutoObs{Mode: principal.AutoOn}
	}
	return AutoObs{Mode: principal.AutoWithoutChecks}
}

func modeFor(switches map[string]principal.SwitchRecord, domain string) principal.AutoMode {
	switch m := switches[domain].Mode; m {
	case principal.AutoOn, principal.AutoWithoutChecks:
		return m
	}
	return principal.AutoOff
}

// touchedDomains returns, sorted by name, the domains other than own that list
// paths and have at least one of files inside them.
func touchedDomains(own string, files []string, domains map[string]source.Domain) []string {
	var out []string
	for name, d := range domains {
		if name == own || len(d.Paths) == 0 {
			continue
		}
		if anyFileMatches(d.Paths, files) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

func anyFileMatches(patterns, files []string) bool {
	for _, f := range files {
		for _, pat := range patterns {
			if MatchPath(pat, f) {
				return true
			}
		}
	}
	return false
}
