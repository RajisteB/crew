package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

func fakeGH(t *testing.T, visibility string) {
	t.Helper()
	old := ghClient
	ghClient = func(dir string) gh.Client {
		return gh.Client{Dir: dir, Exec: func(context.Context, string, []byte, string, ...string) ([]byte, error) {
			return []byte(`{"nameWithOwner":"acme/app","visibility":"` + visibility + `","defaultBranchRef":{"name":"main"}}`), nil
		}}
	}
	t.Cleanup(func() { ghClient = old })
}

// fakeTTY replaces openTTY with one end of a socket pair. The answer is
// already queued on the other end, and the returned func reads the screen
// text the command wrote (call it after repoCmd returns).
func fakeTTY(t *testing.T, answer string) func() string {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatal(err)
	}
	cmdEnd := os.NewFile(uintptr(fds[0]), "tty-cmd")
	human := os.NewFile(uintptr(fds[1]), "tty-human")
	if _, err := human.WriteString(answer); err != nil {
		t.Fatal(err)
	}
	old := openTTY
	openTTY = func() (*os.File, error) { return cmdEnd, nil }
	t.Cleanup(func() { openTTY = old; human.Close() })
	return func() string {
		_ = human.SetReadDeadline(time.Now().Add(time.Second))
		data, _ := io.ReadAll(human)
		return string(data)
	}
}

func repoArgs(t *testing.T, sub string) ([]string, string) {
	t.Helper()
	crewDir := t.TempDir()
	proj := t.TempDir()
	return []string{sub, "--dir", crewDir, "--project", proj}, crewDir
}

func TestConfirm_NoTerminalFails(t *testing.T) {
	fakeGH(t, "private")
	old := openTTY
	openTTY = func() (*os.File, error) { return nil, errors.New("no tty") }
	t.Cleanup(func() { openTTY = old })
	oldIn := os.Stdin
	r, w, _ := os.Pipe()
	w.WriteString("acme/app\n")
	w.Close()
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = oldIn })

	args, crewDir := repoArgs(t, "confirm")
	err := repoCmd(args, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Fatalf("err = %v, want needs a terminal", err)
	}
	if _, statErr := os.Stat(filepath.Join(crewDir, "principal")); statErr == nil {
		t.Fatal("nothing should be recorded")
	}
}

func TestRepoConfirm_ShowsVisibilityAndRecords(t *testing.T) {
	fakeGH(t, "PUBLIC")
	screen := fakeTTY(t, "acme/app\n")
	args, crewDir := repoArgs(t, "confirm")
	if err := repoCmd(args, io.Discard); err != nil {
		t.Fatal(err)
	}
	data := screen()
	for _, want := range []string{"acme/app", "PUBLIC", "missions will push piece branches and open PRs here"} {
		if !strings.Contains(data, want) {
			t.Fatalf("screen lacks %q:\n%s", want, data)
		}
	}
	var out bytes.Buffer
	if err := repoCmd([]string{"status", "--dir", crewDir, "--project", args[4]}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "acme/app: confirmed as public (") {
		t.Fatalf("status: %q", out.String())
	}
}

func TestRepoConfirm_WrongAnswerRecordsNothing(t *testing.T) {
	fakeGH(t, "private")
	fakeTTY(t, "nope\n")
	args, crewDir := repoArgs(t, "confirm")
	if err := repoCmd(args, io.Discard); err == nil {
		t.Fatal("want error")
	}
	if _, err := os.Stat(filepath.Join(crewDir, "principal", "repos.json")); err == nil {
		t.Fatal("recorded despite wrong answer")
	}
}

func TestRepoStatus_States(t *testing.T) {
	args, crewDir := repoArgs(t, "status")
	proj := args[4]
	fakeGH(t, "private")
	var out bytes.Buffer
	if err := repoCmd(args, &out); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "acme/app: not confirmed; run crew repo confirm in a terminal\n" {
		t.Fatalf("got %q", got)
	}
	fakeTTY(t, "acme/app\n")
	if err := repoCmd([]string{"confirm", "--dir", crewDir, "--project", proj}, io.Discard); err != nil {
		t.Fatal(err)
	}
	fakeGH(t, "public")
	out.Reset()
	if err := repoCmd(args, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "was confirmed as private and is now public") {
		t.Fatalf("got %q", out.String())
	}
}

func TestRepoStatus_TamperedStoreIsAnError(t *testing.T) {
	fakeGH(t, "private")
	fakeTTY(t, "acme/app\n")
	args, crewDir := repoArgs(t, "confirm")
	if err := repoCmd(args, io.Discard); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(crewDir, "principal", "repos.json")
	data, _ := os.ReadFile(path)
	if err := os.WriteFile(path, []byte(strings.Replace(string(data), "private", "public ", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	err := repoCmd([]string{"status", "--dir", crewDir, "--project", args[4]}, io.Discard)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("err = %v, want one naming %s", err, path)
	}
}
