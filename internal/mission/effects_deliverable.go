package mission

import (
	"errors"
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
	f, err := t.listScratch(a.Piece, ps.Worktree)
	if err == nil && len(f.Files) == 0 {
		return nil // taken back since it was observed; the next tick sees that
	}
	if err == nil {
		err = swapInto(t.e.Store.DeliverablesDir(a.Piece), scratchPath(ps.Worktree, scratchOut), f.Files)
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

// listScratch lists the worktree's deliverables folder. When .crew or the
// folder is a link or a file it notes that and reports nothing delivered.
func (t *tick) listScratch(piece, worktree string) (Found, error) {
	_, err := plainDir(worktree, run.CrewDir, scratchOut)
	var np *notPlainError
	if errors.As(err, &np) {
		t.note(piece, "the deliverables folder is not a plain folder and was ignored: %s", np.Path)
		return Found{}, nil
	}
	if err != nil {
		return Found{}, err
	}
	return listDelivered(scratchPath(worktree, scratchOut))
}

// swapInto copies the files into a temporary sibling of dest, then replaces
// dest with it. A failure leaves dest as it was.
func swapInto(dest, from string, files []string) error {
	tmp := filepath.Join(filepath.Dir(dest), "."+filepath.Base(dest)+".tmp")
	if err := os.RemoveAll(tmp); err != nil {
		return err
	}
	if err := copyFiles(from, tmp, files); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.RemoveAll(dest); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.RemoveAll(tmp)
		return err
	}
	return nil
}

// prepareScratch makes the worktree's deliverables folder and copies in the
// deliverables of the complete pieces this one receives (see InputsFor). It
// returns them for the input, so the prompt names only what was copied.
func (t *tick) prepareScratch(p Piece, worktree string) ([]Earlier, error) {
	if _, err := plainDir(worktree, run.CrewDir); err != nil {
		return nil, err // before EnsureCrewDir, which would write through a link
	}
	if err := run.EnsureCrewDir(worktree); err != nil {
		return nil, err
	}
	if _, err := plainDir(worktree, run.CrewDir, scratchIn); err != nil {
		return nil, err
	}
	if _, err := plainDir(worktree, run.CrewDir, scratchOut); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(scratchPath(worktree, scratchOut), dirMode); err != nil {
		return nil, err
	}
	var earlier []Earlier
	for _, dep := range InputsFor(t.m, p.ID) {
		ds, ok := t.s.Piece(dep)
		if !ok || ds.Status != PieceComplete || ds.Deliverable == nil {
			continue // a cancelled piece's deliverable is never handed on
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
