package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/build"
	"github.com/crewfile-dev/crew/internal/verify"
)

// compiledDir is a scaffolded crew directory with the orchestrator settings
// file in place, as crew compile would leave it.
func compiledDir(t *testing.T) string {
	t.Helper()
	dir := scaffoldDir(t)
	p, _ := verify.Run(dir)
	path := build.OrchestratorSettingsPath(p, "claude-code", build.Opts{})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPlanOrchestrator_PicksTheFirstHarnessWithAnOrchestrator(t *testing.T) {
	dir := compiledDir(t)
	p, _ := verify.Run(dir)
	got, err := planOrchestrator(p, "", nil, build.Opts{})
	if err != nil {
		t.Fatal(err)
	}
	settings := build.OrchestratorSettingsPath(p, "claude-code", build.Opts{})
	want := []string{"claude", "--agent", "orchestrator", "--settings", settings}
	if got.harness != "claude-code" || !slices.Equal(got.argv, want) {
		t.Fatalf("plan = %+v; want argv %q", got, want)
	}
}

func TestPlanOrchestrator_AppendsExtraArgumentsInOrder(t *testing.T) {
	dir := compiledDir(t)
	p, _ := verify.Run(dir)
	got, err := planOrchestrator(p, "claude-code", []string{"--model", "opus", "a b"}, build.Opts{})
	if err != nil {
		t.Fatal(err)
	}
	if n := len(got.argv); n != 8 || !slices.Equal(got.argv[5:], []string{"--model", "opus", "a b"}) {
		t.Fatalf("argv = %q", got.argv)
	}
}

func TestPlanOrchestrator_HarnessWithoutAnOrchestrator(t *testing.T) {
	dir := compiledDir(t)
	p, _ := verify.Run(dir)
	for _, hn := range []string{"opencode", "no-such-harness"} {
		_, err := planOrchestrator(p, hn, nil, build.Opts{})
		want := hn + " has no orchestrator (see the notes from crew compile)"
		if err == nil || err.Error() != want {
			t.Errorf("%s: err = %v; want %q", hn, err, want)
		}
	}
}

func TestPlanOrchestrator_NoHarnessCanHostIt(t *testing.T) {
	dir := scaffoldDir(t)
	editFile(t, filepath.Join(dir, "harnesses.yaml"), `launch: "claude --agent {name} --settings {settings}"`, `launch: ""`)
	p, _ := verify.Run(dir)
	_, err := planOrchestrator(p, "", nil, build.Opts{})
	if want := "no enabled harness can host the orchestrator (see the notes from crew compile)"; err == nil || err.Error() != want {
		t.Fatalf("err = %v; want %q", err, want)
	}
}

func TestPlanOrchestrator_NotCompiledYet(t *testing.T) {
	p, _ := verify.Run(scaffoldDir(t))
	_, err := planOrchestrator(p, "", nil, build.Opts{})
	if want := "the orchestrator isn't compiled yet; run crew compile"; err == nil || err.Error() != want {
		t.Fatalf("err = %v; want %q", err, want)
	}
}

func TestOrchestratorCmd_PrintShowsTheLaunchLineAndStartsNothing(t *testing.T) {
	dir := compiledDir(t)
	p, _ := verify.Run(dir)
	line, _ := build.OrchestratorLaunch(p, "claude-code", build.Opts{})
	var out bytes.Buffer
	started := false
	err := orchestratorCmd([]string{"--dir", dir, "--print", "--", "--extra"}, &out, func([]string) error { started = true; return nil })
	if err != nil || started || out.String() != line+"\n" {
		t.Fatalf("err = %v, started = %v, out = %q; want %q", err, started, out.String(), line+"\n")
	}
}

func TestOrchestratorCmd_StartsTheArgvWithArgumentsAfterTheDashes(t *testing.T) {
	dir := compiledDir(t)
	var got []string
	err := orchestratorCmd([]string{"--dir", dir, "--harness", "claude-code", "--", "-c", "x"}, &bytes.Buffer{}, func(a []string) error { got = a; return nil })
	if err != nil || len(got) != 7 || !slices.Equal(got[5:], []string{"-c", "x"}) {
		t.Fatalf("err = %v, argv = %q", err, got)
	}
}

func TestOrchestratorCmd_RefusesUnexpectedArguments(t *testing.T) {
	err := orchestratorCmd([]string{"--dir", compiledDir(t), "extra"}, &bytes.Buffer{}, func([]string) error { return nil })
	if _, ok := err.(usageError); !ok {
		t.Fatalf("err = %v (%T); want a usage error", err, err)
	}
}

func TestExecOrchestrator_ProgramNotOnPath(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := execOrchestrator([]string{"no-such-harness-program", "--x"})
	if want := "can't find no-such-harness-program on PATH"; err == nil || err.Error() != want {
		t.Fatalf("err = %v; want %q", err, want)
	}
}

func TestPrintLaunches_PointsAtTheShortCommand(t *testing.T) {
	p, _ := verify.Run(scaffoldDir(t))
	var out bytes.Buffer
	printLaunches(&out, p, build.Opts{Out: "/o"})
	want := "claude-code:\n  orchestrator: claude --agent orchestrator --settings /o/crew-build/claude-code/agents/orchestrator.settings.json\n" +
		"  or, in a project directory: crew orchestrator\n"
	if out.String() != want {
		t.Fatalf("launches:\n%s\nwant:\n%s", out.String(), want)
	}
}

func TestOrchestratorHint_NamesTheHarnessOnlyWhenItIsNotTheDefault(t *testing.T) {
	if got, want := orchestratorHint("claude-code", "claude-code"), "crew orchestrator"; got != want {
		t.Errorf("default: %q; want %q", got, want)
	}
	if got, want := orchestratorHint("other", "claude-code"), "crew orchestrator --harness other"; got != want {
		t.Errorf("not the default: %q; want %q", got, want)
	}
}

func TestCLI_OrchestratorPrintAndNotCompiled(t *testing.T) {
	dir, home := scaffoldDir(t), t.TempDir() // compile links into HOME, so never the real one
	run := func(args ...string) (string, int) {
		cmd := exec.Command(crewBin, args...)
		cmd.Dir = t.TempDir()
		cmd.Env = append(os.Environ(), "CREW_HOME="+dir, "HOME="+home)
		out, err := cmd.CombinedOutput()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		return string(out), code
	}
	if out, code := run("orchestrator", "--print"); code != 1 || !strings.Contains(out, "crew: the orchestrator isn't compiled yet; run crew compile\n") {
		t.Fatalf("not compiled: code %d\n%s", code, out)
	}
	if out, code := run("compile"); code != 0 {
		t.Fatalf("compile: code %d\n%s", code, out)
	}
	out, code := run("orchestrator", "--print")
	if code != 0 || !strings.HasPrefix(out, "claude --agent orchestrator --settings ") || strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("print: code %d\n%s", code, out)
	}
}
