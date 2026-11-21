package mission

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// ---------- inbox ----------

// applyInbox applies each waiting item and returns those to acknowledge once
// the state is saved. An item that can't be read or applied is a note and is
// still acknowledged; one whose run step failed stays for the next tick.
func (t *tick) applyInbox() ([]Item, error) {
	items, err := t.e.Store.Inbox()
	if err != nil {
		return nil, err
	}
	var acked []Item
	for _, it := range items {
		done, err := t.applyItem(it)
		if err != nil {
			return acked, err
		}
		if done {
			acked = append(acked, it)
		}
	}
	return acked, nil
}

func (t *tick) applyItem(it Item) (bool, error) {
	var (
		m     Mission
		s     State
		acts  []Action
		steps []RunStep
		err   error
	)
	switch it.Kind {
	case "answer":
		m, s, acts, steps, err = t.answerItem(it)
	case "amend":
		m, s, acts, steps, err = t.amendItem(it)
	default:
		err = fmt.Errorf("unknown kind %q", it.Kind)
	}
	if err != nil {
		t.note("", "inbox item %s: %v", it.Name, err)
		return true, nil
	}
	for i := range steps {
		if err := t.runStep(&steps[i]); err != nil {
			t.note(steps[i].Piece, "inbox item %s: can't carry it out yet: %v", it.Name, err)
			return false, nil
		}
	}
	t.adopt(m, s)
	if err := t.save(); err != nil {
		return false, err
	}
	return true, t.perform(acts)
}

func (t *tick) answerItem(it Item) (Mission, State, []Action, []RunStep, error) {
	var a Answer
	if err := decodeStrict(it.Body, &a); err != nil {
		return t.m, t.s, nil, nil, err
	}
	m, s, acts, step, err := ApplyAnswer(t.m, t.s, a, t.in, t.now)
	if err != nil || step == nil {
		return m, s, acts, nil, err
	}
	return m, s, acts, []RunStep{*step}, nil
}

func (t *tick) amendItem(it Item) (Mission, State, []Action, []RunStep, error) {
	var a Amendment
	if err := decodeStrict(it.Body, &a); err != nil {
		return t.m, t.s, nil, nil, err
	}
	return Amend(t.m, t.s, a, t.in, t.now)
}

// decodeStrict decodes one JSON value and refuses unknown fields and
// trailing content.
func decodeStrict(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("can't read it: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("can't read it: content after the JSON value")
	}
	return nil
}
