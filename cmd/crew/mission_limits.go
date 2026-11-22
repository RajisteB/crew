package main

import (
	"fmt"

	"github.com/crewfile-dev/crew/internal/mission"
)

// runsAtOnceLine tells the principal how many pieces may run at once across
// the repo's missions.
func runsAtOnceLine(l mission.Limits) string {
	n := "no limit"
	if !l.Concurrency.Unlimited {
		n = fmt.Sprint(l.Concurrency.N)
	}
	return fmt.Sprintf("Runs at once: %s across this repo's missions (limits.runs_at_once in missions.yaml)", n)
}

// printRunsAtOnceLine prints the line after the auto-merge line. Settings
// that can't be read print nothing: the commands report that elsewhere.
func printRunsAtOnceLine(env missionEnv, slug string) {
	if slug == "" {
		return
	}
	settings, err := mission.LoadSettings(env.crewDir)
	if err != nil {
		return
	}
	fmt.Fprintln(env.out, runsAtOnceLine(settings.For(slug)))
}
