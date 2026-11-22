package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/jira/jiratest"
)

const (
	testSite  = "https://example.atlassian.net"
	testEmail = "secret-email@example.com"
	testToken = "secret-token-value"
)

// jiraRig is a mission rig whose Jira site is the local test server.
type jiraRig struct {
	*missionRig
	srv *jiratest.Server
}

func setJiraEnv(t *testing.T, email, token string) {
	t.Helper()
	old := jiraEnv
	jiraEnv = func(k string) string {
		switch k {
		case "CREW_JIRA_EMAIL":
			return email
		case "CREW_JIRA_TOKEN":
			return token
		}
		return ""
	}
	t.Cleanup(func() { jiraEnv = old })
}

func newJiraRig(t *testing.T) *jiraRig {
	t.Helper()
	r := &jiraRig{missionRig: newMissionRig(t), srv: jiratest.New(t)}
	old := newJiraClient
	newJiraClient = func(_ jira.Repo, email, token string) *jira.Client { return jira.New(r.srv.URL(), email, token) }
	t.Cleanup(func() { newJiraClient = old })
	setJiraEnv(t, testEmail, testToken)
	return r
}

func (r *jiraRig) jira(args ...string) (string, error) {
	r.t.Helper()
	var out bytes.Buffer
	full := append(append([]string{}, args...), "--dir", r.crewDir, "--project", r.repo)
	err := jiraCmd(full, &out)
	return out.String(), err
}

func (r *jiraRig) setup(args ...string) (string, error) {
	return r.jira(append([]string{"setup", "--site", testSite, "--key", "ACME"}, args...)...)
}

func (r *jiraRig) saveEntry(board int) {
	r.t.Helper()
	repo := jira.Repo{Site: testSite, Project: "ACME", Board: board}
	cfg := (jira.Config{Version: 1}).With("acme/app", repo)
	if err := jira.SaveConfig(r.crewDir, cfg); err != nil {
		r.t.Fatal(err)
	}
}

func (r *jiraRig) entry() (jira.Repo, bool) {
	r.t.Helper()
	cfg, err := jira.LoadConfig(r.crewDir)
	if err != nil {
		r.t.Fatal(err)
	}
	return cfg.For("acme/app")
}

func (r *jiraRig) noEntry() {
	r.t.Helper()
	if _, err := os.Stat(filepath.Join(r.crewDir, jira.ConfigFile)); err == nil {
		r.t.Fatal("jira.yaml was written")
	}
}

// ttyNeverOpened fails the test if the command opens the terminal.
func ttyNeverOpened(t *testing.T) {
	t.Helper()
	old := openTTY
	openTTY = func() (*os.File, error) {
		t.Error("the terminal was opened")
		return nil, errors.New("no tty")
	}
	t.Cleanup(func() { openTTY = old })
}

const setUpLine = "Jira is set up for acme/app. New missions in this repo will appear in ACME.\n"

func TestJiraSetup_WritesTheEntry(t *testing.T) {
	r := newJiraRig(t)
	fakeTTY(t, "acme/app ACME\n")
	out, err := r.setup()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, ok := r.entry()
	if !ok || got.Board != 0 || got.Project != "ACME" || got.Site != testSite || got.IssueType != "Task" || got.Statuses.Done != "Done" {
		t.Fatalf("entry = %+v, %v", got, ok)
	}
	if !strings.HasSuffix(out, setUpLine) {
		t.Fatalf("out:\n%s", out)
	}
	if !strings.Contains(out, "no board found for ACME: issues are not put in a sprint") {
		t.Fatalf("out:\n%s", out)
	}
}

func TestJiraSetup_TrimsTrailingSlashesAndReplacesEntry(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(9)
	fakeTTY(t, "acme/app ACME\n")
	if out, err := r.jira("setup", "--site", testSite+"/", "--key", "ACME"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	got, _ := r.entry()
	if got.Site != testSite || got.Board != 0 {
		t.Fatalf("entry = %+v", got)
	}
}

func TestJiraSetup_PicksTheOnlyBoard(t *testing.T) {
	r := newJiraRig(t)
	r.srv.SetActiveSprint(1, 4)
	fakeTTY(t, "acme/app ACME\n")
	out, err := r.setup()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := r.entry(); got.Board != 1 {
		t.Fatalf("entry = %+v", got)
	}
	if !strings.Contains(out, `board: 1, active sprint "Sprint 4"`) {
		t.Fatalf("out:\n%s", out)
	}
}

func TestJiraSetup_SeveralBoardsNeedAFlag(t *testing.T) {
	r := newJiraRig(t)
	for _, b := range []int{1, 4, 7} {
		r.srv.SetActiveSprint(b, 0)
	}
	fakeTTY(t, "acme/app ACME\n")
	out, err := r.setup()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := r.entry(); got.Board != 0 {
		t.Fatalf("entry = %+v", got)
	}
	want := `ACME has 3 boards (1 "Board 1", 4 "Board 4", …): pass --board N to use sprints`
	if !strings.Contains(out, want) {
		t.Fatalf("out lacks %q:\n%s", want, out)
	}
}

func TestJiraSetup_TwoBoardsListBoth(t *testing.T) {
	r := newJiraRig(t)
	r.srv.SetActiveSprint(1, 0)
	r.srv.SetActiveSprint(4, 0)
	fakeTTY(t, "acme/app ACME\n")
	out, _ := r.setup()
	want := `ACME has 2 boards (1 "Board 1", 4 "Board 4"): pass --board N to use sprints`
	if !strings.Contains(out, want) {
		t.Fatalf("out lacks %q:\n%s", want, out)
	}
}

func TestJiraSetup_BoardFlag(t *testing.T) {
	r := newJiraRig(t)
	r.srv.SetActiveSprint(4, 2)
	fakeTTY(t, "acme/app ACME\n")
	if out, err := r.setup("--board", "4", "--assignee", "acc-9"); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, _ := r.entry(); got.Board != 4 || got.Assignee != "acc-9" {
		t.Fatalf("entry = %+v", got)
	}
}

func TestJiraSetup_NoTerminalWritesNothing(t *testing.T) {
	r := newJiraRig(t)
	old := openTTY
	openTTY = func() (*os.File, error) { return nil, errors.New("no tty") }
	t.Cleanup(func() { openTTY = old })
	_, err := r.setup()
	if err == nil || !strings.Contains(err.Error(), "needs a terminal") {
		t.Fatalf("err = %v", err)
	}
	r.noEntry()
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

func TestJiraSetup_WrongPhraseWritesNothing(t *testing.T) {
	r := newJiraRig(t)
	fakeTTY(t, "yes\n")
	if _, err := r.setup(); err == nil {
		t.Fatal("want error")
	}
	r.noEntry()
}

func TestJiraSetup_RefusesOtherHosts(t *testing.T) {
	r := newJiraRig(t)
	ttyNeverOpened(t)
	_, err := r.jira("setup", "--site", "https://example.com", "--key", "ACME")
	if err == nil || !strings.Contains(err.Error(), "site must be https://<name>.atlassian.net") {
		t.Fatalf("err = %v", err)
	}
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
	r.noEntry()
}

func TestJiraSetup_ProblemStops(t *testing.T) {
	r := newJiraRig(t)
	r.srv.DropStatus("In Review")
	screen := fakeTTY(t, "acme/app ACME\n")
	out, err := r.setup()
	if err == nil {
		t.Fatalf("want error\n%s", out)
	}
	want := `  problem: status "In Review" is not in ACME's workflow for Task; add it in Jira or set statuses.in_review in jira.yaml`
	if !strings.Contains(out, want) {
		t.Fatalf("out lacks %q:\n%s", want, out)
	}
	r.noEntry()
	if text := screen(); text != "" {
		t.Fatalf("the confirmation was asked:\n%s", text)
	}
}

func TestJiraSetup_FirstSaveIsVersion1(t *testing.T) {
	r := newJiraRig(t)
	fakeTTY(t, "acme/app ACME\n")
	if out, err := r.setup(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, err := os.ReadFile(filepath.Join(r.crewDir, jira.ConfigFile))
	if err != nil || !strings.Contains(string(b), "\nversion: 1\n") {
		t.Fatalf("err = %v\n%s", err, b)
	}
}

func TestJiraSetup_NeedsCredentials(t *testing.T) {
	r := newJiraRig(t)
	setJiraEnv(t, testEmail, "")
	ttyNeverOpened(t)
	_, err := r.setup()
	want := "set CREW_JIRA_EMAIL and CREW_JIRA_TOKEN first (an API token from id.atlassian.com); crew never stores them"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testEmail) {
		t.Fatal("the message holds the email")
	}
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

const reportHead = "Jira for acme/app: project ACME on https://example.atlassian.net\n" +
	"  account: Test User\n  issue types: Task, Epic\n  statuses: To Do, In Progress, In Review, Done\n"

func TestJiraCheck_Report(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(1)
	r.srv.SetActiveSprint(1, 4)
	out, err := r.jira("check")
	want := reportHead + "  board: 1, active sprint \"Sprint 4\"\n  flag: the Flagged field is there\n"
	if err != nil || out != want {
		t.Fatalf("err = %v\n%s", err, out)
	}
	r.srv.SetActiveSprint(1, 0)
	r.srv.DropFlagField()
	out, err = r.jira("check")
	want = reportHead + "  board: 1, no active sprint: new issues go to the backlog and won't show on the board until you start a sprint\n" +
		"  flag: no Flagged field: a waiting piece gets a comment but no flag\n"
	if err != nil || out != want {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestJiraCheck_NoBoard(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	out, err := r.jira("check")
	if err != nil || !strings.Contains(out, "  board: none: issues are not put in a sprint\n") {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestJiraCheck_RejectedCredentials(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	r.srv.Fail(401, 1)
	out, err := r.jira("check")
	if err == nil || !strings.Contains(out, "  problem: the site rejects the credentials (HTTP 401)\n") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if strings.Contains(out+err.Error(), testEmail) || strings.Contains(out+err.Error(), testToken) {
		t.Fatal("a credential is in the output")
	}
}

func TestJiraCheck_ProjectNotFound(t *testing.T) {
	r := newJiraRig(t)
	cfg := (jira.Config{Version: 1}).With("acme/app", jira.Repo{Site: testSite, Project: "NOPE"})
	if err := jira.SaveConfig(r.crewDir, cfg); err != nil {
		t.Fatal(err)
	}
	out, err := r.jira("check")
	if err == nil || !strings.Contains(out, "  problem: the project NOPE isn't found") {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestJiraCheck_MissingIssueType(t *testing.T) {
	r := newJiraRig(t)
	cfg := (jira.Config{Version: 1}).With("acme/app", jira.Repo{Site: testSite, Project: "ACME", IssueType: "Story"})
	if err := jira.SaveConfig(r.crewDir, cfg); err != nil {
		t.Fatal(err)
	}
	out, err := r.jira("check")
	if err == nil || !strings.Contains(out, `  problem: the issue type "Story" doesn't exist in ACME`) {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestJiraCheck_NotSetUp(t *testing.T) {
	r := newJiraRig(t)
	out, err := r.jira("check")
	if err != nil || out != "Jira is not set up for acme/app; run crew jira setup\n" {
		t.Fatalf("err = %v\n%q", err, out)
	}
}
