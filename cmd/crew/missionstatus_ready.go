package main

import (
	"fmt"

	"github.com/crewfile-dev/crew/internal/mission"
)

// readyToStart is what status shows for a queued piece that nothing in its
// own plan or in another mission keeps back.
const readyToStart = "ready to start"

// readyText says why a piece that is ready hasn't started yet: the mission is
// on hold, or one more run would pass runs_at_once across the repo's missions
// that are being driven. Otherwise it is readyToStart.
func readyText(env missionEnv, st mission.State) string {
	if st.Hold != nil && st.Status != mission.StatusDone {
		return "on hold"
	}
	settings, err := mission.LoadSettings(env.crewDir)
	if err != nil {
		return readyToStart
	}
	limit := settings.For(st.Repo).Concurrency
	if limit.Unlimited {
		return readyToStart
	}
	elsewhere, err := mission.RunsElsewhere(env.repoRoot, st.MissionID)
	if err != nil {
		return "nothing starts while the repo's other missions can't be read"
	}
	used := mission.HoldingRuns(st) + elsewhere
	if mission.Allows(limit, used+1) {
		return readyToStart
	}
	return fmt.Sprintf("waiting for a free run: %d of %d in use across this repo's missions", used, limit.N)
}
