package run

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"syscall"
	"time"
)

// Event kinds.
const (
	EvStart       = "start"
	EvStepStart   = "step-start"
	EvStepEnd     = "step-end"
	EvRetry       = "retry"
	EvRoute       = "route"
	EvNote        = "note"
	EvDrift       = "drift"
	EvApproval    = "approval"
	EvInterrupted = "interrupted"
	EvDone        = "done"
	EvFailed      = "failed"
	EvMessage     = "message"
	EvGoto        = "goto"
)

// Event is one line of <run>/events.jsonl. The engine and the CLI append to
// it; `crew run status --since N` and the foreman read it.
type Event struct {
	Seq     int       `json:"seq"`
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Station string    `json:"station,omitempty"`
	Route   string    `json:"route,omitempty"`
	Msg     string    `json:"msg,omitempty"`
	Count   int       `json:"count,omitempty"`
	Max     string    `json:"max,omitempty"`
}

// Emit appends e with the next sequence number. An flock on events.lock keeps
// sequence numbers unique across processes (engine, `crew run say`).
func (s Store) Emit(e Event) (Event, error) {
	lf, err := os.OpenFile(s.path("events.lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return e, err
	}
	defer lf.Close()
	if err := syscall.Flock(int(lf.Fd()), syscall.LOCK_EX); err != nil {
		return e, err
	}
	defer syscall.Flock(int(lf.Fd()), syscall.LOCK_UN)

	all, err := s.EventsSince(0)
	if err != nil {
		return e, err
	}
	e.Seq = len(all) + 1
	if e.Time.IsZero() {
		e.Time = time.Now()
	}
	b, err := json.Marshal(e)
	if err != nil {
		return e, err
	}
	f, err := os.OpenFile(s.path("events.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return e, err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return e, err
	}
	return e, f.Close()
}

// EventsSince returns events with Seq > seq, in order.
func (s Store) EventsSince(seq int) ([]Event, error) {
	f, err := os.Open(s.path("events.jsonl"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			continue
		}
		if e.Seq > seq {
			out = append(out, e)
		}
	}
	return out, sc.Err()
}
