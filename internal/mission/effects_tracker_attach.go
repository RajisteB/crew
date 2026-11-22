package mission

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	maxAttachBytes = 10 << 20
	maxAttachTries = 3 // failed uploads of one file before it is given up
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
// permanent. It never ends the pass: an upload timing out says nothing about
// the rest of Jira. The file is given up on its maxAttachTries-th failure.
func (p *pass) attachFailed(op TrackerOp) (result, error) {
	t := p.t
	if t.e.attachTries == nil {
		t.e.attachTries = map[string]int{}
	}
	k := attachKey(op)
	t.e.attachTries[k]++
	if t.e.attachTries[k] < maxAttachTries {
		return carryOn, nil
	}
	t.note(op.Piece, "Jira: %s could not be attached after %d tries and stays in the mission folder", op.File, maxAttachTries)
	return p.succeeded(op)
}

// forgetAttachTries drops the count of failed uploads of op's file.
func (e *Engine) forgetAttachTries(op TrackerOp) {
	delete(e.attachTries, attachKey(op))
}

func attachKey(op TrackerOp) string { return op.Piece + "\x00" + op.File }
