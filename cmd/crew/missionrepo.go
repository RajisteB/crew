package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/principal"
)

// confirmRepo is the one repo check: the repo gh sees must be the repo that
// origin points at, and it must be confirmed at its current visibility.
// Start, go, resume and the engine's gate before every push all use it.
func confirmRepo(ctx context.Context, crewDir, repoRoot string, info gh.RepoInfo) error {
	if err := checkOriginMatches(ctx, repoRoot, info.Slug); err != nil {
		return err
	}
	return principal.CheckRepo(principal.Store{Dir: filepath.Join(crewDir, "principal")}, info)
}

// repoGate is the engine's check before a push. It reads the repo again, so
// a visibility change or a removed confirmation is seen even days into a mission.
func repoGate(crewDir, repoRoot string) func(context.Context) error {
	return func(ctx context.Context) error {
		info, err := ghClient(repoRoot).Repo(ctx)
		if err != nil {
			return fmt.Errorf("can't reach GitHub: %w", err)
		}
		return confirmRepo(ctx, crewDir, repoRoot, info)
	}
}

// checkOriginMatches refuses a repo whose origin remote is not the repository
// gh reports: pushes go to origin, the confirmation is about gh's repo. An
// origin that has no owner/repo form (a local path) or can't be read is not
// compared; a push to it fails visibly on its own.
func checkOriginMatches(ctx context.Context, repoRoot, ghSlug string) error {
	url, err := gitx.Repo{Dir: repoRoot}.RemoteURL(ctx, "origin")
	if err != nil {
		return nil
	}
	slug, ok := gitx.SlugFromURL(url)
	if !ok || strings.EqualFold(slug, ghSlug) {
		return nil
	}
	return fmt.Errorf("the git remote origin is %s but gh works on %s, so a pull request would go to a repository you didn't confirm; run gh repo set-default %s (or point origin at %s)", slug, ghSlug, slug, ghSlug)
}
