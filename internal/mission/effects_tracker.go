package mission

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	maxAttachBytes   = 10 << 20
	trackerDrainRuns = 10 // extra sync passes after a mission ends while writes are still owed
)

// SyncTracker sends the writes Jira is owed, once, and returns how many are
// still owed. The caller holds the mission lock. It never returns a Jira
// failure as its error: a failure is a note and the write stays owed.
func (e *Engine) SyncTracker(ctx context.Context) (owed int, err error) {
	if e.Tracker == nil {
		return 0, nil
	}
	t, err := e.newTick(ctx)
	if err != nil {
		return 0, err
	}
	owed, err = t.syncTracker()
	e.lastNotes = t.notes
	return owed, err
}

// drainTracker catches Jira up after the mission ended: up to
// trackerDrainRuns passes, one per every, then a note when writes remain.
func (e *Engine) drainTracker(ctx context.Context, every time.Duration) {
	if e.Tracker == nil {
		return
	}
	owed := 0
	for i := range trackerDrainRuns {
		var err error
		if owed, err = e.SyncTracker(ctx); err != nil {
			fmt.Fprintf(e.out(), "mission: can't sync Jira: %v\n", err)
			return
		}
		if owed == 0 || i == trackerDrainRuns-1 {
			break
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
	if owed > 0 {
		e.emit(Event{Kind: EvNote, Msg: fmt.Sprintf(
			"Jira is behind: %d writes were not sent; run crew jira sync %s", owed, filepath.Base(e.Store.Dir))})
	}
}

// pass is one run of the planned writes.
type pass struct {
	t        *tick
	dirty    bool            // the state changed and isn't saved yet
	comments bool            // every comment op so far was carried out
	failed   map[string]bool // pieces whose create failed for good this pass
}

// syncTracker plans the writes owed from t's state and sends them. It
// returns how many are still owed. Its error is only a failure to save.
func (t *tick) syncTracker() (int, error) {
	p := &pass{t: t, comments: true, failed: map[string]bool{}}
	err := p.run()
	if err == nil && p.dirty {
		err = t.save()
	}
	return Owed(t.plan(t.eventsAfterCursor())), err
}

func (t *tick) eventsAfterCursor() []Event {
	evs, err := t.e.Store.EventsSince(t.s.Jira.Seq)
	if err != nil {
		t.note("", "Jira: can't read the events: %v", err)
	}
	return evs
}

func (t *tick) plan(evs []Event) []TrackerOp {
	return TrackerOps(t.m, t.s, evs, t.e.Sprints)
}

func (p *pass) run() error {
	t := p.t
	evs := t.eventsAfterCursor()
	last := t.s.Jira.Seq
	if n := len(evs); n > 0 {
		last = evs[n-1].Seq
	}
	ops := t.plan(evs)
	for i := 0; i < len(ops); i++ {
		if t.ctx.Err() != nil {
			return nil
		}
		op := ops[i]
		if op.Kind == "comment" && !p.comments {
			continue
		}
		res, err := p.send(op)
		if err != nil {
			return err
		}
		switch res {
		case stopPass:
			return nil
		case replan:
			ops, i = p.replanned(evs), -1
		}
	}
	if p.comments && t.s.Jira.Epic != "" && last > t.s.Jira.Seq {
		t.s = CommentedThrough(t.s, last)
		p.dirty = true
	}
	return nil
}

// replanned plans again once issues exist, leaving out the creates that
// failed for good so they are tried again next tick, not in a loop.
func (p *pass) replanned(evs []Event) []TrackerOp {
	var ops []TrackerOp
	for _, op := range p.t.plan(evs) {
		if op.Kind != "create" || !p.failed[op.Piece] {
			ops = append(ops, op)
		}
	}
	return ops
}

type result int

const (
	carryOn result = iota
	stopPass
	replan
)

// send carries out one op. Its error is a failure to save the state.
func (p *pass) send(op TrackerOp) (result, error) {
	t := p.t
	key := t.issueKey(op.Piece)
	if key == "" && op.Kind != "create-epic" && op.Kind != "create" && op.Kind != "sprint" {
		if op.Kind == "comment" {
			p.comments = false
		}
		return carryOn, nil
	}
	cerr := p.call(op, key)
	if cerr == nil {
		return p.succeeded(op)
	}
	if t.ctx.Err() != nil {
		return stopPass, nil
	}
	t.note(op.Piece, "Jira: can't %s: %v", p.what(op, key), cerr)
	if !t.permanent(cerr) {
		return stopPass, nil
	}
	if op.Kind == "create" {
		p.failed[op.Piece] = true
	}
	if op.Kind == "create" || op.Kind == "create-epic" || op.Kind == "sprint" {
		return carryOn, nil
	}
	return p.succeeded(op)
}

// what is the failed op in words: "move J-2", "create the epic".
func (p *pass) what(op TrackerOp, key string) string {
	if op.Kind == "sprint" {
		return "place the issues in a sprint"
	}
	return opVerb(op.Kind) + " " + p.target(op, key)
}

func (p *pass) target(op TrackerOp, key string) string {
	switch {
	case key != "":
		return key
	case op.Piece != "":
		return op.Piece
	}
	return "the epic"
}

func (t *tick) permanent(err error) bool {
	var g *givenUp
	if errors.As(err, &g) {
		return true
	}
	return t.e.TrackerPermanent != nil && t.e.TrackerPermanent(err)
}

// issueKey is the key of a piece's issue, or of the epic for "".
func (t *tick) issueKey(piece string) string {
	if piece == "" {
		return t.s.Jira.Epic
	}
	ps, _ := t.s.Piece(piece)
	return ps.Jira.Issue
}

func opVerb(kind string) string {
	switch kind {
	case "create-epic", "create":
		return "create"
	case "move-epic":
		return "move"
	case "comment":
		return "comment on"
	}
	return kind
}

// call makes the tracker call for op. A call that creates an issue records
// it in the state at once and saves.
func (p *pass) call(op TrackerOp, key string) error {
	t, tr, id := p.t, p.t.e.Tracker, p.t.s.MissionID
	switch op.Kind {
	case "create-epic":
		sum, body := EpicText(id, t.m)
		k, err := tr.Create(t.ctx, "epic", TrackerLabel(id, ""), sum, body, "")
		if err == nil && k == "" {
			err = errNoKey
		}
		if err == nil {
			t.s = EpicCreated(t.s, k)
		}
		return err
	case "create":
		return p.createPiece(op.Piece)
	case "sprint":
		return p.sprint(op)
	case "move", "move-epic":
		return tr.Move(t.ctx, key, op.Lane)
	case "flag":
		return tr.Flag(t.ctx, key, op.On)
	case "attach":
		return p.attach(op, key)
	case "comment":
		return tr.Comment(t.ctx, key, op.Text)
	}
	return fmt.Errorf("unknown tracker op %q", op.Kind)
}

func (p *pass) createPiece(piece string) error {
	t := p.t
	pc, ok := t.m.Piece(piece)
	if !ok {
		return fmt.Errorf("the mission has no piece %s", piece)
	}
	sum, body := IssueText(t.s.MissionID, t.m, pc)
	k, err := t.e.Tracker.Create(t.ctx, "piece", TrackerLabel(t.s.MissionID, piece), sum, body, t.s.Jira.Epic)
	if err == nil && k == "" {
		err = errNoKey
	}
	if err == nil {
		t.s = IssueCreated(t.s, piece, k)
	}
	return err
}

func (p *pass) sprint(op TrackerOp) error {
	t := p.t
	var keys []string
	for _, id := range op.Pieces {
		if k := t.issueKey(id); k != "" {
			keys = append(keys, k)
		}
	}
	placed, err := t.e.Tracker.Sprint(t.ctx, keys)
	if err != nil {
		return err
	}
	if placed {
		t.s = SprintSet(t.s, op.Pieces)
		p.dirty = true
		return nil
	}
	t.note("", "Jira: the board has no active sprint; %d issues are in the backlog and won't show on the board until a sprint starts", len(keys))
	return nil
}

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

var errNoKey = errors.New("the tracker returned no issue key")

// givenUp is an error that can never succeed, whatever the tracker says.
type givenUp struct{ error }

// succeeded records a finished op in the state. Creations are saved at once.
func (p *pass) succeeded(op TrackerOp) (result, error) {
	t := p.t
	switch op.Kind {
	case "move", "move-epic":
		t.s = LaneSet(t.s, op.Piece, op.Lane)
	case "flag":
		t.s = FlagSet(t.s, op.Piece, op.On)
	case "attach":
		t.s = FileAttached(t.s, op.Piece, op.File)
	case "comment":
		t.s = CommentedThrough(t.s, op.Seq)
	case "create-epic", "create":
		p.dirty = false
		if err := t.save(); err != nil {
			return stopPass, err
		}
		return replan, nil
	}
	p.dirty = true
	return carryOn, nil
}
