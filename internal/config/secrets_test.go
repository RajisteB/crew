package config_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
)

func TestWithoutSecrets(t *testing.T) {
	in := []string{"A=1", "CREW_JIRA_TOKEN=x", "CREW_JIRA_EMAIL=y", "CREW_JIRA_TOKENS=z"}
	orig := slices.Clone(in)
	got := config.WithoutSecrets(in)
	if want := []string{"A=1", "CREW_JIRA_TOKENS=z"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if !slices.Equal(in, orig) {
		t.Fatalf("input changed: %v", in)
	}
}

func TestCleanEnv_DropsTheCredentials(t *testing.T) {
	t.Setenv(config.EnvJiraEmail, "someone@example.com")
	t.Setenv(config.EnvJiraToken, "secret-token")
	t.Setenv("CREW_TEST_KEEP", "1")
	got := config.CleanEnv()
	for _, kv := range got {
		if strings.HasPrefix(kv, config.EnvJiraEmail+"=") || strings.HasPrefix(kv, config.EnvJiraToken+"=") {
			t.Fatalf("CleanEnv kept %q", kv)
		}
	}
	if !slices.Contains(got, "CREW_TEST_KEEP=1") {
		t.Fatalf("CleanEnv dropped an ordinary variable: %v", got)
	}
}
