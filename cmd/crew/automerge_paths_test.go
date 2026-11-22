package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

// setDomainPaths rewrites a domain file of the rig with a paths line.
func (r amRig) setDomainPaths(t *testing.T, name, paths string) {
	t.Helper()
	body := "---\nname: " + name + "\ndescription: x\nfactories: [f]\npaths: " + paths + "\n---\nbody\n"
	if err := os.WriteFile(filepath.Join(r.crewDir, "domains", name+".md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

const (
	pluralPathsText = "design, ops list no paths, so crew can't tell when a pull request changes their files: " +
		"their switch won't hold back another domain's pull request. Add paths: to their domain files to change that.\n\n"
	singularPathsText = "ops lists no paths, so crew can't tell when a pull request changes its files: " +
		"its switch won't hold back another domain's pull request. Add paths: to its domain file to change that.\n\n"
)

func TestAutomerge_OnListsDomainsWithoutPaths(t *testing.T) {
	cases := []struct {
		name  string
		setup func(amRig, *testing.T)
		args  []string
		want  string // "" means the paragraph is absent
	}{
		{"plural", func(amRig, *testing.T) {}, []string{"on", "--domain", "product"}, pluralPathsText},
		{"plural without checks", func(amRig, *testing.T) {}, []string{"on", "--domain", "product", "--without-checks"}, pluralPathsText},
		{"singular", func(r amRig, t *testing.T) { r.setDomainPaths(t, "design", "[brand/**]") }, []string{"on", "--domain", "product"}, singularPathsText},
		{"all have paths", func(r amRig, t *testing.T) {
			r.setDomainPaths(t, "design", "[brand/**]")
			r.setDomainPaths(t, "ops", "[infra/]")
		}, []string{"on", "--domain", "product"}, ""},
		{"switched domains are not listed", func(amRig, *testing.T) {}, []string{"on", "--all"}, ""},
		{"off", func(amRig, *testing.T) {}, []string{"off", "--domain", "product"}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newAMRig(t, true, "design", "ops", "product")
			r.confirmRepo(t)
			tc.setup(r, t)
			phrase := "acme/app product\n"
			if tc.args[len(tc.args)-1] == "--all" {
				phrase = "acme/app all\n"
			}
			screen := fakeTTY(t, phrase)
			if err := automergeCmd(r.args(tc.args...), io.Discard); err != nil {
				t.Fatal(err)
			}
			text := screen()
			if tc.want == "" {
				if strings.Contains(text, "no paths") {
					t.Fatalf("screen has the paragraph:\n%s", text)
				}
				return
			}
			if !strings.Contains(text, tc.want+"Type ") {
				t.Fatalf("screen lacks the paragraph before the prompt:\n%s", text)
			}
			if i, j := strings.Index(text, "automatic deploys"), strings.Index(text, "no paths"); i < 0 || j < i {
				t.Fatalf("paragraph is not after the warning:\n%s", text)
			}
		})
	}
}

func TestAutomerge_WorkflowFileSkipsHasChecks(t *testing.T) {
	r := newAMRig(t, false, "product")
	r.confirmRepo(t)
	ghClient = func(dir string) gh.Client {
		return gh.Client{Dir: dir, Exec: func(_ context.Context, _ string, _ []byte, _ string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "api" {
				return nil, errors.New("the check call must not run")
			}
			return []byte(`{"nameWithOwner":"acme/app","visibility":"private","defaultBranchRef":{"name":"main"}}`), nil
		}}
	}
	wf := filepath.Join(r.project, ".github", "workflows")
	if err := os.MkdirAll(wf, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wf, "ci.yaml"), []byte("name: ci\n"), 0o644); err != nil {
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
