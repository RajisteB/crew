package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const stdinText = "use `port` $HOME \"8081\"\nand 'this'\n"

// crewIn is crew with the given text on stdin.
func (e *cliEnv) crewIn(stdin string, args ...string) (string, int) {
	e.t.Helper()
	cmd := exec.Command(crewBin, args...)
	cmd.Dir = e.project
	cmd.Env = append(os.Environ(), "CREW_HOME="+e.crewDir, "FAKE_REPLY_DIR="+e.rep, "FAKE_MODE=")
	cmd.Stdin = strings.NewReader(stdin)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		e.t.Fatal(err)
	}
	return string(out), code
}

func TestCLI_StdinTextArrivesVerbatim(t *testing.T) {
	t.Run("input", func(t *testing.T) {
		e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
		out, code := e.crewIn(stdinText, "run", "ship-change", "--input", "-")
		if code != 0 {
			t.Fatalf("exit %d:\n%s", code, out)
		}
		b, err := os.ReadFile(filepath.Join(e.project, ".crew", "runs", runID(t, out), "input.md"))
		if err != nil || string(b) != stdinText {
			t.Fatalf("input.md = %q (%v), want %q", b, err, stdinText)
		}
	})
	for name, args := range map[string][]string{
		"say":          {"say", "<id>", "-"},
		"approve note": {"approve", "<id>", "--note", "-"},
	} {
		t.Run(name, func(t *testing.T) {
			e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("fix"), verdict("done"), verdict("pass"))
			out, _ := e.crew("run", "ship-change", "--input", "x")
			id := runID(t, out)
			full := []string{"run"}
			for _, a := range args {
				full = append(full, strings.Replace(a, "<id>", id, 1))
			}
			if out, code := e.crewIn(stdinText, full...); code != 0 {
				t.Fatalf("exit %d:\n%s", code, out)
			}
			if name == "say" {
				if out, code := e.crew("run", "approve", id); code != 0 {
					t.Fatalf("approve exit %d:\n%s", code, out)
				}
			}
			b, _ := os.ReadFile(filepath.Join(e.rep, "prompt-4"))
			if !strings.Contains(string(b), stdinText) {
				t.Fatalf("the next station's prompt lacks the text:\n%s", b)
			}
		})
	}
}

func TestCLI_EmptyStdinInputIsRefused(t *testing.T) {
	e := newCLIEnv(t)
	out, code := e.crewIn(" \n\t\n", "run", "ship-change", "--input", "-")
	if code != 2 || !strings.Contains(out, "--input is required (text, @file, or - for stdin)") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	if _, err := os.Stat(filepath.Join(e.project, ".crew", "runs")); !os.IsNotExist(err) {
		t.Fatal("no run folder should be created")
	}
}

func TestCLI_DashInsideAMessageIsJustText(t *testing.T) {
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	out, _ := e.crew("run", "ship-change", "--input", "x")
	id := runID(t, out)
	if out, code := e.crewIn("never read", "run", "say", id, "-", "really"); code != 0 {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	b, err := os.ReadFile(filepath.Join(e.project, ".crew", "runs", id, "inbox.jsonl"))
	if err != nil || !strings.Contains(string(b), `"- really"`) || strings.Contains(string(b), "never read") {
		t.Fatalf("inbox = %q (%v)", b, err)
	}
}
