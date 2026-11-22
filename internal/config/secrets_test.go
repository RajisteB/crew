package config_test

import (
	"slices"
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
