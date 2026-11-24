package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"syscall"

	"github.com/crewfile-dev/crew/internal/build"
	"github.com/crewfile-dev/crew/internal/verify"
)

const orchestratorUsage = `usage:
  crew orchestrator [--dir D] [--harness H] [--print] [-- ARGS...]
  --harness H   start the orchestrator on this harness (default: the first enabled one that has one)
  --print       show the line that starts the orchestrator, and start nothing
  -- ARGS       passed to the harness as they are`

// orchestratorPlan is what `crew orchestrator` runs: the harness it picked
// and the command, with any extra arguments at the end.
type orchestratorPlan struct {
	harness string
	argv    []string
}

// defaultOrchestratorHarness is the first enabled harness that gets an
// orchestrator, or "" when none does.
func defaultOrchestratorHarness(p *verify.Project, o build.Opts) string {
	for _, hn := range p.Enabled {
		if _, ok := build.OrchestratorLaunch(p, hn, o); ok {
			return hn
		}
	}
	return ""
}

// orchestratorHint is the short command that starts the orchestrator on
// harness hn, when def is the harness crew orchestrator picks by default.
func orchestratorHint(hn, def string) string {
	if hn == def {
		return "crew orchestrator"
	}
	return "crew orchestrator --harness " + hn
}

// planOrchestrator decides what to run. It starts nothing.
func planOrchestrator(p *verify.Project, harness string, extra []string, o build.Opts) (orchestratorPlan, error) {
	hn := harness
	if hn == "" {
		if hn = defaultOrchestratorHarness(p, o); hn == "" {
			return orchestratorPlan{}, errors.New("no enabled harness can host the orchestrator (see the notes from crew compile)")
		}
	}
	argv, ok := build.OrchestratorArgv(p, hn, o)
	if !ok || !slices.Contains(p.Enabled, hn) {
		return orchestratorPlan{}, fmt.Errorf("%s has no orchestrator (see the notes from crew compile)", hn)
	}
	if _, err := os.Stat(build.OrchestratorSettingsPath(p, hn, o)); err != nil {
		return orchestratorPlan{}, errors.New("the orchestrator isn't compiled yet; run crew compile")
	}
	return orchestratorPlan{harness: hn, argv: append(argv, extra...)}, nil
}

// orchestratorCmd handles `crew orchestrator …`. start runs the command; it
// is not called with --print.
func orchestratorCmd(args []string, stdout io.Writer, start func(argv []string) error) error {
	fs := flag.NewFlagSet("orchestrator", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirFlag := fs.String("dir", "", "crew directory")
	harness := fs.String("harness", "", "harness")
	printOnly := fs.Bool("print", false, "show the launch line")
	own, extra := args, []string(nil)
	if i := slices.Index(args, "--"); i >= 0 {
		own, extra = args[:i], args[i+1:]
	}
	if err := fs.Parse(own); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(stdout, orchestratorUsage)
			return nil
		}
		return usageError{err.Error() + "\n" + orchestratorUsage}
	}
	if fs.NArg() > 0 {
		return usageError{"unexpected arguments: " + strings.Join(fs.Args(), " ")}
	}
	dir, err := resolveDir(*dirFlag, false)
	if err != nil {
		return usageError{err.Error()}
	}
	p, err := verifiedProject(dir, os.Stderr)
	if err != nil {
		return err
	}
	plan, err := planOrchestrator(p, *harness, extra, build.Opts{})
	if err != nil {
		return err
	}
	if *printOnly {
		line, _ := build.OrchestratorLaunch(p, plan.harness, build.Opts{})
		fmt.Fprintln(stdout, line)
		return nil
	}
	return start(plan.argv)
}

// execOrchestrator replaces this process with the harness, so the harness
// owns the terminal. It returns only when it can't start it.
func execOrchestrator(argv []string) error {
	path, err := exec.LookPath(argv[0])
	if err != nil {
		return fmt.Errorf("can't find %s on PATH", argv[0])
	}
	if err := syscall.Exec(path, argv, os.Environ()); err != nil {
		return fmt.Errorf("can't start %s: %v", argv[0], err)
	}
	return nil
}
