package main

import (
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/run"
)

// runStatus lists runs, or prints one run's state and the events after
// --since, ending with "cursor N" for the foreman's next poll.
func runStatus(pos []string, f runFlags, out io.Writer) error {
	if len(pos) == 0 {
		runs, err := run.ListRuns(f.project)
		if err != nil {
			return err
		}
		if len(runs) == 0 {
			fmt.Fprintf(out, "no runs in %s\n", f.project)
		}
		for _, st := range runs {
			fmt.Fprintf(out, "%-44s %-18s %-10s step %d\n", st.RunID, st.Status, st.Current, st.Step)
		}
		return nil
	}
	store, err := run.OpenRun(f.project, pos[0])
	if err != nil {
		return err
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	live := ""
	if pid, ok := store.LockedBy(); ok {
		live = fmt.Sprintf("  (live, pid %d)", pid)
	}
	fmt.Fprintf(out, "run %s  factory %s  status %s  current %s  step %d%s\n", st.RunID, st.Factory, st.Status, st.Current, st.Step, live)
	if st.Reason != "" {
		fmt.Fprintf(out, "reason: %s\n", st.Reason)
	}
	if p := st.Pending; p != nil {
		fmt.Fprintf(out, "waiting for approval: %s chose %s — %s (output: %s)\n", p.Station, p.Route, p.Verdict.Summary, lastOutput(store))
	}
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

func lastOutput(store run.Store) string {
	steps, err := store.Steps()
	if err != nil || len(steps) == 0 {
		return "-"
	}
	return steps[len(steps)-1].Dir + "/output.md"
}

func runSay(id, msg string, f runFlags, out io.Writer) error {
	store, err := run.OpenRun(f.project, id)
	if err != nil {
		return err
	}
	if err := store.AppendInbox(msg); err != nil {
		return err
	}
	fmt.Fprintln(out, "queued; the next station to start will receive it")
	return nil
}

func runGoto(id, station string, f runFlags, out io.Writer) error {
	store, err := run.OpenRun(f.project, id)
	if err != nil {
		return err
	}
	fac, err := store.Factory()
	if err != nil {
		return err
	}
	if _, ok := fac.Stations[station]; !ok {
		return fmt.Errorf("no station %q in factory %s", station, fac.Name)
	}
	if _, live := store.LockedBy(); live {
		if err := store.RequestGoto(station); err != nil {
			return err
		}
		fmt.Fprintf(out, "requested: the run goes to %s before its next station\n", station)
		return nil
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	if st, err = run.Goto(fac, st, station); err != nil {
		return err
	}
	if err := store.SaveState(st); err != nil {
		return err
	}
	fmt.Fprintf(out, "run will continue at %s: crew run resume %s\n", station, id)
	return nil
}

func runApprove(id string, f runFlags, out io.Writer) error {
	store, err := run.OpenRun(f.project, id)
	if err != nil {
		return err
	}
	fac, err := store.Factory()
	if err != nil {
		return err
	}
	st, err := store.LoadState()
	if err != nil {
		return err
	}
	st, _, err = run.Approve(fac, st, f.gotoStation)
	if err != nil {
		return err
	}
	if err := store.SaveState(st); err != nil {
		return err
	}
	return continueRun(id, f, out)
}

func continueRun(id string, f runFlags, out io.Writer) error {
	store, err := run.OpenRun(f.project, id)
	if err != nil {
		return err
	}
	dir, err := resolveDir(f.dir, false)
	if err != nil {
		return err
	}
	return driveOrDetach(store, id, dir, f, out)
}
