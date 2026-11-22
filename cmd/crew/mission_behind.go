package main

import (
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/source"
)

// domainMap indexes the project's domains by name, as the engine wants them.
func domainMap(domains []source.Domain) map[string]source.Domain {
	out := make(map[string]source.Domain, len(domains))
	for _, d := range domains {
		out[d.Name] = d
	}
	return out
}

// printOthersOverlap prints one line for each unfinished piece of the mission
// that has not started and whose paths overlap a piece another mission in the
// repo holds. It saves
// nothing: the check that counts is the one made when the piece starts.
func printOthersOverlap(out io.Writer, repoRoot string, st mission.State, m mission.Mission, domains []source.Domain) {
	byName := domainMap(domains)
	claims, err := mission.ClaimsElsewhere(repoRoot, st.MissionID, byName)
	if err != nil {
		fmt.Fprintf(out, "can't read the repo's other missions: %v\n", err)
		return
	}
	started := mission.StartedPieces(st)
	for _, p := range m.Pieces {
		if started[p.ID] {
			continue
		}
		if b, blocked := mission.BlockedBy(mission.EffectivePaths(p, byName[p.Domain]), claims); blocked {
			fmt.Fprintf(out, "%s starts after %s\n", p.ID, b)
		}
	}
}

// printOthersOverlapOnStart is printOthersOverlap for a mission already saved.
func printOthersOverlapOnStart(env missionEnv, store mission.Store, m mission.Mission, domains []source.Domain) error {
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	printOthersOverlap(env.out, env.repoRoot, st, m, domains)
	return nil
}

// behindText is what the status shows for a piece kept behind another
// mission's piece, or "" when it is not.
func behindText(st mission.State, id string) string {
	p, ok := st.Piece(id)
	if !ok || p.Behind == nil {
		return ""
	}
	stopped := ""
	if p.Behind.Stopped {
		stopped = mission.NotRunningSuffix
	}
	return fmt.Sprintf("starts after %s of mission %s%s (paths overlap)", p.Behind.Piece, p.Behind.Mission, stopped)
}
