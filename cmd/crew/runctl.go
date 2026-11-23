package main

import (
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/run"
)

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
	text, err := textArg("say", msg, f.stdin)
	if err != nil {
		return err
	}
	if err := store.AppendInbox(text); err != nil {
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
	if f.note != "" {
		note, err := textArg("--note", f.note, f.stdin)
		if err != nil {
			return err
		}
		if err := store.AppendInbox(note); err != nil {
			return err
		}
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
