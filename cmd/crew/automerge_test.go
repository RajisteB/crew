package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

// amRig is a crew dir with domains, a project dir, a confirmed repo and a fake gh.
type amRig struct {
	crewDir, project string
	store            principal.Store
}

func newAMRig(t *testing.T, hasChecks bool, domains ...string) amRig {
	t.Helper()
	r := amRig{crewDir: t.TempDir(), project: t.TempDir()}
	r.store = principal.Store{Dir: filepath.Join(r.crewDir, "principal")}
	if err := os.MkdirAll(filepath.Join(r.crewDir, "domains"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, d := range domains {
		body := "---\nname: " + d + "\ndescription: x\nfactories: [f]\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(r.crewDir, "domains", d+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := ghClient
	ghClient = func(dir string) gh.Client {
		return gh.Client{Dir: dir, Exec: func(_ context.Context, _ string, _ []byte, _ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "api" {
				if hasChecks {
					return []byte("1\n"), nil
				}
				return []byte("0\n"), nil
			}
			return []byte(`{"nameWithOwner":"acme/app","visibility":"private","defaultBranchRef":{"name":"main"}}`), nil
		}}
	}
	t.Cleanup(func() { ghClient = old })
	return r
}

func (r amRig) confirmRepo(t *testing.T) {
	t.Helper()
	rec := principal.RepoRecord{Repo: "acme/app", Visibility: "private", ConfirmedAt: time.Now().UTC()}
	if err := r.store.ConfirmRepo(rec); err != nil {
		t.Fatal(err)
	}
}

func (r amRig) args(sub ...string) []string {
	return append(sub, "--dir", r.crewDir, "--project", r.project)
}

func (r amRig) modes(t *testing.T) map[string]principal.AutoMode {
	t.Helper()
	recs, err := r.store.Switches("acme/app")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]principal.AutoMode{}
	for d, rec := range recs {
		out[d] = rec.Mode
	}
	return out
}

func TestAutomerge_OnRecordsAfterTypedPhrase(t *testing.T) {
	r := newAMRig(t, true, "product")
	r.confirmRepo(t)
	screen := fakeTTY(t, "acme/app product\n")
	var out bytes.Buffer
	if err := automergeCmd(r.args("on", "--domain", "product"), &out); err != nil {
		t.Fatal(err)
	}
	if got := r.modes(t); got["product"] != principal.AutoOn || len(got) != 1 {
		t.Fatalf("modes = %v", got)
	}
	text := screen()
	for _, want := range []string{"acme/app", "product", "automatic deploys", `Type "acme/app product" to confirm: `} {
		if !strings.Contains(text, want) {
			t.Fatalf("screen lacks %q:\n%s", want, text)
		}
	}
	if out.String() != "auto-merge on for product in acme/app\n" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestAutomerge_WrongPhraseRecordsNothing(t *testing.T) {
	r := newAMRig(t, true, "product")
	r.confirmRepo(t)
	fakeTTY(t, "yes\n")
	if err := automergeCmd(r.args("on", "--domain", "product"), io.Discard); err == nil {
		t.Fatal("want error")
	}
	if got := r.modes(t); len(got) != 0 {
		t.Fatalf("recorded %v", got)
	}
}

func TestAutomerge_NoTerminalFails(t *testing.T) {
	r := newAMRig(t, true, "product")
	r.confirmRepo(t)
	old := openTTY
	openTTY = func() (*os.File, error) { return nil, errors.New("no tty") }
	t.Cleanup(func() { openTTY = old })
	oldIn := os.Stdin
	pr, pw, _ := os.Pipe()
	pw.WriteString("acme/app product\n")
	pw.Close()
	os.Stdin = pr
	t.Cleanup(func() { os.Stdin = oldIn })
	err := automergeCmd(r.args("on", "--domain", "product"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Fatalf("err = %v", err)
	}
	if got := r.modes(t); len(got) != 0 {
		t.Fatalf("recorded %v", got)
	}
	rest, _ := io.ReadAll(pr)
	if string(rest) != "acme/app product\n" {
		t.Fatalf("stdin was read: left %q", rest)
	}
}

func TestAutomerge_OnRefusesRepoWithoutChecks(t *testing.T) {
	r := newAMRig(t, false, "product")
	r.confirmRepo(t)
	fakeTTY(t, "acme/app product\n")
	err := automergeCmd(r.args("on", "--domain", "product"), io.Discard)
	want := "acme/app has no checks: nothing ran on the latest commit of main, and there is no workflow in .github/workflows.\n" +
		"With auto-merge on, crew merges a pull request only after a check passes, so nothing would ever merge here.\n" +
		"Add CI, or run crew automerge on --domain product --without-checks to merge as soon as the factory finishes."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if got := r.modes(t); len(got) != 0 {
		t.Fatalf("recorded %v", got)
	}
}

func TestAutomerge_WorkflowFileCountsAsChecks(t *testing.T) {
	r := newAMRig(t, false, "product")
	r.confirmRepo(t)
	wf := filepath.Join(r.project, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wf, "ci.yml"), []byte("name: ci\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeTTY(t, "acme/app product\n")
	if err := automergeCmd(r.args("on", "--domain", "product"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if r.modes(t)["product"] != principal.AutoOn {
		t.Fatal("not recorded")
	}
}

func TestAutomerge_WithoutChecksSkipsTheCheckAndWarns(t *testing.T) {
	r := newAMRig(t, false, "product")
	r.confirmRepo(t)
	screen := fakeTTY(t, "acme/app product\n")
	if err := automergeCmd(r.args("on", "--domain", "product", "--without-checks"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen(), "unchecked work") {
		t.Fatal("no warning")
	}
	if r.modes(t)["product"] != principal.AutoWithoutChecks {
		t.Fatalf("modes = %v", r.modes(t))
	}
}

func TestAutomerge_AllSetsEveryDomain(t *testing.T) {
	r := newAMRig(t, true, "a", "b", "c")
	r.confirmRepo(t)
	fakeTTY(t, "acme/app all\n")
	var out bytes.Buffer
	if err := automergeCmd(r.args("on", "--all"), &out); err != nil {
		t.Fatal(err)
	}
	if got := r.modes(t); len(got) != 3 {
		t.Fatalf("modes = %v", got)
	}
	if out.String() != "auto-merge on for a, b, c in acme/app\n" {
		t.Fatalf("out = %q", out.String())
	}
}

func TestAutomerge_OffNeedsThePhraseToo(t *testing.T) {
	r := newAMRig(t, true, "product")
	r.confirmRepo(t)
	fakeTTY(t, "acme/app product\n")
	if err := automergeCmd(r.args("on", "--domain", "product"), io.Discard); err != nil {
		t.Fatal(err)
	}
	fakeTTY(t, "no\n")
	if err := automergeCmd(r.args("off", "--domain", "product"), io.Discard); err == nil {
		t.Fatal("want error")
	}
	if r.modes(t)["product"] != principal.AutoOn {
		t.Fatal("changed despite wrong phrase")
	}
	screen := fakeTTY(t, "acme/app product\n")
	if err := automergeCmd(r.args("off", "--domain", "product"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if r.modes(t)["product"] != principal.AutoOff {
		t.Fatal("not off")
	}
	if !strings.Contains(screen(), "off. crew leaves this domain's pull requests for you to merge.") {
		t.Fatal("no off mode line")
	}
}

func TestAutomerge_Refusals(t *testing.T) {
	cases := map[string]struct {
		confirmed bool
		args      []string
		want      string
	}{
		"unknown domain": {true, []string{"on", "--domain", "nope"}, "nope"},
		"domain and all": {true, []string{"on", "--domain", "product", "--all"}, "exactly one"},
		"neither":        {true, []string{"on"}, "exactly one"},
		"unconfirmed":    {false, []string{"on", "--domain", "product"}, "acme/app isn't confirmed; run crew repo confirm first"},
		"off w/o checks": {true, []string{"off", "--domain", "product", "--without-checks"}, "--without-checks"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			r := newAMRig(t, true, "product")
			if c.confirmed {
				r.confirmRepo(t)
			}
			fakeTTY(t, "acme/app product\n")
			err := automergeCmd(r.args(c.args...), io.Discard)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v, want %q", err, c.want)
			}
			if got := r.modes(t); len(got) != 0 {
				t.Fatalf("recorded %v", got)
			}
		})
	}
}

func TestAutomerge_NoDomains(t *testing.T) {
	r := newAMRig(t, true)
	err := automergeCmd(r.args("status"), io.Discard)
	want := "no domains defined in " + r.crewDir + "/domains; run crew mission setup"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
}

func TestAutomerge_Status(t *testing.T) {
	r := newAMRig(t, true, "product", "ops", "docs")
	r.confirmRepo(t)
	fakeTTY(t, "acme/app product\n")
	if err := automergeCmd(r.args("on", "--domain", "product"), io.Discard); err != nil {
		t.Fatal(err)
	}
	fakeTTY(t, "acme/app ops\n")
	if err := automergeCmd(r.args("on", "--domain", "ops", "--without-checks"), io.Discard); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := automergeCmd(r.args("status"), &out); err != nil {
		t.Fatal(err)
	}
	day := time.Now().UTC().Format(time.DateOnly)
	want := "docs  off\nops  without-checks  set " + day + "\nproduct  on  set " + day + "\n"
	if out.String() != want {
		t.Fatalf("got %q want %q", out.String(), want)
	}
	if err := os.WriteFile(filepath.Join(r.store.Dir, "automerge.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := automergeCmd(r.args("status"), io.Discard)
	if err == nil || !strings.Contains(err.Error(), "automerge.json") {
		t.Fatalf("err = %v", err)
	}
}
