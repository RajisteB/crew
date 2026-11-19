package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
	"github.com/crewfile-dev/crew/internal/verify"
)

const runUsage = `usage:
  crew run <factory> --input TEXT|@FILE [--project P] [--detach]   start a run
  crew run status [<id>] [--since N]       list runs, or show one run and its events after N
  crew run say <id> <message…>             pass a message to the next station
  crew run goto <id> <station>             send the work to a station
  crew run approve <id> [--goto S] [--detach]   continue a run waiting for approval
  crew run resume <id> [--detach]          continue a stopped, failed or interrupted run
common flags: --dir D (crew directory), --project P (default: current directory)`

// usageError exits 2 instead of 1.
type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

type runFlags struct {
	dir, project, input, gotoStation string
	since                            int
	detach                           bool
}

// runCmd handles `crew run …` and returns the process exit code.
func runCmd(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	var f runFlags
	fs.StringVar(&f.dir, "dir", "", "crew directory")
	fs.StringVar(&f.project, "project", "", "project directory")
	fs.StringVar(&f.input, "input", "", "run input: text, or @file")
	fs.StringVar(&f.gotoStation, "goto", "", "approve: continue at this station instead")
	fs.IntVar(&f.since, "since", 0, "status: only events after this cursor")
	fs.BoolVar(&f.detach, "detach", false, "keep running in the background")
	pos, err := parseInterleaved(fs, args)
	if errors.Is(err, flag.ErrHelp) {
		fmt.Fprintln(stdout, runUsage)
		return 0
	}
	if err == nil && len(pos) == 0 {
		err = usageError{runUsage}
	}
	if err == nil {
		err = dispatchRun(pos, f, stdout)
	}
	var ue usageError
	switch {
	case err == nil:
		return 0
	case errors.As(err, &ue):
		fmt.Fprintf(stderr, "crew: %s\n", ue.msg)
		return 2
	case errors.Is(err, errRunStopped):
		return 1
	}
	fmt.Fprintf(stderr, "crew: %v\n", err)
	return 1
}

// errRunStopped: the run ended failed or interrupted; events already said why.
var errRunStopped = errors.New("run stopped")

func dispatchRun(pos []string, f runFlags, out io.Writer) error {
	project, err := projectDir(f.project)
	if err != nil {
		return err
	}
	f.project = project
	need := func(n int) error {
		if len(pos) < n {
			return usageError{runUsage}
		}
		return nil
	}
	switch pos[0] {
	case "status":
		return runStatus(pos[1:], f, out)
	case "say":
		if err := need(3); err != nil {
			return err
		}
		return runSay(pos[1], strings.Join(pos[2:], " "), f, out)
	case "goto":
		if err := need(3); err != nil {
			return err
		}
		return runGoto(pos[1], pos[2], f, out)
	case "approve":
		if err := need(2); err != nil {
			return err
		}
		return runApprove(pos[1], f, out)
	case "resume":
		if err := need(2); err != nil {
			return err
		}
		return continueRun(pos[1], f, out)
	}
	if len(pos) != 1 {
		return usageError{runUsage}
	}
	return startRun(pos[0], f, out)
}

func projectDir(p string) (string, error) {
	if p == "" {
		return os.Getwd()
	}
	return filepath.Abs(expand(p))
}

func startRun(name string, f runFlags, out io.Writer) error {
	input, err := readInput(f.input)
	if err != nil {
		return err
	}
	dir, err := resolveDir(f.dir, false)
	if err != nil {
		return err
	}
	p, err := verifiedProject(dir, out)
	if err != nil {
		return err
	}
	fac, err := findFactory(p.Factories, name)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(filepath.Join(dir, fac.Path))
	if err != nil {
		return err
	}
	store, st, err := run.NewRun(f.project, fac, src, input, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "run %s\n", st.RunID)
	return driveOrDetach(store, st.RunID, dir, f, out)
}

func readInput(in string) (string, error) {
	if strings.TrimSpace(in) == "" {
		return "", usageError{"--input is required (text, or @file)"}
	}
	if strings.HasPrefix(in, "@") {
		b, err := os.ReadFile(expand(in[1:]))
		if err != nil {
			return "", fmt.Errorf("--input: %w", err)
		}
		return string(b), nil
	}
	return in, nil
}

// verifiedProject runs verify and refuses to start a run on any error or
// warning, printing the report the way `crew verify` does.
func verifiedProject(dir string, out io.Writer) (*verify.Project, error) {
	p, rep := verify.Run(dir)
	if rep.OK(false) {
		return p, nil
	}
	for _, i := range rep.Issues {
		if i.Level != verify.Info {
			fmt.Fprintf(out, "%-5s  %s: %s\n", i.Level, i.Where, i.Msg)
		}
	}
	return nil, errors.New("verify failed; fix the issues above (crew verify) before running")
}

// driveOrDetach runs the engine here, or hands the run to a background
// `crew run resume` whose output goes to <run>/run.log.
func driveOrDetach(store run.Store, id, dir string, f runFlags, out io.Writer) error {
	if !f.detach {
		return drive(store, dir, f.project, out)
	}
	logf, err := os.OpenFile(filepath.Join(store.Dir, "run.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logf.Close()
	self, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(self, "run", "resume", id, "--project", f.project, "--dir", dir)
	cmd.Stdout, cmd.Stderr, cmd.Dir = logf, logf, f.project
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	fmt.Fprintf(out, "detached (pid %d); follow with: crew run status %s --since 0\n", cmd.Process.Pid, id)
	return cmd.Process.Release()
}

func drive(store run.Store, dir, project string, out io.Writer) error {
	fac, err := store.Factory()
	if err != nil {
		return err
	}
	p, _ := verify.Run(dir) // a resume only needs the registry and agents to load
	if p.Registry == nil {
		return errors.New("harnesses.yaml failed to load; run crew verify")
	}
	agents := map[string]source.Agent{}
	for _, a := range p.Agents {
		agents[a.Name] = a
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	e := &run.Engine{
		Store: store, Factory: fac, Agents: agents, Registry: p.Registry,
		Runner: run.HeadlessRunner{Registry: p.Registry}, Snap: run.Snapshot, Project: project, Out: out,
	}
	st, err := e.Drive(ctx)
	switch {
	case errors.Is(err, context.Canceled):
		return errRunStopped
	case err != nil:
		return err
	case st.Status == run.StatusFailed || st.Status == run.StatusInterrupted:
		return errRunStopped
	}
	return nil
}
