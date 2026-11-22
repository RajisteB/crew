package run_test

import (
	"context"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestHeadlessRunner_DropsJiraCredentials(t *testing.T) {
	t.Setenv("FAKE_MODE", "env")
	t.Setenv("CREW_JIRA_EMAIL", "me@example.com")
	t.Setenv("CREW_JIRA_TOKEN", "s3cret")
	out, err := run.HeadlessRunner{Registry: fakeRegistry(t)}.Run(context.Background(), req("p"))
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"CREW_JIRA_TOKEN", "CREW_JIRA_EMAIL", "s3cret"} {
		if strings.Contains(out.Text, bad) {
			t.Fatalf("station environment leaks %q:\n%s", bad, out.Text)
		}
	}
	if !strings.Contains(out.Text, "PATH=") {
		t.Fatalf("station environment lost PATH:\n%s", out.Text)
	}
}
