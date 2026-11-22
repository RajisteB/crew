package gh

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealExec_ChildHasNoJiraCredentials runs a stand-in gh (a script on
// PATH, never the real binary) that prints its environment.
func TestRealExec_ChildHasNoJiraCredentials(t *testing.T) {
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, binary), []byte("#!/bin/sh\nenv\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("CREW_JIRA_EMAIL", "someone@example.com")
	t.Setenv("CREW_JIRA_TOKEN", "secret-token")
	out, err := realExec(context.Background(), t.TempDir(), nil, binary, "pr", "view")
	if err != nil {
		t.Fatalf("exec: %v", err)
	}
	env := string(out)
	if !strings.Contains(env, "GH_PROMPT_DISABLED=1") {
		t.Fatalf("the site's own variables are missing:\n%s", env)
	}
	if strings.Contains(env, "CREW_JIRA_EMAIL") || strings.Contains(env, "CREW_JIRA_TOKEN") {
		t.Fatalf("gh's environment holds the Jira credentials:\n%s", env)
	}
}
