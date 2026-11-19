// Package verify loads a crew directory and checks it strictly. It never
// stops at the first problem: every issue is collected so one run shows
// everything that needs fixing.
package verify

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/source"
)

type Level int

const (
	Info Level = iota
	Warning
	Error
)

func (l Level) String() string {
	return [...]string{"info", "warn", "error"}[l]
}

type Issue struct {
	Level Level
	Where string
	Msg   string
}

type Report struct{ Issues []Issue }

func (r *Report) add(l Level, where, f string, a ...any) {
	r.Issues = append(r.Issues, Issue{l, where, fmt.Sprintf(f, a...)})
}
func (r *Report) errf(w, f string, a ...any)  { r.add(Error, w, f, a...) }
func (r *Report) warnf(w, f string, a ...any) { r.add(Warning, w, f, a...) }
func (r *Report) infof(w, f string, a ...any) { r.add(Info, w, f, a...) }

func (r *Report) Count(l Level) int {
	n := 0
	for _, i := range r.Issues {
		if i.Level == l {
			n++
		}
	}
	return n
}

// OK reports whether the project may be compiled. Strict by default:
// warnings block unless allowWarnings is set.
func (r *Report) OK(allowWarnings bool) bool {
	if r.Count(Error) > 0 {
		return false
	}
	return allowWarnings || r.Count(Warning) == 0
}

// Selected is one agent as it will be compiled for one harness, with the
// model and effort resolved for that harness.
type Selected struct {
	Agent  source.Agent
	Model  string             // models.yaml id, or "inherit"
	Effort string             // "" = harness default
	Entry  *config.ModelEntry // nil when the model is inherited
}

type Project struct {
	Dir      string
	Registry *config.Registry
	Models   *config.Models
	Crew     *config.Crew
	Agents   []source.Agent
	Skills   []source.Skill
	// Enabled harness names, sorted.
	Enabled []string
	// Selection[harness] = agents that harness receives, sorted by name.
	Selection map[string][]Selected
}

var nameRe = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// Now is overridable for tests.
var Now = time.Now

func Run(dir string) (*Project, *Report) {
	rep := &Report{}
	p := &Project{Dir: dir, Selection: map[string][]Selected{}}

	reg, err := config.LoadRegistry(dir)
	if err != nil {
		rep.errf(config.RegistryFile, "%v", err)
	}
	lo, err := config.LoadCrew(dir)
	if err != nil {
		rep.errf(config.CrewFile, "%v", err)
	}
	// models.yaml is only needed when some harness picks models from a catalog.
	// When it's missing, every other check still runs; model lookups are skipped.
	models := &config.Models{}
	if reg != nil && usesCatalog(reg) {
		if m, err := config.LoadModels(dir); err != nil {
			rep.errf(config.ModelsFile, "%v (run `crew models sync` to create it)", err)
			models = nil
		} else {
			models = m
		}
	}
	p.Registry, p.Crew, p.Models = reg, lo, models

	agents, errs := source.LoadAgents(dir)
	for _, e := range errs {
		rep.errf("", "%v", e)
	}
	skills, errs := source.LoadSkills(dir)
	for _, e := range errs {
		rep.errf("", "%v", e)
	}
	p.Agents, p.Skills = agents, skills

	for _, s := range source.StrayEntries(dir, source.AgentsDir) {
		rep.errf(s, "only one .md file per definition is allowed directly inside %s/", source.AgentsDir)
	}
	for _, li := range source.SkillLayoutIssues(dir) {
		rep.errf(li.Path, "%s", li.Msg)
	}
	if fi, err := os.Stat(filepath.Join(dir, source.LegacyWorkflowsDir)); err == nil && fi.IsDir() {
		rep.infof(source.LegacyWorkflowsDir+"/", "workflows/ is now factories/; move your files there")
	}

	if reg == nil || lo == nil {
		return p, rep // nothing more can be checked reliably
	}

	checkRegistry(reg, rep)
	if models != nil && usesCatalog(reg) {
		checkModels(reg, models, rep)
	}
	checkSources(p, rep)
	checkCrew(p, rep)
	return p, rep
}

// ---------------- registry ----------------

func checkRegistry(r *config.Registry, rep *Report) {
	const w = config.RegistryFile
	if r.Version != config.SchemaVersion {
		rep.errf(w, "version must be %d (got %d)", config.SchemaVersion, r.Version)
	}
	if v, err := time.Parse("2006-01-02", r.Verified); err != nil {
		rep.errf(w, "verified must be a YYYY-MM-DD date (got %q)", r.Verified)
	} else if r.StaleAfterDays <= 0 {
		rep.errf(w, "stale_after_days must be > 0")
	} else if age := int(Now().Sub(v).Hours() / 24); age > r.StaleAfterDays {
		rep.warnf(w, "harness facts were verified %d days ago (limit %d): re-check paths, fields and prices, then bump 'verified'", age, r.StaleAfterDays)
	}

	checkVocab(rep, w, "capabilities", r.Capabilities)
	for _, c := range r.Capabilities {
		if c == source.CapAll {
			rep.errf(w, "capability %q is reserved", c)
		}
	}
	if usesCatalog(r) && !strings.HasPrefix(r.CatalogSource, "https://") {
		rep.errf(w, "catalog_source must be an https:// URL (got %q)", r.CatalogSource)
	}

	if len(r.Harnesses) == 0 {
		rep.errf(w, "harnesses: at least one harness is required")
	}
	for _, name := range sortedKeys(r.Harnesses) {
		checkHarness(r, name, r.Harnesses[name], rep)
	}
}

func checkVocab(rep *Report, w, field string, vals []string) {
	if len(vals) == 0 {
		rep.errf(w, "%s must not be empty", field)
	}
	seen := map[string]bool{}
	for _, v := range vals {
		if !nameRe.MatchString(v) {
			rep.errf(w, "%s: %q must be lowercase kebab-case", field, v)
		}
		if seen[v] {
			rep.errf(w, "%s: %q listed twice", field, v)
		}
		seen[v] = true
	}
}

func checkHarness(r *config.Registry, name string, h config.Harness, rep *Report) {
	at := fmt.Sprintf("%s harnesses.%s", config.RegistryFile, name)
	if !nameRe.MatchString(name) {
		rep.errf(at, "harness key must be lowercase kebab-case")
	}
	if h.Display == "" {
		rep.errf(at, "display is required")
	}
	if c := h.Catalog; c != nil {
		if len(c.Providers) == 0 {
			rep.errf(at+".catalog", "providers must list at least one models.dev provider")
		}
		if c.IDStyle != "model" && c.IDStyle != "provider/model" {
			rep.errf(at+".catalog", "id_style must be \"model\" or \"provider/model\" (got %q)", c.IDStyle)
		}
	}

	// skills
	s := h.Skills
	if s.UserDir == "" || s.ProjectDir == "" {
		rep.errf(at+".skills", "user_dir and project_dir are required")
	}
	if filepath.IsAbs(s.ProjectDir) || strings.HasPrefix(s.ProjectDir, "~") {
		rep.errf(at+".skills", "project_dir must be relative to the project")
	}
	if s.Layout != "dir" && s.Layout != "flat" {
		rep.errf(at+".skills", "layout must be \"dir\" or \"flat\" (got %q)", s.Layout)
	}
	for _, o := range s.SharesWith {
		if o == name {
			rep.errf(at+".skills", "shares_with cannot reference itself")
		} else if _, ok := r.Harnesses[o]; !ok {
			rep.errf(at+".skills", "shares_with: unknown harness %q", o)
		}
	}

	// agents
	a := h.Agents
	if !a.Supported {
		if a.UserDir != "" || a.Format != "" || len(a.Static) > 0 || len(a.Derived) > 0 {
			rep.errf(at+".agents", "supported is false, so no other agent settings may be set")
		}
		if h.Catalog != nil || len(h.Capabilities) > 0 {
			rep.errf(at, "catalog/capabilities are only meaningful when agents.supported is true")
		}
		return
	}
	if a.UserDir == "" || a.ProjectDir == "" {
		rep.errf(at+".agents", "user_dir and project_dir are required")
	}
	if filepath.IsAbs(a.ProjectDir) || strings.HasPrefix(a.ProjectDir, "~") {
		rep.errf(at+".agents", "project_dir must be relative to the project")
	}
	if !strings.HasPrefix(a.Ext, ".") {
		rep.errf(at+".agents", "ext must start with '.' (got %q)", a.Ext)
	}
	f := a.Fields
	switch a.Format {
	case "markdown":
		if f.Body != "" {
			rep.errf(at+".agents.fields", "body is only for toml; markdown agents put the prompt after the frontmatter")
		}
	case "toml":
		if f.Body == "" {
			rep.errf(at+".agents.fields", "toml agents need fields.body (the key that receives the prompt)")
		}
	default:
		rep.errf(at+".agents", "format must be \"markdown\" or \"toml\" (got %q)", a.Format)
	}
	if f.Description == "" {
		rep.errf(at+".agents.fields", "description key is required")
	}
	keys := map[string]string{}
	claim := func(k, by string) {
		if k == "" {
			return
		}
		if prev, dup := keys[k]; dup {
			rep.errf(at+".agents", "output key %q is produced by both %s and %s", k, prev, by)
		}
		keys[k] = by
	}
	claim(f.Name, "fields.name")
	claim(f.Description, "fields.description")
	claim(f.Model, "fields.model")
	claim(f.Effort, "fields.effort")
	claim(f.Skills, "fields.skills")
	claim(f.Body, "fields.body")
	if f.Tools != nil {
		if f.Tools.Key == "" {
			rep.errf(at+".agents.fields.tools", "key is required")
		}
		if f.Tools.Style != "csv" && f.Tools.Style != "list" && f.Tools.Style != "map" {
			rep.errf(at+".agents.fields.tools", "style must be csv, list or map (got %q)", f.Tools.Style)
		}
		claim(f.Tools.Key, "fields.tools")
	}
	for _, k := range sortedKeys(a.Static) {
		claim(k, "static."+k)
		if !scalarOK(a.Static[k]) {
			rep.errf(at+".agents.static."+k, "only strings, numbers, booleans and string lists are supported")
		}
	}
	capSet := setOf(r.Capabilities)
	for i, d := range a.Derived {
		dat := fmt.Sprintf("%s.agents.derived[%d]", at, i)
		if d.Key == "" {
			rep.errf(dat, "key is required")
		}
		claim(d.Key, fmt.Sprintf("derived[%d]", i))
		if len(d.IfAny) == 0 {
			rep.errf(dat, "if_any must list capabilities")
		}
		for _, c := range d.IfAny {
			if !capSet[c] {
				rep.errf(dat, "unknown capability %q", c)
			}
		}
		if d.Then == nil {
			rep.errf(dat, "then is required")
		}
		if !scalarOK(d.Then) || (d.Else != nil && !scalarOK(d.Else)) {
			rep.errf(dat, "then/else must be scalars")
		}
	}

	if f.Effort != "" && f.EffortSuffix != "" {
		rep.errf(at+".agents.fields", "set effort or effort_suffix, not both")
	}
	if f.EffortSuffix != "" && f.Model == "" {
		rep.errf(at+".agents.fields", "effort_suffix needs a model field to append to")
	}
	if h.Catalog != nil && f.Model == "" {
		rep.errf(at, "catalog is set but agents.fields.model is empty, so no model could be written")
	}

	// capabilities: must map every vocabulary entry, nothing else
	for _, c := range r.Capabilities {
		if _, ok := h.Capabilities[c]; !ok {
			rep.errf(at+".capabilities", "missing mapping for %q (use [] if the harness has no per-agent tool control)", c)
		}
	}
	for _, c := range sortedKeys(h.Capabilities) {
		if !capSet[c] {
			rep.errf(at+".capabilities", "unknown capability %q (not in top-level capabilities)", c)
		}
	}
	if f.Tools != nil {
		any := false
		for _, v := range h.Capabilities {
			if len(v) > 0 {
				any = true
			}
		}
		if !any {
			rep.errf(at, "fields.tools is set but no capability maps to a native tool")
		}
	}
}

// ---------------- models.yaml ----------------

func usesCatalog(r *config.Registry) bool {
	for _, h := range r.Harnesses {
		if h.Catalog != nil {
			return true
		}
	}
	return false
}

func checkModels(r *config.Registry, m *config.Models, rep *Report) {
	const w = config.ModelsFile
	if m.Version != config.SchemaVersion {
		rep.errf(w, "version must be %d (got %d)", config.SchemaVersion, m.Version)
	}
	if v, err := time.Parse("2006-01-02", m.Fetched); err != nil {
		rep.errf(w, "fetched must be a YYYY-MM-DD date (got %q)", m.Fetched)
	} else if r.StaleAfterDays > 0 {
		if age := int(Now().Sub(v).Hours() / 24); age > r.StaleAfterDays {
			rep.warnf(w, "model list is %d days old (limit %d): run `crew models sync`", age, r.StaleAfterDays)
		}
	}
	for _, hn := range sortedKeys(r.Harnesses) {
		if r.Harnesses[hn].Catalog == nil {
			continue
		}
		if len(m.Harnesses[hn]) == 0 {
			rep.errf(w, "no models listed for %s: run `crew models sync`", hn)
		}
	}
	for _, hn := range sortedKeys(m.Harnesses) {
		h, ok := r.Harnesses[hn]
		if !ok || h.Catalog == nil {
			rep.warnf(w, "lists models for %q, which has no catalog in %s: run `crew models sync`", hn, config.RegistryFile)
		}
		seen := map[string]bool{}
		for _, e := range m.Harnesses[hn] {
			if e.ID == "" {
				rep.errf(w, "%s: a model has an empty id", hn)
			} else if seen[e.ID] {
				rep.errf(w, "%s: model %q listed twice", hn, e.ID)
			}
			seen[e.ID] = true
		}
	}
}

// ---------------- sources ----------------

func checkSources(p *Project, rep *Report) {
	r := p.Registry
	capSet := setOf(r.Capabilities)
	skillNames := map[string]bool{}
	for _, s := range p.Skills {
		checkCommon(rep, s.Path, s.Name, s.Description, s.Body)
		skillNames[s.Name] = true
	}
	for _, a := range p.Agents {
		checkCommon(rep, a.Path, a.Name, a.Description, a.Body)
		if skillNames[a.Name] {
			rep.errf(a.Path, "name %q is also used by a skill; keep names unique across agents and skills", a.Name)
		}
		if a.Model == "" {
			rep.errf(a.Path, "model is required: an id from %s (see `crew models`) or %q", config.ModelsFile, source.ModelInherit)
		}
		for _, hn := range sortedKeys(a.PerHarness) {
			if _, ok := r.Harnesses[hn]; !ok {
				rep.errf(a.Path, "per_harness: unknown harness %q", hn)
			} else if c := a.PerHarness[hn]; c.Model == "" && c.Effort == "" {
				rep.errf(a.Path, "per_harness.%s: set model, effort or both", hn)
			}
		}
		if len(a.Capabilities) == 0 {
			rep.errf(a.Path, "capabilities must be listed (use [all] to inherit everything)")
		}
		seen := map[string]bool{}
		for _, c := range a.Capabilities {
			if seen[c] {
				rep.errf(a.Path, "capability %q listed twice", c)
			}
			seen[c] = true
			if c == source.CapAll {
				if len(a.Capabilities) > 1 {
					rep.errf(a.Path, "\"all\" must be the only capability when used")
				}
			} else if !capSet[c] {
				rep.errf(a.Path, "unknown capability %q (known: %s)", c, strings.Join(r.Capabilities, ", "))
			}
		}
		seen = map[string]bool{}
		for _, s := range a.Skills {
			if seen[s] {
				rep.errf(a.Path, "skill %q listed twice", s)
			}
			seen[s] = true
			if !skillNames[s] {
				rep.errf(a.Path, "references unknown skill %q", s)
			}
		}
	}
}

func checkCommon(rep *Report, path, name, desc, body string) {
	stem := source.DefinitionStem(path)
	what := "filename"
	if filepath.Base(path) == source.SkillFile {
		what = "folder name"
	}
	if name == "" {
		rep.errf(path, "name is required")
	} else {
		if !nameRe.MatchString(name) {
			rep.errf(path, "name %q must be lowercase kebab-case", name)
		}
		if name != stem {
			rep.errf(path, "name %q must match the %s %q", name, what, stem)
		}
	}
	if strings.TrimSpace(desc) == "" {
		rep.errf(path, "description is required")
	} else if strings.ContainsAny(desc, "\n\r") {
		rep.errf(path, "description must be a single line")
	} else if strings.ContainsAny(desc, "<>") {
		rep.errf(path, "description must not contain '<' or '>'")
	}
	if strings.TrimSpace(body) == "" {
		rep.errf(path, "body (the instructions after the frontmatter) is empty")
	}
}

// ---------------- crew ----------------

func checkCrew(p *Project, rep *Report) {
	const w = config.CrewFile
	r, lo := p.Registry, p.Crew
	if lo.Version != config.SchemaVersion {
		rep.errf(w, "version must be %d (got %d)", config.SchemaVersion, lo.Version)
	}
	if len(lo.Targets) == 0 {
		rep.errf(w, "targets must list at least one harness")
	}
	used := map[string]bool{}

	for _, hn := range sortedKeys(lo.Targets) {
		t := lo.Targets[hn]
		at := fmt.Sprintf("%s targets.%s", w, hn)
		h, ok := r.Harnesses[hn]
		if !ok {
			rep.errf(at, "unknown harness (not in %s)", config.RegistryFile)
			continue
		}
		if t.Enabled == nil {
			rep.errf(at, "enabled must be set explicitly to true or false")
			continue
		}
		if !t.IsEnabled() {
			continue
		}
		p.Enabled = append(p.Enabled, hn)

		if !h.Agents.Supported {
			if len(t.Agents) > 0 || len(t.Exclude) > 0 {
				rep.errf(at, "%s has no custom agents; remove agents/exclude", h.Display)
			}
			continue
		}
		include := matchAll(rep, at+".agents", t.Agents, p.Agents)
		exclude := matchAll(rep, at+".exclude", t.Exclude, p.Agents)
		var sel []Selected
		for _, a := range p.Agents {
			if !include[a.Name] || exclude[a.Name] {
				continue
			}
			sel = append(sel, resolveModel(rep, p.Models, hn, h, a))
			used[a.Name] = true
		}
		if len(t.Agents) == 0 {
			rep.infof(at, "no agents selected (skills are still compiled)")
		}
		p.Selection[hn] = sel

		// per-harness rules on what it receives
		for _, s := range sel {
			checkRules(rep, s.Agent.Path, hn, h.Rules, s.Agent.Name, s.Agent.Description)
			noteMissingTools(rep, r, hn, h, s.Agent)
			if len(s.Agent.Skills) > 0 && h.Agents.Fields.Skills == "" {
				rep.infof(s.Agent.Path, "%s can't preload skills per agent; skills stay globally available there", h.Display)
			}
		}
	}
	for _, hn := range p.Enabled {
		h := r.Harnesses[hn]
		for _, s := range p.Skills {
			checkRules(rep, s.Path, hn, h.Rules, s.Name, s.Description)
		}
	}
	sort.Strings(p.Enabled)
	if len(p.Enabled) == 0 {
		rep.errf(w, "no harness is enabled")
	}
	for _, hn := range sortedKeys(r.Harnesses) {
		if _, ok := lo.Targets[hn]; !ok {
			rep.infof(w, "harness %q is defined but not targeted", hn)
		}
	}
	for _, a := range p.Agents {
		if !used[a.Name] {
			rep.infof(a.Path, "not compiled for any enabled harness")
		}
	}
}

// resolveModel picks the agent's model and effort for harness hn and checks
// them against that harness's list in models.yaml.
func resolveModel(rep *Report, models *config.Models, hn string, h config.Harness, a source.Agent) Selected {
	c := a.ChoiceFor(hn)
	sel := Selected{Agent: a, Model: c.Model, Effort: c.Effort}
	f := h.Agents.Fields
	switch {
	case c.Model == "":
		return sel // reported by checkSources
	case c.Model == source.ModelInherit:
		if c.Effort == "" {
			return sel
		}
		if f.Effort == "" { // no key, or only a model suffix with no model to hang it on
			rep.infof(a.Path, "[%s] %s can't set an effort without a model; effort %q is ignored", hn, h.Display, c.Effort)
			sel.Effort = ""
		} else if models != nil && h.Catalog != nil && !anyModelAccepts(models.Harnesses[hn], c.Effort) {
			rep.errf(a.Path, "[%s] effort %q isn't accepted by any %s model (see `crew models --harness %s`)", hn, c.Effort, h.Display, hn)
		}
		return sel
	case h.Catalog == nil:
		rep.errf(a.Path, "[%s] %s agents can't pin a model; set per_harness.%s.model: %s", hn, h.Display, hn, source.ModelInherit)
		return sel
	case models == nil:
		return sel // models.yaml is missing; already reported
	}
	if c.Effort != "" && f.Effort == "" && f.EffortSuffix == "" {
		rep.infof(a.Path, "[%s] %s has no per-agent effort setting; effort %q is ignored", hn, h.Display, c.Effort)
		sel.Effort = ""
		c.Effort = ""
	}
	e, ok := models.Find(hn, c.Model)
	if !ok {
		rep.errf(a.Path, "[%s] model %q isn't available in %s; pick one from `crew models --harness %s` (set it under per_harness.%s if other harnesses use a different model)",
			hn, c.Model, h.Display, hn, hn)
		return sel
	}
	sel.Entry = &e
	if c.Effort != "" && !e.SupportsEffort(c.Effort) {
		accepted := "none, so drop effort"
		if len(e.Efforts) > 0 {
			accepted = strings.Join(e.Efforts, ", ")
		}
		rep.errf(a.Path, "[%s] %s doesn't accept effort %q (accepted: %s)", hn, c.Model, c.Effort, accepted)
	}
	return sel
}

// noteMissingTools flags capabilities an agent asks for that this harness
// has no tool for. Only harnesses with a per-agent tool list are checked;
// the others (Codex, Cursor) only derive a sandbox/readonly flag.
func noteMissingTools(rep *Report, r *config.Registry, hn string, h config.Harness, a source.Agent) {
	if h.Agents.Fields.Tools == nil || a.InheritsAll() {
		return
	}
	for _, c := range a.Capabilities {
		if len(h.Capabilities[c]) == 0 {
			rep.infof(a.Path, "[%s] %s has no %s tool, so it's dropped there (see `crew capabilities`)", hn, c, h.Display)
		}
	}
}

func anyModelAccepts(es []config.ModelEntry, effort string) bool {
	for _, e := range es {
		if e.SupportsEffort(effort) {
			return true
		}
	}
	return false
}

func checkRules(rep *Report, path, hn string, ru config.Rules, name, desc string) {
	max := ru.NameMax
	if max == 0 {
		max = 64
	}
	if len(name) > max {
		rep.errf(path, "[%s] name is %d chars (max %d)", hn, len(name), max)
	}
	for _, bad := range ru.NameForbidden {
		if strings.Contains(strings.ToLower(name), strings.ToLower(bad)) {
			rep.errf(path, "[%s] name must not contain %q", hn, bad)
		}
	}
	if ru.DescriptionMax > 0 && len(desc) > ru.DescriptionMax {
		rep.errf(path, "[%s] description is %d chars (max %d)", hn, len(desc), ru.DescriptionMax)
	}
}

// matchAll resolves names/globs. A plain name must exist; a glob must match something.
func matchAll(rep *Report, at string, pats []string, agents []source.Agent) map[string]bool {
	out := map[string]bool{}
	for _, pat := range pats {
		if _, err := filepath.Match(pat, ""); err != nil {
			rep.errf(at, "bad pattern %q: %v", pat, err)
			continue
		}
		n := 0
		for _, a := range agents {
			if ok, _ := filepath.Match(pat, a.Name); ok {
				out[a.Name] = true
				n++
			}
		}
		if n == 0 {
			if strings.ContainsAny(pat, "*?[") {
				rep.errf(at, "pattern %q matches no agents", pat)
			} else {
				rep.errf(at, "unknown agent %q", pat)
			}
		}
	}
	return out
}

// ---------------- helpers ----------------

func scalarOK(v any) bool {
	switch x := v.(type) {
	case string, bool, int, int64, float64:
		return true
	case []any:
		for _, e := range x {
			if _, ok := e.(string); !ok {
				return false
			}
		}
		return true
	}
	return false
}

func setOf(xs []string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}

func sortedKeys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
