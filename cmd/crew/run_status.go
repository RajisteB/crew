package main

import (
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/run"
)

// runStatus lists runs, or prints one run's state and the events after
// --since, ending with "cursor N" for the next poll.
func runStatus(pos []string, f runFlags, out io.Writer) error {
	if len(pos) == 0 {
		if f.wait != 0 {
			return usageError{"--wait needs a run id"}
		}
		return listRuns(f.project, out)
	}
	if f.wait < 0 || f.wait > maxStatusWait {
		return usageError{"--wait takes a time up to 10m, like 90s"}
	}
	store, err := run.OpenRun(f.project, pos[0])
	if err != nil {
		return err
	}
	if err := waitForNews(store, f.since, f.wait); err != nil {
		return err
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	printRunHeader(out, f.project, store, st)
	evs, err := store.EventsSince(f.since)
	if err != nil {
		return err
	}
	cursor := f.since
	for _, ev := range evs {
		fmt.Fprintln(out, run.FormatEvent(ev))
		cursor = ev.Seq
	}
	fmt.Fprintf(out, "cursor %d\n", cursor)
	return nil
}

// listRuns prints one line per run, newest first, ending with the run's
// owner when it has one. The repo's mission states are read once; when one
// can't be read, a last line says the owner of an unstamped run is unknown.
func listRuns(project string, out io.Writer) error {
	runs, err := run.ListRuns(project)
	if err != nil {
		return err
	}
	if len(runs) == 0 {
		fmt.Fprintf(out, "no runs in %s\n", project)
		return nil
	}
	owners, ownersErr := mission.RunOwners(project)
	for _, st := range runs {
		owner := st.Owner
		if owner == "" {
			owner = owners[st.RunID]
		}
		if owner != "" {
			owner = "  " + owner
		}
		fmt.Fprintf(out, "%-44s %-18s %-10s step %d%s\n", st.RunID, st.Status, st.Current, st.Step, owner)
	}
	if ownersErr != nil {
		fmt.Fprintf(out, "owner: unknown for a run with no owner in its state (%v)\n", ownersErr)
	}
	return nil
}

// printRunHeader prints the lines of `crew run status <id>` before the
// events: the run, its owner, why it stopped, what to do next and any
// approval it waits for.
func printRunHeader(out io.Writer, project string, store run.Store, st run.State) {
	live := ""
	if pid, ok := store.LockedBy(); ok {
		live = fmt.Sprintf("  (live, pid %d)", pid)
	}
	fmt.Fprintf(out, "run %s  factory %s  status %s  current %s  step %d%s\n", st.RunID, st.Factory, st.Status, st.Current, st.Step, live)
	owner, ownerErr := ownerOf(project, st)
	switch {
	case ownerErr != nil:
		fmt.Fprintf(out, "owner: unknown (%v)\n", ownerErr)
	case owner != "":
		fmt.Fprintf(out, "owner: %s\n", owner)
		fmt.Fprintln(out, "driven by its mission: use crew mission status")
	}
	if st.Reason != "" {
		fmt.Fprintf(out, "reason: %s\n", st.Reason)
	}
	if owner == "" && ownerErr == nil {
		printResumeHint(out, st)
	}
	if p := st.Pending; p != nil {
		fmt.Fprintf(out, "waiting for approval: %s chose %s — %s (output: %s)\n", p.Station, p.Route, p.Verdict.Summary, lastOutput(store))
	}
}

// printResumeHint says how to continue a plain run that failed and can be
// retried, or was interrupted.
func printResumeHint(out io.Writer, st run.State) {
	if st.Status == run.StatusFailed && st.Retryable {
		fmt.Fprintf(out, "retryable: fix the cause, then crew run resume %s\n", st.RunID)
	} else if st.Status == run.StatusInterrupted {
		fmt.Fprintf(out, "resumable: crew run resume %s\n", st.RunID)
	}
}
