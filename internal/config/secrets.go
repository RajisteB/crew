package config

import "strings"

// The environment variables that hold Jira credentials. crew reads them; no
// station or orchestrator process it starts may see them.
const (
	EnvJiraEmail = "CREW_JIRA_EMAIL"
	EnvJiraToken = "CREW_JIRA_TOKEN"
)

// WithoutSecrets returns environ without the entries of the variables above.
// It never changes environ.
func WithoutSecrets(environ []string) []string {
	out := make([]string, 0, len(environ))
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if name == EnvJiraEmail || name == EnvJiraToken {
			continue
		}
		out = append(out, kv)
	}
	return out
}
