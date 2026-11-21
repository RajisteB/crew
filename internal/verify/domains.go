package verify

import "github.com/crewfile-dev/crew/internal/source"

// checkDomains runs after checkFactories. Releases are only noted: they are
// checked once release targets exist.
func checkDomains(p *Project, rep *Report) {
	factories := map[string]bool{}
	for _, f := range p.Factories {
		factories[f.Name] = true
	}
	seen := map[string]bool{}
	for _, d := range p.Domains {
		checkDomain(rep, d, factories, seen)
	}
}

func checkDomain(rep *Report, d source.Domain, factories, seen map[string]bool) {
	if d.Name != "" && d.Name != source.DefinitionStem(d.Path) {
		rep.errf(d.Path, "name %q must match the filename %q", d.Name, source.DefinitionStem(d.Path))
	}
	if d.Name == "" {
		rep.errf(d.Path, "domain: name is required")
	} else if seen[d.Name] {
		rep.errf(d.Path, "domain %s: name is also used by another domain", d.Name)
	}
	seen[d.Name] = true
	if d.Description == "" {
		rep.errf(d.Path, "domain %s: description is required", d.Name)
	}
	if len(d.Factories) == 0 {
		rep.errf(d.Path, "domain %s: factories must list at least one factory", d.Name)
	}
	for _, f := range d.Factories {
		if !factories[f] {
			rep.errf(d.Path, "domain %s: no factory %q", d.Name, f)
		}
	}
	if len(d.Releases) > 0 {
		rep.infof(d.Path, "domain %s: releases are checked from Project 2", d.Name)
	}
}
