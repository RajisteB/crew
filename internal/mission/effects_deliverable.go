package mission

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/crewfile-dev/crew/internal/run"
)

const maxDeliverableNote = "%d more files in .crew/deliverables were left out: a deliverable is at most %d files"

// scratchPath is a folder under the worktree's .crew.
func scratchPath(worktree string, parts ...string) string {
	return filepath.Join(append([]string{worktree, run.CrewDir}, parts...)...)
}

// collectDeliverable copies what the station left into the mission folder and
// records it. A failure is a note and changes nothing.
func (t *tick) collectDeliverable(a CollectDeliverable) error {
	_, ps, ok := t.pieceMission(a.Piece)
	if !ok || ps.Worktree == "" {
		return nil
	}
	f, err := listDelivered(scratchPath(ps.Worktree, scratchOut))
	if err == nil && len(f.Files) == 0 {
		return nil // taken back since it was observed; the next tick sees that
	}
	dest := t.e.Store.DeliverablesDir(a.Piece)
	if err == nil {
		if err = os.RemoveAll(dest); err == nil {
			err = copyFiles(scratchPath(ps.Worktree, scratchOut), dest, f.Files)
		}
	}
	if err != nil {
		t.note(a.Piece, "can't collect the deliverable: %v", err)
		return nil
	}
	t.s = DeliverableCollected(t.s, a.Piece, f.Files, f.Print, t.now)
	if err := t.save(); err != nil {
		return err
	}
	t.emit(Event{Kind: EvDeliverablePosted, Piece: a.Piece, Run: ps.RunID, Msg: fileList(f.Files)})
	if f.Extra > 0 {
		t.note(a.Piece, maxDeliverableNote, f.Extra, maxDeliverableFiles)
	}
	return nil
}

// prepareScratch makes the worktree's deliverables folder and copies in the
// deliverables of the pieces this one waits for. It returns them for the input.
func (t *tick) prepareScratch(p Piece, worktree string) ([]Earlier, error) {
	if err := run.EnsureCrewDir(worktree); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(scratchPath(worktree, scratchOut), dirMode); err != nil {
		return nil, err
	}
	var earlier []Earlier
	for _, dep := range p.WaitsFor {
		ds, ok := t.s.Piece(dep)
		if !ok || ds.Deliverable == nil {
			continue
		}
		into := scratchPath(worktree, scratchIn, dep)
		if err := os.RemoveAll(into); err != nil {
			return nil, err
		}
		if err := copyFiles(t.e.Store.DeliverablesDir(dep), into, ds.Deliverable.Files); err != nil {
			return nil, fmt.Errorf("%s: %w", dep, err)
		}
		earlier = append(earlier, Earlier{Piece: dep, Files: ds.Deliverable.Files})
	}
	return earlier, nil
}
