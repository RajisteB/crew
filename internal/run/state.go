// Package run is the factory engine: a pure state machine (machine.go), the
// run folder it persists to (store.go), and the pieces that run one station
// (prompt, verdict, snapshot, runner). Both `crew run` and the foreman drive
// factories through it, so routing, limits, guards and approvals live here
// and nowhere else.
package run

type Status string

const (
	StatusRunning          Status = "running"
	StatusAwaitingApproval Status = "awaiting-approval"
	StatusInterrupted      Status = "interrupted"
	StatusDone             Status = "done"
	StatusFailed           Status = "failed"
)

// Verdict is what a station returns at the end of its reply.
type Verdict struct {
	Route   string   `json:"route"`
	Summary string   `json:"summary"`
	Notes   []string `json:"notes,omitempty"`
}

// Pending is a finished station waiting for the user's approval.
type Pending struct {
	Station string  `json:"station"`
	Route   string  `json:"route"`
	Verdict Verdict `json:"verdict"`
}

// State is everything needed to continue a run. Functions in this package
// never modify a State they are given; they return a new one.
type State struct {
	RunID         string            `json:"run_id"`
	Factory       string            `json:"factory"`
	Status        Status            `json:"status"`
	Current       string            `json:"current"`
	Step          int               `json:"step"`
	RouteCounts   map[string]int    `json:"route_counts"`
	Snapshot      map[string]string `json:"snapshot,omitempty"`
	Pending       *Pending          `json:"pending,omitempty"`
	Reason        string            `json:"reason,omitempty"`
	SkipDriftOnce bool              `json:"skip_drift_once,omitempty"`
	Retryable     bool              `json:"retryable,omitempty"`
	LimitRoute    string            `json:"limit_route,omitempty"` // "station.route" whose limit ended the run
	InStation     bool              `json:"in_station,omitempty"`  // a station was running when this was saved
}

// Terminal reports whether nothing more will run without a resume.
func (s State) Terminal() bool { return s.Status == StatusDone || s.Status == StatusFailed }

type DecisionKind string

const (
	RunStation    DecisionKind = "run-station"
	AwaitApproval DecisionKind = "await-approval"
	Finished      DecisionKind = "finished"
	Failed        DecisionKind = "failed"
)

// Decision is what the engine should do next.
type Decision struct {
	Kind    DecisionKind
	Station string
	Reason  string
}

func cloneState(s State) State {
	c := s
	c.RouteCounts = make(map[string]int, len(s.RouteCounts))
	for k, v := range s.RouteCounts {
		c.RouteCounts[k] = v
	}
	if s.Snapshot != nil {
		c.Snapshot = make(map[string]string, len(s.Snapshot))
		for k, v := range s.Snapshot {
			c.Snapshot[k] = v
		}
	}
	if s.Pending != nil {
		p := *s.Pending
		p.Verdict.Notes = append([]string(nil), s.Pending.Verdict.Notes...)
		c.Pending = &p
	}
	return c
}

// WithSnapshot returns s with its end-of-step snapshot replaced.
func WithSnapshot(s State, snap map[string]string) State {
	c := cloneState(s)
	c.Snapshot = make(map[string]string, len(snap))
	for k, v := range snap {
		c.Snapshot[k] = v
	}
	return c
}
