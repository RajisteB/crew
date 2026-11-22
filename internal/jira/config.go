package jira

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/crewfile-dev/crew/internal/config"
)

// ConfigFile is the name of the per-repo Jira config inside the crew directory.
const ConfigFile = "jira.yaml"

const configHeader = `# jira.yaml — where crew shows missions in Jira, one entry per repo.
# ` + "`crew jira setup`" + ` writes this file; you can edit it. Credentials are never
# stored here: crew reads CREW_JIRA_EMAIL and CREW_JIRA_TOKEN from the environment.
`

var projectKey = regexp.MustCompile(`^[A-Z][A-Z0-9_]{1,9}$`)

// Statuses names the Jira statuses crew moves an issue between.
type Statuses struct {
	ToDo       string `yaml:"to_do"`
	InProgress string `yaml:"in_progress"`
	InReview   string `yaml:"in_review"`
	Done       string `yaml:"done"`
}

// Repo is how one repo's missions show in Jira.
type Repo struct {
	Site      string   `yaml:"site"`
	Project   string   `yaml:"project"`
	Board     int      `yaml:"board"`      // 0: issues are not put in a sprint
	Assignee  string   `yaml:"assignee"`   // an account id; "" means the account the token belongs to
	IssueType string   `yaml:"issue_type"` // default Task
	EpicType  string   `yaml:"epic_type"`  // default Epic
	Statuses  Statuses `yaml:"statuses"`   // defaults: To Do, In Progress, In Review, Done
}

// Config is the content of jira.yaml.
type Config struct {
	Version int             `yaml:"version"`
	Repos   map[string]Repo `yaml:"repos"` // "owner/name"
}

// LoadConfig reads <crewDir>/jira.yaml. A missing file is an empty Config and no error.
// Every Repo it returns has its defaults filled in and has passed Validate.
func LoadConfig(crewDir string) (Config, error) {
	b, err := os.ReadFile(filepath.Join(crewDir, ConfigFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	var raw struct {
		Version int                  `yaml:"version"`
		Repos   map[string]yaml.Node `yaml:"repos"`
	}
	if err := decodeStrict(b, &raw); err != nil {
		return Config{}, fmt.Errorf("%s: %w", ConfigFile, err)
	}
	repos := make(map[string]Repo, len(raw.Repos))
	for name, node := range raw.Repos {
		r, err := decodeRepo(node)
		if err == nil {
			r = r.withDefaults()
			err = r.Validate()
		}
		if err != nil {
			return Config{}, fmt.Errorf("%s: repos.%s: %w", ConfigFile, name, err)
		}
		repos[name] = r
	}
	return Config{Version: raw.Version, Repos: repos}, nil
}

// decodeRepo decodes one repo's entry on its own, so an unknown key in it is
// attributed to that repo.
func decodeRepo(node yaml.Node) (Repo, error) {
	b, err := yaml.Marshal(&node)
	if err != nil {
		return Repo{}, err
	}
	var r Repo
	if err := decodeStrict(b, &r); err != nil {
		return Repo{}, err
	}
	return r, nil
}

func decodeStrict(b []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// For returns the entry for repo ("owner/name").
func (c Config) For(repo string) (Repo, bool) {
	r, ok := c.Repos[repo]
	return r, ok
}

// With returns a copy of c with repo set to r. c is not changed.
func (c Config) With(repo string, r Repo) Config {
	repos := maps.Clone(c.Repos)
	if repos == nil {
		repos = map[string]Repo{}
	}
	repos[repo] = r
	c.Repos = repos
	return c
}

// SaveConfig writes <crewDir>/jira.yaml through a temp file and a rename, mode 0600.
func SaveConfig(crewDir string, c Config) error {
	if c.Version == 0 {
		c.Version = 1
	}
	body, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Errorf("%s: %w", ConfigFile, err)
	}
	tmp, err := os.CreateTemp(crewDir, ConfigFile+".*.tmp")
	if err != nil {
		return fmt.Errorf("%s: %w", ConfigFile, err)
	}
	defer os.Remove(tmp.Name())
	_, werr := tmp.WriteString(configHeader + string(body))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr, os.Chmod(tmp.Name(), 0o600)); err != nil {
		return fmt.Errorf("%s: %w", ConfigFile, err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(crewDir, ConfigFile)); err != nil {
		return fmt.Errorf("%s: %w", ConfigFile, err)
	}
	return nil
}

// WithDefaults returns r with the site's trailing slashes cut and every empty
// issue type and status name set to its default.
func (r Repo) WithDefaults() Repo { return r.withDefaults() }

func (r Repo) withDefaults() Repo {
	r.Site = strings.TrimRight(r.Site, "/")
	fill := func(v *string, def string) {
		if *v == "" {
			*v = def
		}
	}
	fill(&r.IssueType, "Task")
	fill(&r.EpicType, "Epic")
	fill(&r.Statuses.ToDo, "To Do")
	fill(&r.Statuses.InProgress, "In Progress")
	fill(&r.Statuses.InReview, "In Review")
	fill(&r.Statuses.Done, "Done")
	return r
}

// Validate checks the site, project key and board. Errors carry no prefix.
func (r Repo) Validate() error {
	if !validSite(r.Site) {
		return fmt.Errorf("site must be https://<name>.atlassian.net, got %q", r.Site)
	}
	if !projectKey.MatchString(r.Project) {
		return fmt.Errorf("project must be a Jira project key like ACME, got %q", r.Project)
	}
	if r.Board < 0 {
		return fmt.Errorf("board must be 0 or more, got %d", r.Board)
	}
	return nil
}

func validSite(site string) bool {
	u, err := url.Parse(site)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	host := u.Hostname()
	if !strings.HasSuffix(host, ".atlassian.net") || host == ".atlassian.net" {
		return false
	}
	return (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == "" && u.Port() == ""
}

// Credentials reads the two variables through getenv. It never puts a value in an error.
func Credentials(getenv func(string) string) (email, token string, err error) {
	email, token = getenv(config.EnvJiraEmail), getenv(config.EnvJiraToken)
	switch {
	case email == "" && token == "":
		return "", "", fmt.Errorf("%s and %s are not set", config.EnvJiraEmail, config.EnvJiraToken)
	case email == "":
		return "", "", fmt.Errorf("%s is not set", config.EnvJiraEmail)
	case token == "":
		return "", "", fmt.Errorf("%s is not set", config.EnvJiraToken)
	}
	return email, token, nil
}
