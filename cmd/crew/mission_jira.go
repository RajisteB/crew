package main

import (
	"fmt"

	"github.com/crewfile-dev/crew/internal/jira"
)

// printJiraLine says where the mission's Jira issues will go. It reads only
// jira.yaml.
func printJiraLine(env missionEnv, slug string) {
	if slug == "" {
		return
	}
	cfg, err := jira.LoadConfig(env.crewDir)
	if err != nil {
		fmt.Fprintf(env.out, "Jira: %s\n", safeText(err.Error()))
		return
	}
	r, ok := cfg.For(slug)
	if !ok {
		fmt.Fprintf(env.out, "Jira: not set up for %s; deliverables stay in the mission folder (crew jira setup)\n", safeText(slug))
		return
	}
	fmt.Fprintf(env.out, "Jira: project %s on %s\n", r.Project, r.Site)
}

// jiraStartCheck refuses to start an engine that has a Jira project to write
// to but no credentials. It makes no network call.
func jiraStartCheck(env missionEnv, slug string) error {
	return jiraCredentialsCheck(env.crewDir, slug)
}

// jiraCredentialsCheck is that check for a crew directory and a repo.
func jiraCredentialsCheck(crewDir, slug string) error {
	cfg, err := jira.LoadConfig(crewDir)
	if err != nil {
		return err
	}
	if _, ok := cfg.For(slug); !ok {
		return nil
	}
	if _, _, err := jira.Credentials(jiraEnv); err != nil {
		return fmt.Errorf("Jira is set up for %s, but %v here; set it, or remove the repo from jira.yaml", slug, err)
	}
	return nil
}
