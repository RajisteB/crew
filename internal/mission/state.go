package mission

import (
	"maps"
	"slices"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

// Status is where the whole mission stands.
type Status string

const (
	StatusPlanned    Status = "planned"
	StatusAwaitingGo Status = "awaiting-go"
	StatusRunning    Status = "running"
	StatusStopped    Status = "stopped"
	StatusDone       Status = "done"
)

// PieceStatus is where one piece stands.
type PieceStatus string

const (
	PieceQueued    PieceStatus = "queued"
	PieceRunning   PieceStatus = "running"
	PiecePROpen    PieceStatus = "pr-open"
	PieceWaiting   PieceStatus = "waiting"
	PieceComplete  PieceStatus = "complete"
	PieceCancelled PieceStatus = "cancelled"
	// PieceDelivered: the run is done and any pull request is merged; its
	// deliverable waits to be accepted.
	PieceDelivered PieceStatus = "delivered"
)

// Wait kinds: what a waiting piece is waiting for.
const (
	WaitMerge       = "merge"
	WaitApproval    = "approval"
	WaitQuestion    = "question"
	WaitLimit       = "limit"
	WaitPRClosed    = "pr-closed"
	WaitDeliverable = "deliverable"
)

// Wait is something a piece is blocked on.
type Wait struct {
	ID       string      `json:"id"`
	Kind     string      `json:"kind"`
	Question string      `json:"question,omitempty"`
	Since    time.Time   `json:"since"`
	Reminded int         `json:"reminded,omitempty"`
	Back     PieceStatus `json:"back,omitempty"` // status to return to once answered
}

// Decision is one recorded choice about a piece. By is "engine",
// "orchestrator" or "principal".
type Decision struct {
	Time    time.Time `json:"time"`
	By      string    `json:"by"`
	Failure string    `json:"failure,omitempty"`
	Action  string    `json:"action,omitempty"`
	Station string    `json:"station,omitempty"`
	Note    string    `json:"note,omitempty"`
	Reason  string    `json:"reason,omitempty"`
}

// Deliverable is the work a piece produced that isn't a change to the repo.
type Deliverable struct {
	Files    []string  `json:"files"` // paths relative to the piece's deliverables folder
	Print    string    `json:"print"` // fingerprint of the files as collected
	Posted   time.Time `json:"posted"`
	Accepted bool      `json:"accepted,omitempty"`
	By       string    `json:"by,omitempty"`       // who accepted: "engine" or "principal"
	Attached []string  `json:"attached,omitempty"` // the Files crew is done with on the Jira issue
}

// Tracked is what Jira already shows for a piece.
type Tracked struct {
	Issue   string `json:"issue,omitempty"`
	Lane    string `json:"lane,omitempty"`
	Flagged bool   `json:"flagged,omitempty"`
	Sprint  bool   `json:"sprint,omitempty"`
	// Unconfirmed: Lane was recorded when Jira refused the move for good, so
	// the board may show another lane.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
}

// TrackerState is what Jira already shows for the mission.
type TrackerState struct {
	Epic     string `json:"epic,omitempty"`
	EpicLane string `json:"epic_lane,omitempty"`
	Seq      int    `json:"seq,omitempty"` // events up to this one have had their comment sent
}

// PieceState is one piece's progress.
type PieceState struct {
	ID          string         `json:"id"`
	Status      PieceStatus    `json:"status"`
	RunID       string         `json:"run_id,omitempty"`
	Branch      string         `json:"branch,omitempty"`
	Worktree    string         `json:"worktree,omitempty"`
	PR          int            `json:"pr,omitempty"`
	PRURL       string         `json:"pr_url,omitempty"`
	Resumes     int            `json:"resumes,omitempty"`
	Calls       int            `json:"calls,omitempty"`
	SentBack    map[string]int `json:"sent_back,omitempty"` // reason -> times
	Decisions   []Decision     `json:"decisions,omitempty"`
	Wait        *Wait          `json:"wait,omitempty"`
	Added       bool           `json:"added,omitempty"`
	Head        string         `json:"head,omitempty"`        // the commit crew pushed to the pull request
	PRSince     time.Time      `json:"pr_since,omitempty"`    // when crew last pushed that head
	GreenSince  time.Time      `json:"green_since,omitempty"` // when crew first saw this head with its checks all green
	Deliverable *Deliverable   `json:"deliverable,omitempty"`
	Jira        Tracked        `json:"jira"`
	Problem     string         `json:"problem,omitempty"` // why the piece was last sent back from its pull request: names only, at most 300 runes
	// Behind is the other mission's piece this queued piece is kept behind.
	Behind *Blocker `json:"behind,omitempty"`
}

// State is state.json. It holds ids, counts and short messages only: never
// file contents or secrets.
type State struct {
	MissionID string       `json:"mission_id"`
	Status    Status       `json:"status"`
	Repo      string       `json:"repo"` // owner/name
	Base      string       `json:"base"` // default branch
	Pieces    []PieceState `json:"pieces"`
	NextWait  int          `json:"next_wait"`
	Additions int          `json:"additions,omitempty"`
	Jira      TrackerState `json:"jira"`
}

// NewState is a planned mission with every piece queued, in file order.
func NewState(id string, m Mission, info gh.RepoInfo) State {
	pieces := make([]PieceState, len(m.Pieces))
	for i, p := range m.Pieces {
		pieces[i] = PieceState{ID: p.ID, Status: PieceQueued}
	}
	return State{MissionID: id, Status: StatusPlanned, Repo: info.Slug, Base: info.DefaultBranch, Pieces: pieces, NextWait: 1}
}

// Piece returns a copy of the piece with this id.
func (s State) Piece(id string) (PieceState, bool) {
	for _, p := range s.Pieces {
		if p.ID == id {
			return p.clone(), true
		}
	}
	return PieceState{}, false
}

// Clone is a deep copy: changing it never changes s.
func (s State) Clone() State {
	c := s
	c.Pieces = make([]PieceState, len(s.Pieces))
	for i, p := range s.Pieces {
		c.Pieces[i] = p.clone()
	}
	return c
}

func (p PieceState) clone() PieceState {
	p.SentBack = maps.Clone(p.SentBack)
	p.Decisions = slices.Clone(p.Decisions)
	if p.Wait != nil {
		w := *p.Wait
		p.Wait = &w
	}
	if p.Behind != nil {
		b := *p.Behind
		p.Behind = &b
	}
	if p.Deliverable != nil {
		d := *p.Deliverable
		d.Files = slices.Clone(d.Files)
		d.Attached = slices.Clone(d.Attached)
		p.Deliverable = &d
	}
	return p
}
