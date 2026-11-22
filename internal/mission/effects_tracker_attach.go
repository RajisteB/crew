package mission

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxAttachBytes = 10 << 20
	maxAttachTries = 3 // failed uploads of one file before this engine stops trying it
)

// attach sends one deliverable file. A file that is missing, isn't a regular
// file, or is too big is given up on with a note, so it isn't tried forever.
func (p *pass) attach(op TrackerOp, key string) error {
	t := p.t
	if !filepath.IsLocal(op.File) {
		return &givenUp{fmt.Errorf("%s is not a file in the deliverables folder", op.File)}
	}
	path := filepath.Join(t.e.Store.DeliverablesDir(op.Piece), op.File)
	fi, err := os.Lstat(path)
	switch {
	case err != nil:
		return &givenUp{err}
	case !fi.Mode().IsRegular():
		return &givenUp{fmt.Errorf("%s is not a regular file", op.File)}
	case fi.Size() > maxAttachBytes:
		t.note(op.Piece, "Jira: %s is over 10 MB and stays in the mission folder", op.File)
		return nil
	}
	return t.e.Tracker.Attach(t.ctx, key, path)
}

// attachFailed handles an upload that failed with an error that is not
// permanent. A down error other than the upload's own deadline ends the pass
// and costs no try: an outage never uses tries up. Any other failure is a
// try, and the pass goes on. After maxAttachTries this engine stops trying
// the file, but it stays owed, so crew jira sync (a new engine) tries again.
func (p *pass) attachFailed(op TrackerOp, err error) (result, error) {
	t := p.t
	if t.down(err) && !errors.Is(err, context.DeadlineExceeded) {
		return stopPass, nil
	}
	if t.e.attachTries == nil {
		t.e.attachTries = map[string]int{}
	}
	k := attachKey(op)
	t.e.attachTries[k]++
	if t.e.attachTries[k] == maxAttachTries {
		t.note(op.Piece, "Jira: %s could not be attached after %d tries; crew jira sync %s tries again",
			op.File, maxAttachTries, t.s.MissionID)
	}
	return carryOn, nil
}

// stoppedTrying reports whether this engine has used up op's tries.
func (e *Engine) stoppedTrying(op TrackerOp) bool {
	return e.attachTries[attachKey(op)] >= maxAttachTries
}

// forgetAttachTries drops the count of failed uploads of op's file.
func (e *Engine) forgetAttachTries(op TrackerOp) {
	delete(e.attachTries, attachKey(op))
}

func attachKey(op TrackerOp) string { return op.Piece + "\x00" + op.File }
