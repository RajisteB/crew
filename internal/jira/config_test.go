package jira_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/jira"
)

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, jira.ConfigFile), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadConfig_MissingFileIsEmpty(t *testing.T) {
	c, err := jira.LoadConfig(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Repos) != 0 {
		t.Fatalf("want empty config, got %+v", c)
	}
}

func TestLoadConfig_FillsDefaults(t *testing.T) {
	dir := writeConfig(t, "version: 1\nrepos:\n  acme/app:\n    site: https://example.atlassian.net/\n    project: ACME\n")
	c, err := jira.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := c.For("acme/app")
	if !ok {
		t.Fatal("repo missing")
	}
	want := jira.Statuses{ToDo: "To Do", InProgress: "In Progress", InReview: "In Review", Done: "Done"}
	if r.IssueType != "Task" || r.EpicType != "Epic" || r.Statuses != want {
		t.Fatalf("defaults not filled: %+v", r)
	}
	if r.Site != "https://example.atlassian.net" {
		t.Fatalf("trailing slash kept: %q", r.Site)
	}
}

func TestLoadConfig_Refuses(t *testing.T) {
	cases := map[string]string{
		"http":     "site: http://example.atlassian.net\n    project: ACME",
		"host":     "site: https://example.com\n    project: ACME",
		"path":     "site: https://evil.example/x.atlassian.net\n    project: ACME",
		"userinfo": "site: https://user:pw@example.atlassian.net\n    project: ACME",
		"wiki":     "site: https://example.atlassian.net/wiki\n    project: ACME",
		"lower":    "site: https://example.atlassian.net\n    project: acme",
		"short":    "site: https://example.atlassian.net\n    project: A",
		"board":    "site: https://example.atlassian.net\n    project: ACME\n    board: -1",
		"unknown":  "site: https://example.atlassian.net\n    project: ACME\n    colour: red",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeConfig(t, "version: 1\nrepos:\n  acme/app:\n    "+body+"\n")
			_, err := jira.LoadConfig(dir)
			if err == nil {
				t.Fatal("want an error")
			}
			if name != "unknown" && !strings.Contains(err.Error(), "jira.yaml: repos.acme/app: ") {
				t.Fatalf("error does not name the repo: %v", err)
			}
		})
	}
}

func TestSaveConfig_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	c := jira.Config{Version: 1}.With("acme/app", jira.Repo{Site: "https://example.atlassian.net", Project: "ACME", Board: 3, Assignee: "abc"})
	if err := jira.SaveConfig(dir, c); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, jira.ConfigFile)
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
	b, _ := os.ReadFile(path)
	if !strings.HasPrefix(string(b), "# jira.yaml — where crew shows missions in Jira") {
		t.Fatalf("header missing:\n%s", b)
	}
	got, err := jira.LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := got.For("acme/app")
	if r.Board != 3 || r.Assignee != "abc" || r.Project != "ACME" || r.IssueType != "Task" {
		t.Fatalf("round trip: %+v", r)
	}
	if _, ok := c.For("acme/app"); !ok {
		t.Fatal("With changed or lost the original")
	}
}

func TestWith_DoesNotChangeReceiver(t *testing.T) {
	c := jira.Config{Version: 1}
	_ = c.With("acme/app", jira.Repo{})
	if len(c.Repos) != 0 {
		t.Fatal("receiver changed")
	}
}

func TestCredentials(t *testing.T) {
	env := map[string]string{"CREW_JIRA_EMAIL": "me@example.com", "CREW_JIRA_TOKEN": "tok"}
	get := func(k string) string { return env[k] }
	e, tk, err := jira.Credentials(get)
	if err != nil || e != "me@example.com" || tk != "tok" {
		t.Fatalf("got %q %q %v", e, tk, err)
	}
	delete(env, "CREW_JIRA_TOKEN")
	_, _, err = jira.Credentials(get)
	if err == nil || !strings.Contains(err.Error(), "CREW_JIRA_TOKEN") ||
		strings.Contains(err.Error(), "CREW_JIRA_EMAIL") || strings.Contains(err.Error(), "me@example.com") {
		t.Fatalf("bad error: %v", err)
	}
	delete(env, "CREW_JIRA_EMAIL")
	_, _, err = jira.Credentials(get)
	if err == nil || err.Error() != "CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are not set" {
		t.Fatalf("bad error: %v", err)
	}
}
