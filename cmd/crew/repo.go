package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/principal"
)

const repoUsage = `usage:
  crew repo confirm [--project P] [--dir D]   confirm this repo once, in a terminal
  crew repo status  [--project P] [--dir D]   show whether this repo is confirmed`

// openTTY and ghClient are variables so tests can replace them.
var (
	openTTY  = principal.OpenTTY
	ghClient = func(dir string) gh.Client { return gh.Client{Dir: dir} }
)

// repoCmd handles `crew repo …`.
func repoCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("repo", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirFlag := fs.String("dir", "", "crew directory")
	projFlag := fs.String("project", "", "project root (default: current directory)")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, repoUsage)
	}
	if len(pos) != 1 || (pos[0] != "confirm" && pos[0] != "status") {
		return errors.New(repoUsage)
	}
	dir, err := resolveDir(*dirFlag, false)
	if err != nil {
		return err
	}
	project, err := projectDir(*projFlag)
	if err != nil {
		return err
	}
	store := principal.Store{Dir: filepath.Join(dir, "principal")}
	if pos[0] == "status" {
		return repoStatus(store, project, out)
	}
	return repoConfirm(store, project, out)
}

func repoStatus(store principal.Store, project string, out io.Writer) error {
	info, err := ghClient(project).Repo(context.Background())
	if err != nil {
		return err
	}
	rec, ok, err := store.Repo(info.Slug)
	if err == nil && !ok {
		fmt.Fprintf(out, "%s: not confirmed; run crew repo confirm in a terminal\n", info.Slug)
		return nil
	}
	if err == nil {
		err = principal.CheckRepo(store, info)
	}
	if err != nil {
		fmt.Fprintf(out, "%s: %v\n", info.Slug, err)
		return nil
	}
	fmt.Fprintf(out, "%s: confirmed as %s (%s)\n", info.Slug, rec.Visibility, rec.ConfirmedAt.Format(time.DateOnly))
	return nil
}

func repoConfirm(store principal.Store, project string, out io.Writer) error {
	tty, err := openTTY()
	if err != nil {
		return fmt.Errorf("crew repo confirm needs a terminal: %w", err)
	}
	defer tty.Close()
	info, err := ghClient(project).Repo(context.Background())
	if err != nil {
		return err
	}
	remote, err := gitx.Repo{Dir: project}.RemoteURL("origin")
	if err != nil {
		remote = "(no origin remote)"
	}
	visibility := info.Visibility
	if visibility == "public" {
		visibility = "PUBLIC"
	}
	fmt.Fprintf(tty, "\nRepo:       %s\nVisibility: %s\nRemote:     %s\n\nmissions will push piece branches and open PRs here.\n\n", info.Slug, visibility, remote)
	if err := principal.Confirm(tty, fmt.Sprintf("Type %s to confirm: ", info.Slug), info.Slug); err != nil {
		return err
	}
	rec := principal.RepoRecord{Repo: info.Slug, Visibility: info.Visibility, ConfirmedAt: time.Now().UTC()}
	if err := store.ConfirmRepo(rec); err != nil {
		return err
	}
	fmt.Fprintf(out, "confirmed %s as %s\n", info.Slug, info.Visibility)
	return nil
}
