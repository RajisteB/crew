package gh

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
)

const rollupJSON = `{"number":12,"state":"OPEN","url":"https://github.com/a/b/pull/12",
"mergeable":"CONFLICTING","headRefOid":"abc123","statusCheckRollup":[
{"__typename":"CheckRun","name":"build","status":"COMPLETED","conclusion":"SUCCESS","detailsUrl":"https://x/1"},
{"__typename":"CheckRun","name":"lint","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"https://x/2"},
{"__typename":"CheckRun","name":"slow","status":"IN_PROGRESS","conclusion":"","detailsUrl":"https://x/3"},
{"__typename":"CheckRun","name":"opt","status":"COMPLETED","conclusion":"SKIPPED","detailsUrl":"https://x/4"},
{"__typename":"CheckRun","name":"gone","status":"COMPLETED","conclusion":"CANCELLED","detailsUrl":"https://x/5"},
{"__typename":"StatusContext","context":"ci/ok","state":"SUCCESS","targetUrl":"https://x/6"},
{"__typename":"StatusContext","context":"ci/wait","state":"PENDING","targetUrl":"https://x/7"},
{"__typename":"StatusContext","context":"ci/bad","state":"ERROR","targetUrl":"https://x/8"}]}`

func TestPRState_ParsesChecks(t *testing.T) {
	f := &fake{out: rollupJSON}
	pr, err := f.client().PRState(context.Background(), 12)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range pr.Checks {
		got = append(got, c.State)
	}
	want := []string{"pass", "fail", "pending", "skip", "fail", "pass", "pending", "fail"}
	if !slices.Equal(got, want) {
		t.Fatalf("states %v want %v", got, want)
	}
	if pr.Checks[0] != (Check{"build", "pass", "https://x/1"}) || pr.Checks[5] != (Check{"ci/ok", "pass", "https://x/6"}) {
		t.Fatalf("names/urls wrong: %+v", pr.Checks)
	}
	if pr.Mergeable != "CONFLICTING" || pr.HeadSHA != "abc123" {
		t.Fatalf("got %+v", pr)
	}
	wantArgs := []string{"pr", "view", "12", "--json", "number,state,url,mergeable,headRefOid,statusCheckRollup"}
	if !slices.Equal(f.calls[0].args, wantArgs) {
		t.Fatalf("args %v", f.calls[0].args)
	}
}

func TestPRState_UnknownEntryIsPending(t *testing.T) {
	f := &fake{out: `{"number":1,"state":"OPEN","statusCheckRollup":[{"__typename":"Future","name":"x"}]}`}
	pr, err := f.client().PRState(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.Checks) != 1 || pr.Checks[0] != (Check{Name: "x", State: "pending"}) {
		t.Fatalf("got %+v", pr.Checks)
	}
}

func TestPRState_NoRollupIsNoChecks(t *testing.T) {
	f := &fake{out: `{"number":1,"state":"OPEN","statusCheckRollup":null}`}
	pr, err := f.client().PRState(context.Background(), 1)
	if err != nil || len(pr.Checks) != 0 {
		t.Fatalf("got %+v, %v", pr, err)
	}
}

func TestMergePR_NamesTheHead(t *testing.T) {
	f := &fake{}
	if err := f.client().MergePR(context.Background(), 12, "squash", "abc123"); err != nil {
		t.Fatal(err)
	}
	want := []string{"pr", "merge", "12", "--squash", "--match-head-commit", "abc123"}
	if !slices.Equal(f.calls[0].args, want) {
		t.Fatalf("args %v want %v", f.calls[0].args, want)
	}
}

func TestMergePR_RefusesBadInput(t *testing.T) {
	f := &fake{}
	if err := f.client().MergePR(context.Background(), 12, "fast", "abc123"); err == nil {
		t.Fatal("bad method accepted")
	}
	if err := f.client().MergePR(context.Background(), 12, "squash", ""); err == nil {
		t.Fatal("empty head accepted")
	}
	if len(f.calls) != 0 {
		t.Fatalf("Exec called: %v", f.calls)
	}
}

func TestFailedLog_TailOfActionsJob(t *testing.T) {
	var lines []string
	for i := 1; i <= 100; i++ {
		lines = append(lines, fmt.Sprintf("line %d", i))
	}
	f := &fake{out: strings.Join(lines, "\n") + "\n"}
	got, err := f.client().FailedLog(context.Background(), Check{URL: "https://github.com/a/b/actions/runs/7/job/99"})
	if err != nil {
		t.Fatal(err)
	}
	if want := strings.Join(lines[60:], "\n"); got != want {
		t.Fatalf("got %q", got)
	}
	if want := []string{"run", "view", "--job", "99", "--log-failed"}; !slices.Equal(f.calls[0].args, want) {
		t.Fatalf("args %v", f.calls[0].args)
	}
}

func TestFailedLog_CutsToRunes(t *testing.T) {
	f := &fake{out: strings.Repeat("é", 3000)}
	got, err := f.client().FailedLog(context.Background(), Check{URL: "https://github.com/a/b/actions/runs/7/job/99"})
	if err != nil || len([]rune(got)) != 2000 {
		t.Fatalf("len %d, %v", len([]rune(got)), err)
	}
}

func TestFailedLog_NotActions(t *testing.T) {
	f := &fake{}
	got, err := f.client().FailedLog(context.Background(), Check{URL: "https://ci.example/1"})
	if got != "" || err != nil || len(f.calls) != 0 {
		t.Fatalf("got %q, %v, %d calls", got, err, len(f.calls))
	}
}

type seqExec struct {
	outs  []string
	calls [][]string
}

func (s *seqExec) exec(_ context.Context, _ string, _ []byte, _ string, args ...string) ([]byte, error) {
	s.calls = append(s.calls, slices.Clone(args))
	return []byte(s.outs[len(s.calls)-1]), nil
}

func TestHasChecks(t *testing.T) {
	for _, tc := range []struct {
		runs, statuses string
		want           bool
	}{{"0", "0", false}, {"0", "2", true}, {"1", "0", true}} {
		s := &seqExec{outs: []string{tc.runs + "\n", tc.statuses + "\n"}}
		got, err := Client{Exec: s.exec}.HasChecks(context.Background(), "feat/x")
		if err != nil || got != tc.want {
			t.Fatalf("%v: got %v, %v", tc, got, err)
		}
	}
	s := &seqExec{outs: []string{"0", "0"}}
	_, _ = Client{Exec: s.exec}.HasChecks(context.Background(), "feat/x")
	want := []string{"api", "repos/{owner}/{repo}/commits/feat/x/check-runs", "--jq", ".total_count"}
	if !slices.Equal(s.calls[0], want) {
		t.Fatalf("args %v", s.calls[0])
	}
}
