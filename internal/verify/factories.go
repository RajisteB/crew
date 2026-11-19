package verify

import (
	"github.com/crewfile-dev/crew/internal/factory"
	"github.com/crewfile-dev/crew/internal/source"
)

// ForemanPrefix names the agent crew compiles for each factory.
const ForemanPrefix = "foreman-"

// RunSubcommands can't be factory names: `crew run <name>` would be ambiguous.
var RunSubcommands = []string{"status", "say", "goto", "approve", "resume"}

// checkFactories runs after checkCrew, so p.Enabled and p.Selection are set.
func checkFactories(p *Project, rep *Report) {
	agents := map[string]source.Agent{}
	for _, a := range p.Agents {
		agents[a.Name] = a
	}
	skills := map[string]bool{}
	for _, s := range p.Skills {
		skills[s.Name] = true
	}
	for _, f := range p.Factories {
		checkCommon(rep, f.Path, f.Name, f.Description, f.Body)
		checkFactoryName(rep, f, agents, skills)
		for _, i := range factory.Validate(f) {
			if i.Info {
				rep.infof(f.Path, "%s", i.Msg)
			} else {
				rep.errf(f.Path, "%s", i.Msg)
			}
		}
		for _, n := range f.StationNames() {
			checkStationAgent(p, rep, f, n, agents)
		}
	}
}

func checkFactoryName(rep *Report, f source.Factory, agents map[string]source.Agent, skills map[string]bool) {
	for _, r := range RunSubcommands {
		if f.Name == r {
			rep.errf(f.Path, "factory name %q is reserved (it's a crew run subcommand)", f.Name)
		}
	}
	if _, ok := agents[f.Name]; ok || skills[f.Name] {
		rep.errf(f.Path, "name %q is also used by an agent or skill; keep names unique", f.Name)
	}
	if a, ok := agents[ForemanPrefix+f.Name]; ok {
		rep.errf(a.Path, "agent %q clashes with the foreman crew generates for factory %s; rename the agent", a.Name, f.Name)
	}
}

func checkStationAgent(p *Project, rep *Report, f source.Factory, n string, agents map[string]source.Agent) {
	s := f.Stations[n]
	if s.Agent == "" {
		rep.errf(f.Path, "station %s: agent is required", n)
		return
	}
	if _, ok := agents[s.Agent]; !ok {
		rep.errf(f.Path, "station %s: unknown agent %q", n, s.Agent)
		return
	}
	if _, err := f.TimeoutFor(n); err != nil {
		rep.errf(f.Path, "station %s: %v", n, err)
	}
	hn := f.HarnessFor(n)
	if hn == "" {
		rep.errf(f.Path, "station %s has no harness: set harness: on the factory or the station", n)
		return
	}
	h, ok := p.Registry.Harnesses[hn]
	switch {
	case !ok:
		rep.errf(f.Path, "station %s: unknown harness %q", n, hn)
		return
	case !contains(p.Enabled, hn):
		rep.errf(f.Path, "station %s runs on %s, which isn't enabled in crew.yaml", n, hn)
		return
	case h.Headless == nil:
		rep.errf(f.Path, "station %s runs on %s, which has no headless: command in harnesses.yaml", n, hn)
		return
	}
	for _, sel := range p.Selection[hn] {
		if sel.Agent.Name == s.Agent {
			return
		}
	}
	rep.errf(f.Path, "station %s: agent %s isn't compiled for %s (crew.yaml targets.%s.agents)", n, s.Agent, hn, hn)
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
