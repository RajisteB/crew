package run_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/run"
)

func fakeHarnessPath(t *testing.T) string {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(here), "testdata", "fakeharness.sh")
}

func fakeRegistry(t *testing.T, cmd ...string) *config.Registry {
	t.Helper()
	if len(cmd) == 0 {
		cmd = []string{fakeHarnessPath(t), "--agent", "{agent}", "--allowedTools", "{allowed}", "--mode", "dontAsk"}
	}
	return &config.Registry{Harnesses: map[string]config.Harness{
		"fake": {Display: "Fake", Headless: &config.HeadlessSpec{Command: cmd, PromptVia: "stdin", AllowVia: "flag"}},
	}}
}

func req(prompt string, allowed ...string) run.StationRequest {
	return run.StationRequest{Harness: "fake", Agent: "reviewer", Prompt: prompt, Allowed: allowed, Timeout: 10 * time.Second}
}

func TestHeadlessRunner_StdinAndArgs(t *testing.T) {
	t.Setenv("FAKE_MODE", "echo")
	r := run.HeadlessRunner{Registry: fakeRegistry(t)}
	out, err := r.Run(context.Background(), req("hello prompt", "Bash", "Read"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ARG --agent\nARG reviewer\n", "ARG --allowedTools\nARG Bash,Read\n", "ARG --mode\nARG dontAsk\n", "STDIN hello prompt"} {
		if !strings.Contains(out.Text, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestHeadlessRunner_EmptyAllowlistDropsFlag(t *testing.T) {
	t.Setenv("FAKE_MODE", "echo")
	out, err := run.HeadlessRunner{Registry: fakeRegistry(t)}.Run(context.Background(), req("p"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.Text, "--allowedTools") || !strings.Contains(out.Text, "ARG --mode") {
		t.Fatalf("empty allowlist must drop --allowedTools and its value:\n%s", out)
	}
}

func TestHeadlessRunner_PromptAsArg(t *testing.T) {
	t.Setenv("FAKE_MODE", "echo")
	reg := fakeRegistry(t, fakeHarnessPath(t), "run", "--agent", "{agent}", "{prompt}")
	reg.Harnesses["fake"].Headless.PromptVia = "arg"
	reg.Harnesses["fake"].Headless.AllowVia = "agent-file"
	out, err := run.HeadlessRunner{Registry: reg}.Run(context.Background(), req("the prompt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Text, "ARG the prompt\n") {
		t.Fatalf("prompt not passed as an argument:\n%s", out)
	}
}

func TestHeadlessRunner_MissingBinary(t *testing.T) {
	r := run.HeadlessRunner{Registry: fakeRegistry(t, "crew-no-such-cli-xyz", "{agent}")}
	_, err := r.Run(context.Background(), req("p"))
	if err == nil || !strings.Contains(err.Error(), `fake: command "crew-no-such-cli-xyz" not found on PATH (headless.command in harnesses.yaml)`) {
		t.Fatalf("got %v", err)
	}
}

func TestHeadlessRunner_Timeout(t *testing.T) {
	t.Setenv("FAKE_MODE", "sleep")
	rq := req("p")
	rq.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := run.HeadlessRunner{Registry: fakeRegistry(t)}.Run(context.Background(), rq)
	if err == nil || !strings.Contains(err.Error(), "timed out after 200ms") {
		t.Fatalf("got %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("took %v; the child must be killed on timeout", time.Since(start))
	}
}

func TestHeadlessRunner_Canceled(t *testing.T) {
	t.Setenv("FAKE_MODE", "sleep")
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err := run.HeadlessRunner{Registry: fakeRegistry(t)}.Run(ctx, req("p"))
	if err == nil || ctx.Err() == nil || !run.IsCanceled(err) {
		t.Fatalf("want a cancellation error, got %v", err)
	}
}

func TestHeadlessRunner_NonZeroExit(t *testing.T) {
	t.Setenv("FAKE_MODE", "exit3")
	_, err := run.HeadlessRunner{Registry: fakeRegistry(t)}.Run(context.Background(), req("p"))
	if err == nil || !strings.Contains(err.Error(), "fake station failed (exit 3) running "+fakeHarnessPath(t)) || !strings.Contains(err.Error(), "boom: not logged in") {
		t.Fatalf("got %v", err)
	}
}

func TestHeadlessRunner_NoHeadless(t *testing.T) {
	reg := &config.Registry{Harnesses: map[string]config.Harness{"fake": {Display: "Fake"}}}
	_, err := run.HeadlessRunner{Registry: reg}.Run(context.Background(), req("p"))
	if err == nil || !strings.Contains(err.Error(), "no headless") {
		t.Fatalf("got %v", err)
	}
}

func TestHeadlessRunner_NonZeroExitFallsBackToStdout(t *testing.T) {
	t.Setenv("FAKE_MODE", "stdoutfail")
	_, err := run.HeadlessRunner{Registry: fakeRegistry(t)}.Run(context.Background(), req("p"))
	if err == nil || !strings.Contains(err.Error(), "Please run /login first") {
		t.Fatalf("stdout must be shown when stderr is empty: %v", err)
	}
}

func TestHeadlessRunner_OnStartGetsPid(t *testing.T) {
	t.Setenv("FAKE_MODE", "echo")
	rq := req("p")
	var pid int
	rq.OnStart = func(p int) { pid = p }
	if _, err := (run.HeadlessRunner{Registry: fakeRegistry(t)}).Run(context.Background(), rq); err != nil {
		t.Fatal(err)
	}
	if pid <= 0 {
		t.Fatalf("OnStart pid = %d", pid)
	}
}

func TestHeadlessRunner_ClaudeJSON(t *testing.T) {
	t.Setenv("FAKE_MODE", "json")
	reg := fakeRegistry(t)
	reg.Harnesses["fake"].Headless.Output = "claude-json"
	out, err := run.HeadlessRunner{Registry: reg}.Run(context.Background(), req("p"))
	if err != nil {
		t.Fatal(err)
	}
	if out.Text != "hello from json" || len(out.Denials) != 1 || !strings.Contains(out.Denials[0], "Bash") || !strings.Contains(out.Denials[0], "rm -rf x") {
		t.Fatalf("reply = %+v", out)
	}
	t.Setenv("FAKE_MODE", "jsonerr")
	if _, err := (run.HeadlessRunner{Registry: reg}).Run(context.Background(), req("p")); err == nil || !strings.Contains(err.Error(), "API overloaded") {
		t.Fatalf("is_error must fail the station: %v", err)
	}
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestExpandCommand_DropsSettingsFlagWhenNoRules(t *testing.T) {
	h := config.HeadlessSpec{Command: []string{"x", "--agent", "{agent}", "--settings", "{settings}", "--mode", "dontAsk"}}
	got := run.ExpandCommandForTest(h, run.StationRequest{Agent: "a"}, "")
	if want := "x --agent a --mode dontAsk"; strings.Join(got, " ") != want {
		t.Fatalf("got %q, want %q", strings.Join(got, " "), want)
	}
	got = run.ExpandCommandForTest(h, run.StationRequest{Agent: "a"}, "/tmp/s.json")
	if want := "x --agent a --settings /tmp/s.json --mode dontAsk"; strings.Join(got, " ") != want {
		t.Fatalf("got %q, want %q", strings.Join(got, " "), want)
	}
}
