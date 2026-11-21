package gh

import (
	"context"
	"errors"
	"os/exec"
	"slices"
	"strings"
	"testing"
)

type call struct {
	dir   string
	stdin []byte
	name  string
	args  []string
}

type fake struct {
	calls []call
	out   string
	err   error
}

func (f *fake) exec(_ context.Context, dir string, stdin []byte, name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, call{dir, stdin, name, slices.Clone(args)})
	return []byte(f.out), f.err
}

func (f *fake) client() Client { return Client{Dir: "/repo", Exec: f.exec} }

func TestRepo_ParsesSlugVisibilityBranch(t *testing.T) {
	f := &fake{out: `{"nameWithOwner":"acme/app","visibility":"PRIVATE","defaultBranchRef":{"name":"main"}}`}
	got, err := f.client().Repo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	want := RepoInfo{Slug: "acme/app", Visibility: "private", DefaultBranch: "main"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	c := f.calls[0]
	if c.dir != "/repo" || c.name != "gh" ||
		!slices.Equal(c.args, []string{"repo", "view", "--json", "nameWithOwner,visibility,defaultBranchRef"}) {
		t.Fatalf("unexpected call %+v", c)
	}
}

func TestRepo_UnparseableOutputIsError(t *testing.T) {
	f := &fake{out: "not json"}
	if _, err := f.client().Repo(context.Background()); err == nil || !strings.Contains(err.Error(), "gh repo view") {
		t.Fatalf("want error naming command, got %v", err)
	}
}

func TestCreatePR_SendsBodyOnStdin(t *testing.T) {
	f := &fake{out: "Creating pull request...\nhttps://github.com/acme/app/pull/12\n"}
	got, err := f.client().CreatePR(context.Background(), "-head", "main", "-title", "secret body text")
	if err != nil {
		t.Fatal(err)
	}
	want := PR{Number: 12, State: "OPEN", URL: "https://github.com/acme/app/pull/12"}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}
	c := f.calls[0]
	if string(c.stdin) != "secret body text" {
		t.Fatalf("stdin = %q", c.stdin)
	}
	for _, a := range c.args {
		if strings.Contains(a, "secret body text") {
			t.Fatalf("body leaked into args: %v", c.args)
		}
	}
	wantArgs := []string{"pr", "create", "--head=-head", "--base=main", "--title=-title", "--body-file", "-"}
	if !slices.Equal(c.args, wantArgs) {
		t.Fatalf("args %v want %v", c.args, wantArgs)
	}
}

func TestCreatePR_BadURLIsError(t *testing.T) {
	f := &fake{out: "something odd\n"}
	if _, err := f.client().CreatePR(context.Background(), "h", "b", "t", "x"); err == nil || !strings.Contains(err.Error(), "gh pr create") {
		t.Fatalf("want error naming command, got %v", err)
	}
}

func TestPRState_Merged(t *testing.T) {
	f := &fake{out: `{"number":12,"state":"MERGED","url":"https://github.com/acme/app/pull/12"}`}
	got, err := f.client().PRState(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != "MERGED" || got.Number != 12 {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal(f.calls[0].args, []string{"pr", "view", "12", "--json", "number,state,url"}) {
		t.Fatalf("args %v", f.calls[0].args)
	}
}

func TestPRState_UnparseableOutputIsError(t *testing.T) {
	f := &fake{out: "{}"}
	if _, err := f.client().PRState(context.Background(), 3); err == nil {
		t.Fatal("want error for empty result")
	}
}

func TestClient_ErrorCarriesStderr(t *testing.T) {
	f := &fake{err: errors.New("exit status 1: GraphQL: no pull request found")}
	_, err := f.client().PRState(context.Background(), 9)
	if err == nil || !strings.Contains(err.Error(), "no pull request found") || !strings.Contains(err.Error(), "gh pr view") {
		t.Fatalf("got %v", err)
	}
}

func TestClient_MissingBinary(t *testing.T) {
	f := &fake{err: &exec.Error{Name: "gh", Err: exec.ErrNotFound}}
	_, err := f.client().Repo(context.Background())
	want := "gh not found on PATH; install it and run gh auth login"
	if err == nil || err.Error() != want {
		t.Fatalf("got %v want %q", err, want)
	}
}

func TestTail(t *testing.T) {
	in := "a\nb\nc\nd\ne\nf\n"
	if got := tail(in, 3); got != "d\ne\nf" {
		t.Fatalf("got %q", got)
	}
}

func TestFindPR_ReturnsExisting(t *testing.T) {
	f := &fake{out: `[{"number":7,"state":"OPEN","url":"https://github.com/acme/app/pull/7"}]`}
	got, ok, err := f.client().FindPR(context.Background(), "crew/m1/p1")
	if err != nil || !ok {
		t.Fatalf("got %v %v", ok, err)
	}
	if got != (PR{Number: 7, State: "OPEN", URL: "https://github.com/acme/app/pull/7"}) {
		t.Fatalf("got %+v", got)
	}
	want := []string{"pr", "list", "--head=crew/m1/p1", "--state", "all", "--json", "number,state,url", "--limit", "1"}
	if !slices.Equal(f.calls[0].args, want) {
		t.Fatalf("args %v want %v", f.calls[0].args, want)
	}
}

func TestFindPR_EmptyListIsNotFound(t *testing.T) {
	f := &fake{out: "[]"}
	got, ok, err := f.client().FindPR(context.Background(), "b")
	if err != nil || ok || got != (PR{}) {
		t.Fatalf("got %+v %v %v", got, ok, err)
	}
}

func TestFindPR_ErrorsAreReported(t *testing.T) {
	f := &fake{err: errors.New("boom")}
	if _, _, err := f.client().FindPR(context.Background(), "b"); err == nil || !strings.Contains(err.Error(), "gh pr list") {
		t.Fatalf("got %v", err)
	}
	f = &fake{out: "not json"}
	if _, _, err := f.client().FindPR(context.Background(), "b"); err == nil {
		t.Fatal("want error for bad output")
	}
}
