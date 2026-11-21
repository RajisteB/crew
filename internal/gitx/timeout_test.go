package gitx

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeGit puts a `git` that records its environment and then sleeps first on
// PATH. It returns the file the environment is written to.
func fakeGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.txt")
	script := "#!/bin/sh\nenv > '" + envFile + "'\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return envFile
}

func TestGit_DeadlineEndsAHungCommandAndDisablesPrompts(t *testing.T) {
	envFile := fakeGit(t)
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := Repo{Dir: t.TempDir()}.Fetch(ctx, "main")
	if err == nil {
		t.Fatal("a hung git must be an error")
	}
	if took := time.Since(start); took > time.Second {
		t.Fatalf("took %s", took)
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v; it should say the command timed out", err)
	}
	env, rerr := os.ReadFile(envFile)
	if rerr != nil {
		t.Fatalf("the fake git never ran: %v", rerr)
	}
	if !strings.Contains("\n"+string(env), "\nGIT_TERMINAL_PROMPT=0\n") {
		t.Fatalf("GIT_TERMINAL_PROMPT=0 missing from the environment:\n%s", env)
	}
	if strings.Contains(string(env), "GIT_SSH_COMMAND=") && os.Getenv("GIT_SSH_COMMAND") == "" {
		t.Fatalf("crew must not set GIT_SSH_COMMAND:\n%s", env)
	}
}

func TestGit_CancelIsReportedAsCancelled(t *testing.T) {
	fakeGit(t)
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	err := Repo{Dir: t.TempDir()}.Push(ctx, t.TempDir(), "b")
	if err == nil || !strings.Contains(err.Error(), "cancel") {
		t.Fatalf("err = %v", err)
	}
}

func TestTimeouts_NetworkLongerThanLocal(t *testing.T) {
	if networkTimeout != 2*time.Minute || localTimeout != 30*time.Second {
		t.Fatalf("network %s local %s", networkTimeout, localTimeout)
	}
}
