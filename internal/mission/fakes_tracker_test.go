package mission

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

var errFakePermanent = errors.New("permanent")

// fakeTracker is a Tracker that records calls in the shared callLog. Both
// failures and permanent are keyed by the op name ("create-epic", "create",
// "move", "comment", "flag", "attach", "sprint", "done") or by the more
// specific "<op>:<label or key>".
type fakeTracker struct {
	log       *callLog
	fail      *failures
	permanent map[string]bool
	noSprint  bool
	doneKeys  map[string]bool
	comments  []string // the text of every comment sent
	next      int
}

func (f *fakeTracker) hit(op, id string) error {
	for _, k := range []string{op + ":" + id, op} {
		if f.permanent[k] {
			return fmt.Errorf("%s failed: %w", k, errFakePermanent)
		}
		if err := f.fail.hit(k); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeTracker) Create(_ context.Context, kind, label, _, _, _ string) (string, error) {
	f.log.add("create:%s:%s", kind, label)
	op := "create"
	if kind == "epic" {
		op = "create-epic"
	}
	if err := f.hit(op, label); err != nil {
		return "", err
	}
	f.next++
	return fmt.Sprintf("J-%d", f.next), nil
}

func (f *fakeTracker) Move(_ context.Context, key, lane string) error {
	f.log.add("move:%s:%s", key, lane)
	return f.hit("move", key)
}

func (f *fakeTracker) Comment(_ context.Context, key, text string) error {
	f.log.add("comment:%s", key)
	if err := f.hit("comment", key); err != nil {
		return err
	}
	f.comments = append(f.comments, text)
	return nil
}

func (f *fakeTracker) Flag(_ context.Context, key string, on bool) error {
	f.log.add("flag:%s:%v", key, on)
	return f.hit("flag", key)
}

func (f *fakeTracker) Attach(_ context.Context, key, path string) error {
	f.log.add("attach:%s:%s", key, filepath.Base(path))
	return f.hit("attach", key)
}

func (f *fakeTracker) Done(_ context.Context, key string) (bool, error) {
	f.log.add("done:%s", key)
	if err := f.hit("done", key); err != nil {
		return false, err
	}
	return f.doneKeys[key], nil
}

func (f *fakeTracker) Sprint(_ context.Context, keys []string) (bool, error) {
	f.log.add("sprint:%d", len(keys))
	if err := f.hit("sprint", ""); err != nil {
		return false, err
	}
	return !f.noSprint, nil
}

// withTracker gives the engine a fake Jira and returns it.
func (h *harness) withTracker() *fakeTracker {
	ft := &fakeTracker{log: h.log, fail: h.fail, permanent: map[string]bool{}, doneKeys: map[string]bool{}}
	h.e.Tracker = ft
	h.e.TrackerPermanent = func(err error) bool { return errors.Is(err, errFakePermanent) }
	return ft
}
