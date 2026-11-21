// Package source reads the harness-agnostic definitions in agents/ and
// skills/. Each agent is one Markdown file (agents/<name>.md); each skill is a
// folder holding one SKILL.md (skills/<name>/SKILL.md), the Agent Skills layout.
// Both use YAML frontmatter.
package source

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	AgentsDir    = "agents"
	SkillsDir    = "skills"
	FactoriesDir = "factories"
	// LegacyWorkflowsDir is the old name of factories/; verify points it out.
	LegacyWorkflowsDir = "workflows"
	// SkillFile is the one file inside each skills/<name>/ folder.
	SkillFile = "SKILL.md"
	// CapAll grants every capability and tells harnesses to inherit their full tool set.
	CapAll = "all"
	// ModelInherit means "don't pin a model; follow the session's model".
	ModelInherit = "inherit"
)

type Agent struct {
	Name         string                 `yaml:"name"`
	Description  string                 `yaml:"description"`
	Model        string                 `yaml:"model"`  // an id from models.yaml, or "inherit"
	Effort       string                 `yaml:"effort"` // optional; must be one the model accepts
	PerHarness   map[string]ModelChoice `yaml:"per_harness"`
	Capabilities []string               `yaml:"capabilities"`
	Skills       []string               `yaml:"skills"`

	Body string `yaml:"-"`
	Path string `yaml:"-"`
}

// ModelChoice is a model and effort for one harness. An empty field falls back
// to the agent's top-level value.
type ModelChoice struct {
	Model  string `yaml:"model"`
	Effort string `yaml:"effort"`
}

// ChoiceFor returns the model and effort the agent uses in harness hn.
func (a Agent) ChoiceFor(hn string) ModelChoice {
	c := ModelChoice{a.Model, a.Effort}
	if o, ok := a.PerHarness[hn]; ok {
		if o.Model != "" {
			c.Model = o.Model
			c.Effort = "" // a different model doesn't inherit an effort chosen for another one
		}
		if o.Effort != "" {
			c.Effort = o.Effort
		}
	}
	return c
}

func (a Agent) HasCap(c string) bool {
	for _, x := range a.Capabilities {
		if x == c || x == CapAll {
			return true
		}
	}
	return false
}

func (a Agent) InheritsAll() bool {
	for _, x := range a.Capabilities {
		if x == CapAll {
			return true
		}
	}
	return false
}

// Skill is a skills/<name>/ folder. Only name and description are read from
// SKILL.md; any other frontmatter keys (license, allowed-tools, metadata …)
// and any other files in the folder are passed through untouched.
type Skill struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`

	Body string `yaml:"-"`
	Path string `yaml:"-"` // skills/<name>/SKILL.md, relative to the crew dir
}

// Dir is the skill's folder relative to the crew dir.
func (s Skill) Dir() string { return filepath.Dir(s.Path) }

// FileError ties a problem to the source file it came from.
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string { return e.Path + ": " + e.Err.Error() }

// LoadAgents parses every agents/*.md. Parse failures are returned per file so
// verify can report all of them at once instead of stopping at the first.
func LoadAgents(root string) ([]Agent, []error) {
	var out []Agent
	var errs []error
	files, err := listMarkdown(filepath.Join(root, AgentsDir))
	if err != nil {
		return nil, []error{err}
	}
	for _, f := range files {
		var a Agent
		body, err := parse(f, &a)
		if err != nil {
			errs = append(errs, &FileError{rel(root, f), err})
			continue
		}
		a.Body, a.Path = body, rel(root, f)
		out = append(out, a)
	}
	return out, errs
}

func LoadSkills(root string) ([]Skill, []error) {
	var out []Skill
	var errs []error
	files, err := listSkillFiles(filepath.Join(root, SkillsDir))
	if err != nil {
		return nil, []error{err}
	}
	for _, f := range files {
		var s Skill
		body, err := parseLenient(f, &s)
		if err != nil {
			errs = append(errs, &FileError{rel(root, f), err})
			continue
		}
		s.Body, s.Path = body, rel(root, f)
		out = append(out, s)
	}
	return out, errs
}

// StrayEntries returns anything in dir that isn't a top-level .md file
// (subdirectories, other extensions). One file per definition is the rule.
func StrayEntries(root, sub string) []string {
	entries, err := os.ReadDir(filepath.Join(root, sub))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		if e.IsDir() || filepath.Ext(n) != ".md" {
			out = append(out, filepath.Join(sub, n))
		}
	}
	return out
}

// SkillLayoutIssues reports anything in skills/ that isn't a folder holding
// exactly one SKILL.md. Paths are relative to root.
func SkillLayoutIssues(root string) []LayoutIssue {
	dir := filepath.Join(root, SkillsDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []LayoutIssue
	for _, e := range entries {
		n := e.Name()
		if strings.HasPrefix(n, ".") {
			continue
		}
		where := filepath.Join(SkillsDir, n)
		if !isDir(filepath.Join(dir, n)) {
			out = append(out, LayoutIssue{where, "skills must be folders: move this to " + SkillsDir + "/<name>/" + SkillFile})
			continue
		}
		out = append(out, skillFolderIssues(root, where)...)
	}
	return out
}

// LayoutIssue is a misplaced or missing file in a source directory.
type LayoutIssue struct {
	Path string
	Msg  string
}

func skillFolderIssues(root, where string) []LayoutIssue {
	info, err := os.Stat(filepath.Join(root, where, SkillFile))
	if err != nil || !info.Mode().IsRegular() {
		return []LayoutIssue{{where, "missing " + SkillFile}}
	}
	return nil
}

// SkillFiles lists every file in a skill folder (relative to it), following
// a symlinked folder, skipping dotfiles and __pycache__.
func SkillFiles(dir string) ([]string, error) {
	var out []string
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		n := d.Name()
		if p != root && (strings.HasPrefix(n, ".") || n == "__pycache__") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		include := d.Type().IsRegular()
		if d.Type()&fs.ModeSymlink != 0 {
			ok, err := linkInside(root, p)
			if err != nil {
				return err
			}
			include = ok
		}
		if include {
			r, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out = append(out, r)
		}
		return nil
	})
	sort.Strings(out)
	return out, err
}

// linkInside reports whether the link at p resolves to a regular file inside
// root. A link to a folder is skipped; a link that leaves the skill folder is
// an error, because project copies get committed and must never pull in an
// outside file (a key in ~/.ssh, say).
func linkInside(root, p string) (bool, error) {
	target, err := filepath.EvalSymlinks(p)
	if err != nil {
		return false, fmt.Errorf("%s: broken link: %w", p, err)
	}
	if rel, err := filepath.Rel(root, target); err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("%s links outside the skill folder (to %s); copy the file in instead", p, target)
	}
	info, err := os.Stat(target)
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

// listSkillFiles returns skills/<name>/SKILL.md for every folder that has one.
func listSkillFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") || !isDir(filepath.Join(dir, e.Name())) {
			continue
		}
		f := filepath.Join(dir, e.Name(), SkillFile)
		if info, err := os.Stat(f); err == nil && info.Mode().IsRegular() {
			out = append(out, f)
		}
	}
	sort.Strings(out)
	return out, nil
}

// isDir follows symlinks, so a skill folder linked in from another repo counts.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// DefinitionStem is the name a definition's path implies: the folder name for
// skills/<name>/SKILL.md, the file name without .md for agents/<name>.md.
func DefinitionStem(path string) string {
	if filepath.Base(path) == SkillFile {
		return filepath.Base(filepath.Dir(path))
	}
	return strings.TrimSuffix(filepath.Base(path), ".md")
}

func listMarkdown(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || strings.HasPrefix(e.Name(), ".") || filepath.Ext(e.Name()) != ".md" {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func parse(path string, into any) (string, error) { return parseWith(path, into, true) }

// parseLenient ignores frontmatter keys the target type doesn't declare.
func parseLenient(path string, into any) (string, error) { return parseWith(path, into, false) }

func parseWith(path string, into any, strict bool) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	raw = bytes.ReplaceAll(raw, []byte("\r\n"), []byte("\n"))
	s := string(raw)
	if !strings.HasPrefix(s, "---\n") {
		return "", errors.New("must start with a '---' frontmatter block")
	}
	rest := s[4:]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n---") {
			end = len(rest) - 4
		} else {
			return "", errors.New("frontmatter block is not closed with '---'")
		}
	}
	fm := rest[:end]
	body := ""
	if end+5 <= len(rest) {
		body = rest[end+5:]
	}
	dec := yaml.NewDecoder(strings.NewReader(fm))
	dec.KnownFields(strict)
	if err := dec.Decode(into); err != nil {
		if errors.Is(err, io.EOF) {
			return "", errors.New("frontmatter is empty")
		}
		return "", fmt.Errorf("frontmatter: %s", friendly(err, into))
	}
	return strings.TrimSpace(body) + "\n", nil
}

var (
	unknownField = regexp.MustCompile(`field (\S+) not found in type (\S+)`)
	allowedKeys  = map[string]string{
		"source.Agent":   "name, description, model, effort, per_harness, capabilities, skills",
		"source.Factory": "name, description, start, rework, harness, watch, timeout, stations",
		"source.Domain":  "name, description, factories, paths, releases",
		"source.Station": "agent, harness, approve, on_drift, timeout, routes",
	}
)

// friendly rewrites yaml.v3's "field x not found in type source.Agent" into
// something that says what is allowed and where harness specifics belong.
func friendly(err error, into any) string {
	msg := unknownField.ReplaceAllStringFunc(err.Error(), func(m string) string {
		sub := unknownField.FindStringSubmatch(m)
		allowed, ok := allowedKeys[sub[2]]
		if !ok {
			allowed = "name, description"
		}
		hint := "; harness-specific settings belong in harnesses.yaml"
		if sub[2] != "source.Agent" && sub[2] != "source.Skill" {
			hint = ""
		}
		return fmt.Sprintf("unknown key %q (allowed: %s%s)", sub[1], allowed, hint)
	})
	if strings.Contains(msg, "did not find expected alphabetic or numeric character") {
		msg += ` (a bare * is YAML syntax; write max: "*" in quotes)`
	}
	return msg
}

func rel(root, p string) string {
	if r, err := filepath.Rel(root, p); err == nil {
		return r
	}
	return p
}
