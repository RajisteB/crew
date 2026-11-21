// Package build turns a verified project into files for each harness and
// writes them safely: it only ever overwrites or deletes files it wrote
// itself (tracked in .crew/state.*.json), unless --force is given.
//
// In user scope every harness points back at the crew directory: each
// skill is a link to skills/<name>/, and each agent is a link to a file
// generated under build/<harness>/. Skill edits are live; agent edits need a
// compile. Project scope writes real copies, since a repo can't commit links
// into someone's home directory.
package build

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/crewfile-dev/crew/internal/factory"
	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
	"github.com/crewfile-dev/crew/internal/verify"
)

type Opts struct {
	Scope      string // "user" | "project"
	ProjectDir string // required for scope=project
	Out        string // optional staging root; nothing outside it is touched
	Force      bool
}

type File struct {
	Path      string
	Content   []byte // regular file contents; unused when Link is set
	Link      string // when set, Path is a symlink to this absolute path
	Root      string // the folder this output lives under (a harness's skills/ or agents/, or build/<harness>/agents)
	Kind      string // "skill" | "agent" | "agent-build" | "factory-diagram" | "orchestrator-settings"
	Name      string
	Harnesses []string
}

type Note struct{ Harness, Msg string }

// want is the fingerprint recorded for an output: a content hash, or the
// link target.
func (f File) want() string {
	if f.Link != "" {
		return "link:" + f.Link
	}
	return hashOf(f.Content)
}

// BuildDir is where generated agent files live in user scope.
func BuildDir(crewDir string, o Opts) string {
	if o.Out != "" {
		return filepath.Join(o.Out, "crew-build")
	}
	return filepath.Join(crewDir, "build")
}

// Plan computes every file that should exist. It does not touch the disk.
func Plan(p *verify.Project, o Opts) ([]File, []Note, error) {
	crewDir, err := filepath.Abs(p.Dir)
	if err != nil {
		return nil, nil, err
	}
	link := o.Scope != "project"
	byPath := map[string]*File{}
	var order []string
	var notes []Note
	var errs []string

	put := func(f File) {
		if prev, ok := byPath[f.Path]; ok {
			if !bytes.Equal(prev.Content, f.Content) || prev.Link != f.Link {
				errs = append(errs, fmt.Sprintf("%s: %s %q and %s %q would write different content to the same file",
					f.Path, strings.Join(prev.Harnesses, "+"), prev.Name, strings.Join(f.Harnesses, "+"), f.Name))
				return
			}
			prev.Harnesses = append(prev.Harnesses, f.Harnesses...)
			return
		}
		cp := f
		byPath[f.Path] = &cp
		order = append(order, f.Path)
	}

	enabled := map[string]bool{}
	for _, h := range p.Enabled {
		enabled[h] = true
	}

	for _, hn := range p.Enabled {
		h := p.Registry.Harnesses[hn]

		// Skills are global: every enabled harness gets all of them.
		shared := ""
		for _, o := range h.Skills.SharesWith {
			if enabled[o] {
				shared = o
				break
			}
		}
		if shared != "" {
			notes = append(notes, Note{hn, fmt.Sprintf("skills not written: %s already loads %s's skill directory", h.Display, shared)})
		} else if len(p.Skills) > 0 {
			dir, err := resolve(h.Skills.UserDir, h.Skills.ProjectDir, o)
			if err != nil {
				return nil, nil, err
			}
			for _, s := range p.Skills {
				files, err := skillOutputs(crewDir, dir, h.Skills.Layout, s, link, hn)
				if err != nil {
					return nil, nil, fmt.Errorf("%s: %w", s.Path, err)
				}
				for _, f := range files {
					put(f)
				}
			}
		}

		if len(p.Factories) > 0 && !canHostForeman(h) {
			notes = append(notes, Note{hn, fmt.Sprintf("no foreman: %s can't host a main-session agent with shell (crew run still works)", h.Display)})
		}
		if canHostForeman(h) && hasOrchestratorLaunch(h) && !run.CanDeny(h) {
			notes = append(notes, Note{hn, fmt.Sprintf("%s can't apply deny rules, so the orchestrator isn't built for it", h.Display)})
		}
		if !h.Agents.Supported {
			continue
		}
		dir, err := resolve(h.Agents.UserDir, h.Agents.ProjectDir, o)
		if err != nil {
			return nil, nil, err
		}
		if len(p.Factories) > 0 && canHostForeman(h) {
			fs, err := foremanOutputs(p, hn, crewDir, dir, link, o)
			if err != nil {
				return nil, nil, err
			}
			for _, f := range fs {
				put(f)
			}
		}
		if canHostOrchestrator(h) {
			fs, err := orchestratorOutputs(p, hn, crewDir, dir, link, o)
			if err != nil {
				return nil, nil, err
			}
			for _, f := range fs {
				put(f)
			}
		}
		sel := p.Selection[hn]
		if len(sel) == 0 {
			continue
		}
		for _, s := range sel {
			content, err := renderAgent(p.Registry, h, s)
			if err != nil {
				return nil, nil, fmt.Errorf("%s [%s]: %w", s.Agent.Path, hn, err)
			}
			dest := filepath.Join(dir, s.Agent.Name+h.Agents.Ext)
			if !link {
				put(File{Path: dest, Root: dir, Content: content, Kind: "agent", Name: s.Agent.Name, Harnesses: []string{hn}})
				continue
			}
			built := filepath.Join(BuildDir(crewDir, o), hn, "agents", s.Agent.Name+h.Agents.Ext)
			put(File{Path: built, Root: filepath.Dir(built), Content: content, Kind: "agent-build", Name: s.Agent.Name, Harnesses: []string{hn}})
			put(File{Path: dest, Root: dir, Link: built, Kind: "agent", Name: s.Agent.Name, Harnesses: []string{hn}})
		}
	}
	for _, f := range p.Factories {
		dir := filepath.Join(BuildDir(crewDir, o), "factories")
		put(File{Path: filepath.Join(dir, f.Name+".md"), Root: dir, Content: []byte(factory.DiagramDoc(f)),
			Kind: "factory-diagram", Name: f.Name, Harnesses: []string{}})
	}
	if len(errs) > 0 {
		return nil, nil, errors.New(strings.Join(errs, "\n"))
	}
	out := make([]File, 0, len(order))
	for _, k := range order {
		out = append(out, *byPath[k])
	}
	return out, notes, nil
}

// skillOutputs is one link to the skill folder (user scope) or a copy of
// every file in it (project scope). A "flat" layout gets just SKILL.md.
func skillOutputs(crewDir, dir, layout string, s source.Skill, link bool, hn string) ([]File, error) {
	src := filepath.Join(crewDir, s.Dir())
	mk := func(path string) File {
		return File{Path: path, Root: dir, Kind: "skill", Name: s.Name, Harnesses: []string{hn}}
	}
	if layout != "dir" {
		f := mk(filepath.Join(dir, s.Name+".md"))
		if link {
			f.Link = filepath.Join(src, source.SkillFile)
			return []File{f}, nil
		}
		b, err := os.ReadFile(filepath.Join(src, source.SkillFile))
		f.Content = b
		return []File{f}, err
	}
	if link {
		f := mk(filepath.Join(dir, s.Name))
		f.Link = src
		return []File{f}, nil
	}
	rels, err := source.SkillFiles(src)
	if err != nil {
		return nil, err
	}
	out := make([]File, 0, len(rels))
	for _, r := range rels {
		b, err := os.ReadFile(filepath.Join(src, r))
		if err != nil {
			return nil, err
		}
		f := mk(filepath.Join(dir, s.Name, r))
		f.Content = b
		out = append(out, f)
	}
	return out, nil
}

func resolve(userDir, projectDir string, o Opts) (string, error) {
	switch o.Scope {
	case "", "user":
		if strings.HasPrefix(userDir, "~/") || userDir == "~" {
			rest := strings.TrimPrefix(strings.TrimPrefix(userDir, "~"), "/")
			if o.Out != "" {
				return filepath.Join(o.Out, "home", rest), nil
			}
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			return filepath.Join(home, rest), nil
		}
		if o.Out != "" {
			return filepath.Join(o.Out, "root", userDir), nil
		}
		return userDir, nil
	case "project":
		if o.ProjectDir == "" {
			return "", errors.New("--project is required with --scope project")
		}
		if o.Out != "" {
			return filepath.Join(o.Out, "project", projectDir), nil
		}
		abs, err := filepath.Abs(o.ProjectDir)
		if err != nil {
			return "", err
		}
		return filepath.Join(abs, projectDir), nil
	}
	return "", fmt.Errorf("unknown scope %q (use user or project)", o.Scope)
}
