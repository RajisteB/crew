package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/run"
)

const recentEvents = 20

// missionNow is the clock for how long a wait has lasted; tests replace it.
var missionNow = time.Now

func waitingCount(st mission.State) int {
	n := 0
	for _, p := range st.Pieces {
		if p.Wait != nil {
			n++
		}
	}
	return n
}

// missionList prints one line per mission, newest first.
func missionList(env missionEnv) error {
	states, err := mission.List(env.repoRoot)
	if err != nil {
		return err
	}
	if len(states) == 0 {
		fmt.Fprintln(env.out, "no missions")
		return nil
	}
	for _, st := range states {
		fmt.Fprintf(env.out, "%s  %s  %d pieces, %d waiting on you%s\n", st.MissionID, st.Status, len(st.Pieces), waitingCount(st), deadEngineSuffix(env, st))
	}
	return nil
}

// deadEngineText is what a running mission without a live engine says.
func deadEngineText(id string) string {
	return "engine not running; crew mission resume " + id
}

// deadEngineSuffix is "  engine not running; …" for a mission whose status
// is running but whose engine is gone (after a reboot, say), else "".
func deadEngineSuffix(env missionEnv, st mission.State) string {
	if st.Status != mission.StatusRunning {
		return ""
	}
	store, err := mission.Open(env.repoRoot, st.MissionID)
	if err != nil {
		return ""
	}
	if _, live := store.LockedBy(); live {
		return ""
	}
	return "  " + deadEngineText(st.MissionID)
}

func missionStatus(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	m, err := store.Mission()
	if err != nil {
		return err
	}
	live := "not running"
	if pid, ok := store.LockedBy(); ok {
		live = fmt.Sprintf("live, pid %d", pid)
	} else if st.Status == mission.StatusRunning {
		live = deadEngineText(id)
	}
	out := env.out
	fmt.Fprintln(out, safeText(fmt.Sprintf("mission %s  %s  %s  (%s, base %s)", id, st.Status, live, st.Repo, st.Base)))
	printWaiting(out, st)
	printRunning(out, env.repoRoot, st)
	printQueued(out, m, st)
	printEnded(out, "Complete", st, mission.PieceComplete)
	printEnded(out, "Cancelled", st, mission.PieceCancelled)
	return printEvents(out, store, env.flags.since)
}

func section(out io.Writer, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(out, "\n%s:\n", title)
	for _, l := range lines {
		fmt.Fprintf(out, "  %s\n", safeText(l))
	}
}

func printWaiting(out io.Writer, st mission.State) {
	var lines []string
	for _, p := range st.Pieces {
		if p.Wait == nil {
			continue
		}
		w := p.Wait
		detail := w.Question
		if w.Kind == mission.WaitMerge || w.Kind == mission.WaitPRClosed {
			detail = p.PRURL
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s  %s  waiting %s", w.ID, p.ID, w.Kind, oneLine(detail), sinceText(w.Since)))
	}
	section(out, "Waiting on you", lines)
}

func printRunning(out io.Writer, repoRoot string, st mission.State) {
	var lines []string
	for _, p := range st.Pieces {
		switch {
		case p.Status == mission.PieceRunning && p.Wait == nil:
			lines = append(lines, fmt.Sprintf("%s  run %s  %s", p.ID, p.RunID, runPosition(repoRoot, p.RunID)))
		case p.Status == mission.PiecePROpen && p.Wait == nil:
			lines = append(lines, fmt.Sprintf("%s  PR open  %s", p.ID, p.PRURL))
		}
	}
	section(out, "Running", lines)
}

// runPosition is the run's current station and step, read from its folder.
func runPosition(repoRoot, runID string) string {
	store, err := run.OpenRun(repoRoot, runID)
	if err != nil {
		return "run state unavailable"
	}
	rs, err := store.LoadState()
	if err != nil {
		return "run state unavailable"
	}
	return fmt.Sprintf("station %s, step %d", rs.Current, rs.Step)
}

func printQueued(out io.Writer, m mission.Mission, st mission.State) {
	var lines []string
	for _, p := range st.Pieces {
		if p.Status != mission.PieceQueued || p.Wait != nil {
			continue
		}
		lines = append(lines, fmt.Sprintf("%s  %s", p.ID, waitsText(m, st, p.ID)))
	}
	section(out, "Queued", lines)
}

func waitsText(m mission.Mission, st mission.State, id string) string {
	piece, _ := m.Piece(id)
	var open []string
	for _, w := range piece.WaitsFor {
		if dep, ok := st.Piece(w); !ok || dep.Status != mission.PieceComplete {
			open = append(open, w)
		}
	}
	if len(open) == 0 {
		return "ready to start"
	}
	return "waits for " + strings.Join(open, ", ")
}

func printEnded(out io.Writer, title string, st mission.State, status mission.PieceStatus) {
	var ids []string
	for _, p := range st.Pieces {
		if p.Status == status {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) > 0 {
		section(out, title, []string{strings.Join(ids, ", ")})
	}
}

func printEvents(out io.Writer, store mission.Store, since int) error {
	all, err := store.EventsSince(0)
	if err != nil {
		return err
	}
	cursor, shown := 0, all
	if n := len(all); n > 0 {
		cursor = all[n-1].Seq
	}
	if since >= 0 {
		shown = nil
		for _, e := range all {
			if e.Seq > since {
				shown = append(shown, e)
			}
		}
		cursor = max(cursor, since)
	} else if len(shown) > recentEvents {
		shown = shown[len(shown)-recentEvents:]
	}
	var lines []string
	for _, e := range shown {
		lines = append(lines, eventLine(e))
	}
	section(out, "Events", lines)
	fmt.Fprintf(out, "\ncursor %d\n", cursor)
	return nil
}

func eventLine(e mission.Event) string {
	parts := []string{fmt.Sprint(e.Seq), e.Time.Format("15:04:05"), oneLine(e.Kind)}
	if e.Piece != "" {
		parts = append(parts, oneLine(e.Piece))
	}
	if e.Msg != "" {
		parts = append(parts, oneLine(e.Msg))
	}
	return strings.Join(parts, "  ")
}

// oneLine collapses whitespace, newlines included, and makes the text safe
// to print.
func oneLine(s string) string {
	return safeText(strings.Join(strings.Fields(s), " "))
}

// safeText replaces the control characters of s (below 0x20 except tab, 0x7f
// and the C1 range) with "?". Mission text written by a model reaches the
// terminal through it, so an escape sequence can't rewrite what the
// principal sees.
func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if (r < 0x20 && r != '\t') || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return '?'
		}
		return r
	}, s)
}

// sinceText is how long ago t was, like 2h10m.
func sinceText(t time.Time) string {
	d := missionNow().Sub(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
