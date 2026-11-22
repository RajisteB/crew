package main

import (
	"fmt"
	"io"
	"strings"

	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/mission"
)

// printJiraStatus is the line under the mission's first line that names its
// epic and says how many writes Jira is still owed. It reads jira.yaml and
// the mission's own files and makes no network call.
func printJiraStatus(env missionEnv, store mission.Store, m mission.Mission, st mission.State) error {
	repo, entry := jiraEntry(env.crewDir, st.Repo)
	if st.Jira.Epic == "" && (!entry || !begun(st)) {
		return nil
	}
	if st.Jira.Epic == "" {
		// Without an epic the plan holds only its create: a count would mislead.
		fmt.Fprintln(env.out, "Jira: nothing has reached the board yet")
		return nil
	}
	evs, err := store.EventsSince(st.Jira.Seq)
	if err != nil {
		return err
	}
	owed := mission.Owed(mission.TrackerOps(m, st, evs, entry && repo.Board != 0))
	line := "Jira: " + st.Jira.Epic
	if entry {
		line += "  " + repo.Site + "/browse/" + st.Jira.Epic
	}
	if owed > 0 {
		line += " (" + writesText(owed) + " not sent yet)"
	}
	fmt.Fprintln(env.out, safeText(line))
	return nil
}

// writesText is "1 write" or "n writes".
func writesText(n int) string {
	if n == 1 {
		return "1 write"
	}
	return fmt.Sprintf("%d writes", n)
}

// begun says whether the mission has been released to run.
func begun(st mission.State) bool {
	return st.Status != mission.StatusPlanned && st.Status != mission.StatusAwaitingGo
}

// jiraEntry is the repo's entry in jira.yaml; false when there is none or the
// file can't be read.
func jiraEntry(crewDir, slug string) (jira.Repo, bool) {
	cfg, err := jira.LoadConfig(crewDir)
	if err != nil {
		return jira.Repo{}, false
	}
	return cfg.For(slug)
}

// withKey is a piece id followed by its Jira issue, as "P1 (ACME-12)".
func withKey(p mission.PieceState) string {
	if p.Jira.Issue == "" {
		return p.ID
	}
	return p.ID + " (" + p.Jira.Issue + ")"
}

// printDeliverables lists the deliverables pieces have left, each with its
// files, whether it is accepted, and where crew keeps it.
func printDeliverables(out io.Writer, st mission.State) {
	var lines []string
	for _, p := range st.Pieces {
		d := p.Deliverable
		if d == nil {
			continue
		}
		state := "waiting on you"
		switch {
		case d.Accepted:
			state = "accepted by " + d.By
		case p.Status == mission.PieceCancelled:
			state = "cancelled"
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s  %s", withKey(p), strings.Join(d.Files, ", "), state,
			mission.DeliverablesRel(st.MissionID, p.ID)))
	}
	section(out, "Deliverables", lines)
}
