package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/principal"
)

const jiraUsage = `usage:
  crew jira setup --site URL --key KEY [--board N] [--assignee ACCOUNT_ID]   show this repo's missions on a Jira board (asks you to confirm in the terminal)
  crew jira check                                                           test this repo's Jira settings against the site
  crew jira sync <mission>                                                  send the Jira writes a finished or stopped mission still owes
common flags: --dir D (crew directory), --project P (project directory, default: current directory)`

const needCredentials = "set CREW_JIRA_EMAIL and CREW_JIRA_TOKEN first (an API token from id.atlassian.com); crew never stores them"

// Seams: tests point the site at jiratest and the credentials at a fake environment.
var (
	newJiraClient = func(r jira.Repo, email, token string) *jira.Client { return jira.New(r.Site, email, token) }
	jiraEnv       = os.Getenv
)

var (
	_ mission.Tracker = (*jira.Board)(nil)
	_ mission.Tracker = jira.Unavailable{}
)

// trackerFor returns the tracker for a repo: nil when jira.yaml has no entry for it,
// jira.Unavailable when credentials are missing, else a *jira.Board. It makes no
// network call.
func trackerFor(crewDir, slug string, getenv func(string) string) (t mission.Tracker, sprints bool, err error) {
	cfg, err := jira.LoadConfig(crewDir)
	if err != nil {
		return nil, false, err
	}
	repo, ok := cfg.For(slug)
	if !ok {
		return nil, false, nil
	}
	email, token, err := jira.Credentials(getenv)
	if err != nil {
		reason := "CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are not set where this engine was started"
		return jira.Unavailable{Reason: reason}, repo.Board != 0, nil
	}
	return &jira.Board{Client: newJiraClient(repo, email, token), Repo: repo}, repo.Board != 0, nil
}

type jiraRequest struct {
	crewDir, project string
	site, key        string
	board            int
	assignee         string
}

// jiraCmd handles `crew jira …`.
func jiraCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("jira", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirFlag := fs.String("dir", "", "crew directory")
	projFlag := fs.String("project", "", "project directory (default: current directory)")
	var req jiraRequest
	fs.StringVar(&req.site, "site", "", "setup: the Jira site, https://<name>.atlassian.net")
	fs.StringVar(&req.key, "key", "", "setup: the Jira project key")
	fs.IntVar(&req.board, "board", 0, "setup: the board whose active sprint gets new issues")
	fs.StringVar(&req.assignee, "assignee", "", "setup: the account id issues are assigned to")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, jiraUsage)
	}
	if len(pos) == 0 || len(pos) > 2 || (pos[0] != "sync") != (len(pos) == 1) {
		return errors.New(jiraUsage)
	}
	if req.crewDir, err = resolveDir(*dirFlag, false); err != nil {
		return err
	}
	if req.project, err = projectDir(*projFlag); err != nil {
		return err
	}
	switch pos[0] {
	case "setup":
		return jiraSetup(req, out)
	case "check":
		return jiraCheck(req, out)
	case "sync":
		return jiraSync(req, pos[1], out)
	}
	return fmt.Errorf("unknown jira command %q\n%s", pos[0], jiraUsage)
}

func repoSlug(project string) (string, error) {
	info, err := ghClient(project).Repo(context.Background())
	if err != nil {
		return "", fmt.Errorf("can't reach GitHub: %w", err)
	}
	return info.Slug, nil
}

func jiraSetup(req jiraRequest, out io.Writer) error {
	email, token, err := jira.Credentials(jiraEnv)
	if err != nil {
		return errors.New(needCredentials)
	}
	repo := jira.Repo{Site: strings.TrimRight(req.site, "/"), Project: req.key, Board: req.board, Assignee: req.assignee}
	if err := repo.Validate(); err != nil {
		return err
	}
	repo = repo.WithDefaults()
	slug, err := repoSlug(req.project)
	if err != nil {
		return err
	}
	cfg, err := jira.LoadConfig(req.crewDir)
	if err != nil {
		return err
	}
	tty, err := openTTY()
	if err != nil {
		return fmt.Errorf("crew jira setup needs a terminal: %w", err)
	}
	defer tty.Close()
	client := newJiraClient(repo, email, token)
	if repo.Board == 0 {
		var line string
		repo.Board, line = chooseBoard(context.Background(), client, repo.Project)
		fmt.Fprintln(out, line)
	}
	if n := jiraReport(context.Background(), client, slug, repo, out); n > 0 {
		return errors.New("Jira can't be used as set; nothing was written")
	}
	return confirmAndSave(req, tty, cfg, slug, repo, out)
}

// confirmAndSave asks on the terminal, and only then writes the entry.
func confirmAndSave(req jiraRequest, tty io.ReadWriter, cfg jira.Config, slug string, repo jira.Repo, out io.Writer) error {
	phrase := slug + " " + repo.Project
	fmt.Fprintf(tty, "\nRepo:     %s\nJira:     project %s on %s\n\nmissions in this repo will create issues, comments and attachments there.\n\n", slug, repo.Project, repo.Site)
	if err := principal.Confirm(tty, fmt.Sprintf(`Type "%s" to confirm: `, phrase), phrase); err != nil {
		return err
	}
	if err := jira.SaveConfig(req.crewDir, cfg.With(slug, repo)); err != nil {
		return err
	}
	fmt.Fprintf(out, "Jira is set up for %s. New missions in this repo will appear in %s.\n", slug, repo.Project)
	return nil
}

// chooseBoard picks the project's only board, or none, and says which.
func chooseBoard(ctx context.Context, c *jira.Client, project string) (int, string) {
	boards, err := c.Boards(ctx, project)
	switch {
	case err != nil:
		return 0, fmt.Sprintf("can't list the boards of %s (%v): issues are not put in a sprint", project, err)
	case len(boards) == 1:
		return boards[0].ID, ""
	case len(boards) == 0:
		return 0, fmt.Sprintf("no board found for %s: issues are not put in a sprint", project)
	}
	shown := make([]string, 0, 3)
	for _, b := range boards[:2] {
		shown = append(shown, fmt.Sprintf("%d %q", b.ID, b.Name))
	}
	if len(boards) > 2 {
		shown = append(shown, "…")
	}
	return 0, fmt.Sprintf("%s has %d boards (%s): pass --board N to use sprints", project, len(boards), strings.Join(shown, ", "))
}

func jiraCheck(req jiraRequest, out io.Writer) error {
	slug, err := repoSlug(req.project)
	if err != nil {
		return err
	}
	cfg, err := jira.LoadConfig(req.crewDir)
	if err != nil {
		return err
	}
	repo, ok := cfg.For(slug)
	if !ok {
		fmt.Fprintf(out, "Jira is not set up for %s; run crew jira setup\n", slug)
		return nil
	}
	email, token, err := jira.Credentials(jiraEnv)
	if err != nil {
		return errors.New(needCredentials)
	}
	if n := jiraReport(context.Background(), newJiraClient(repo, email, token), slug, repo, out); n > 0 {
		return fmt.Errorf("%d problem(s) with the Jira settings for %s", n, slug)
	}
	return nil
}

// jiraSync sends the writes a mission still owes, once, as the mission's only writer.
func jiraSync(req jiraRequest, id string, out io.Writer) error {
	root, err := (gitx.Repo{Dir: req.project}).Root(context.Background())
	if err != nil {
		return fmt.Errorf("crew jira sync needs a git repository: %w", err)
	}
	store, err := mission.Open(root, id)
	if err != nil {
		return err
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	if err := jiraCredentialsCheck(req.crewDir, st.Repo); err != nil {
		return err
	}
	unlock, err := store.Lock()
	if err != nil {
		if _, live := store.LockedBy(); live {
			return fmt.Errorf("mission %s has a live engine, which sends Jira writes itself", id)
		}
		return err
	}
	defer unlock()
	e, runs, err := newMissionEngine(store, req.crewDir, root, out)
	if err != nil {
		return err
	}
	defer runs.Wait()
	if e.Tracker == nil {
		return fmt.Errorf("Jira is not set up for %s; run crew jira setup", st.Repo)
	}
	owed, err := e.SyncTracker(context.Background())
	if err != nil {
		return err
	}
	if owed == 0 {
		fmt.Fprintf(out, "Jira is up to date for mission %s\n", id)
		return nil
	}
	fmt.Fprintf(out, "%d writes still not sent; see the notes in crew mission status %s\n", owed, id)
	return nil
}
