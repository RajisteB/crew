package main

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

// confirmRepo is the one repo check: the repo gh sees must be confirmed at
// its current visibility. Start, go, resume and the engine's gate before
// every push all use it.
func confirmRepo(crewDir string, info gh.RepoInfo) error {
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
		return confirmRepo(crewDir, info)
	}
}
