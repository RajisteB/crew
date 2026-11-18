// Command crewfile compiles harness-agnostic agents and skills into the native
// formats of Claude Code, Codex, OpenCode, Gemini CLI, Cursor, etc.
//
//	crewfile init     [--dir D]
//	crewfile verify   [--dir D] [-v] [--allow-warnings]
//	crewfile models   [--dir D] [--harness H | --all]
//	crewfile models sync [--dir D]
//	crewfile capabilities [--dir D] [--harness H | --all]
//	crewfile plan     [--dir D] [scope flags]
//	crewfile compile  [--dir D] [scope flags] [--dry-run] [--force] [--allow-warnings]
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/crewfile-dev/crewfile/internal/build"
	"github.com/crewfile-dev/crewfile/internal/config"
	"github.com/crewfile-dev/crewfile/internal/scaffold"
	"github.com/crewfile-dev/crewfile/internal/verify"
)

const usage = `crewfile — compile agents and skills across AI coding harnesses

Usage:
  crewfile init     [--dir D]                 scaffold a crewfile directory
  crewfile verify   [--dir D] [-v]            strict check of config + sources
  crewfile models   [--harness H | --all]     list every model + effort each harness can run
  crewfile models sync                        refresh models.yaml from the catalog (models.dev)
  crewfile capabilities [--harness H | --all] which native tools each capability becomes
  crewfile plan     [--dir D] [scope flags]   show what each harness gets, models, costs, notes, file changes
  crewfile compile  [--dir D] [scope flags]   verify, then write files

Scope flags:
  --scope user|project   write to ~/... (default) or into a project's dot-dirs
  --project PATH         project root for --scope project
  --out DIR              stage everything under DIR instead of the real locations

Other flags:
  --dry-run              (compile) show actions without writing
  --force                (compile) overwrite/delete files crewfile doesn't own or that were hand-edited
  --allow-warnings       don't fail on warnings (errors still fail)
  -v                     also print info notes

Directory resolution: --dir, then $CREWFILE_HOME, then the current directory if it
contains crewfile.yaml, then ~/crewfile.
`

type flags struct {
	dir, scope, project, out string
	dryRun, force, allowWarn bool
	verbose                  bool
	harness                  string
	all                      bool
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	if cmd == "-h" || cmd == "--help" || cmd == "help" {
		fmt.Print(usage)
		return
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	var f flags
	fs.StringVar(&f.dir, "dir", "", "crewfile directory")
	fs.StringVar(&f.scope, "scope", "user", "user or project")
	fs.StringVar(&f.project, "project", "", "project root (scope=project)")
	fs.StringVar(&f.out, "out", "", "staging directory")
	fs.BoolVar(&f.dryRun, "dry-run", false, "don't write")
	fs.BoolVar(&f.force, "force", false, "take over conflicting files")
	fs.BoolVar(&f.allowWarn, "allow-warnings", false, "don't fail on warnings")
	fs.BoolVar(&f.verbose, "v", false, "print info notes")
	fs.StringVar(&f.harness, "harness", "", "models/capabilities: only this harness")
	fs.BoolVar(&f.all, "all", false, "models/capabilities: every harness, not just enabled ones")
	fs.Usage = func() { fmt.Fprint(os.Stderr, usage) }
	rest, sub := os.Args[2:], []string(nil)
	if cmd == "models" && len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
		sub, rest = rest[:1], rest[1:] // `models sync --dir D`: flags may follow the subcommand
	}
	if err := fs.Parse(rest); err != nil {
		os.Exit(2)
	}
	if fs.NArg() > 0 {
		fail(2, "unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	dir, err := resolveDir(f.dir, cmd == "init")
	if err != nil {
		fail(2, "%v", err)
	}

	switch cmd {
	case "init":
		created, err := scaffold.Init(dir)
		if err != nil {
			fail(1, "%v", err)
		}
		fmt.Printf("created %d files in %s\nnext: cd %s && crewfile verify && crewfile plan\n", len(created), dir, dir)
	case "capabilities":
		runCapabilities(dir, f)
	case "models":
		runModels(dir, sub, f)
	case "verify":
		p, _ := runVerify(dir, f)
		if _, _, err := build.Plan(p, opts(f)); err != nil {
			fail(1, "output collision:\n%v", err)
		}
	case "plan":
		withNotes := f
		withNotes.verbose = true // plan is for reviewing, so show every note
		p, _ := runVerify(dir, withNotes)
		printMatrix(p)
		files, notes, err := build.Plan(p, opts(f))
		if err != nil {
			fail(1, "%v", err)
		}
		acts, err := build.Diff(build.StatePath(dir, opts(f)), files, f.force)
		if err != nil {
			fail(1, "%v", err)
		}
		printNotes(notes)
		printActions(acts, true)
	case "compile":
		p, _ := runVerify(dir, f)
		files, notes, err := build.Plan(p, opts(f))
		if err != nil {
			fail(1, "%v", err)
		}
		state := build.StatePath(dir, opts(f))
		acts, err := build.Diff(state, files, f.force)
		if err != nil {
			fail(1, "%v", err)
		}
		printNotes(notes)
		printActions(acts, f.verbose || f.dryRun)
		if f.dryRun {
			fmt.Println("dry run: nothing written")
			return
		}
		if err := build.Apply(state, acts); err != nil {
			fail(1, "%v", err)
		}
		fmt.Println(summary(acts))
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func opts(f flags) build.Opts {
	return build.Opts{Scope: f.scope, ProjectDir: f.project, Out: f.out, Force: f.force}
}

// runVerify prints the report and exits non-zero if the project can't be compiled.
func runVerify(dir string, f flags) (*verify.Project, *verify.Report) {
	p, rep := verify.Run(dir)
	for _, i := range rep.Issues {
		if i.Level == verify.Info && !f.verbose {
			continue
		}
		where := i.Where
		if where != "" {
			where += ": "
		}
		fmt.Fprintf(os.Stderr, "%-5s  %s%s\n", i.Level, where, i.Msg)
	}
	e, w, n := rep.Count(verify.Error), rep.Count(verify.Warning), rep.Count(verify.Info)
	counts := fmt.Sprintf("%d errors, %d warnings, %d notes", e, w, n)
	if !rep.OK(f.allowWarn) {
		hint := ""
		if e == 0 {
			hint = " (warnings are fatal; fix them or pass --allow-warnings)"
		}
		fail(1, "verify failed: %s%s", counts, hint)
	}
	fmt.Fprintf(os.Stderr, "ok  %d agents, %d skills → %s  (%s)\n",
		len(p.Agents), len(p.Skills), strings.Join(p.Enabled, ", "), counts)
	return p, rep
}

func printMatrix(p *verify.Project) {
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, hn := range p.Enabled {
		h := p.Registry.Harnesses[hn]
		fmt.Fprintf(tw, "\n%s (%s) — %d skills\n", h.Display, hn, len(p.Skills))
		sel := p.Selection[hn]
		if !h.Agents.Supported {
			fmt.Fprintln(tw, "  no custom agents in this harness")
			continue
		}
		if len(sel) == 0 {
			fmt.Fprintln(tw, "  no agents selected")
			continue
		}
		fmt.Fprintln(tw, "  AGENT\tMODEL\tEFFORT\t$/MTok in · out")
		for _, s := range sel {
			price := "inherits session"
			if s.Entry != nil {
				price = fmt.Sprintf("%.2f · %.2f", s.Entry.InputPerMTok, s.Entry.OutputPerMTok)
			}
			eff := s.Effort
			if eff == "" {
				eff = "-"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", s.Agent.Name, s.Model, eff, price)
		}
	}
	tw.Flush()
	fmt.Println()
}

func printNotes(notes []build.Note) {
	for _, n := range notes {
		fmt.Printf("note   [%s] %s\n", n.Harness, n.Msg)
	}
}

func printActions(acts []build.Action, showSame bool) {
	home, _ := os.UserHomeDir()
	for _, a := range acts {
		if (a.Kind == build.Unchanged && !showSame) || (a.Kind == build.Forget && a.Reason == "") {
			continue
		}
		p := a.Path
		if home != "" && strings.HasPrefix(p, home+string(filepath.Separator)) {
			p = "~" + p[len(home):]
		}
		line := fmt.Sprintf("%-8s %s", a.Kind, p)
		if a.Reason != "" {
			line += "  — " + a.Reason
		}
		fmt.Println(line)
	}
	fmt.Println(summary(acts))
}

func summary(acts []build.Action) string {
	c := map[build.ActionKind]int{}
	for _, a := range acts {
		c[a.Kind]++
	}
	s := fmt.Sprintf("%d create, %d update, %d delete, %d unchanged", c[build.Create], c[build.Update], c[build.Delete], c[build.Unchanged])
	if c[build.Conflict] > 0 {
		s += fmt.Sprintf(", %d CONFLICT", c[build.Conflict])
	}
	return s
}

func resolveDir(flagDir string, forInit bool) (string, error) {
	pick := func(d string) (string, error) { return filepath.Abs(expand(d)) }
	if flagDir != "" {
		return pick(flagDir)
	}
	if env := os.Getenv("CREWFILE_HOME"); env != "" {
		return pick(env)
	}
	if !forInit {
		if _, err := os.Stat(config.CrewfileFile); err == nil {
			return pick(".")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("can't find home directory; pass --dir")
	}
	return filepath.Join(home, "crewfile"), nil
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

func fail(code int, f string, a ...any) {
	fmt.Fprintf(os.Stderr, "crewfile: "+f+"\n", a...)
	os.Exit(code)
}
