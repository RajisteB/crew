package mission

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

const (
	planTag     = "plan"
	failurePlan = "plan" // Decision.Failure for a planning try
)

// SubPlan is the scoped orchestrator's reply for a delegated piece.
type SubPlan struct {
	Pieces   []Piece `json:"pieces"`
	Question string  `json:"question"` // instead of pieces: what it must know before it can plan
	Reason   string  `json:"reason"`
}

// scopedBy names the scoped orchestrator of a delegated piece: the By of its
// events, decisions and changes.
func scopedBy(parent string) string { return "scoped orchestrator " + parent }

// ParseSubPlan decodes the last fenced block tagged `plan`: one JSON object,
// no unknown fields, nothing after it.
func ParseSubPlan(reply string) (SubPlan, error) {
	body, ok := lastTaggedBlock(reply, planTag)
	if !ok {
		return SubPlan{}, errors.New("the reply has no fenced block tagged `plan`")
	}
	return decodeOne[SubPlan](body, planTag)
}

// decodeOne decodes body as exactly one JSON object of type T: no unknown
// fields, nothing after it. what names the block in the error texts.
func decodeOne[T any](body, what string) (T, error) {
	var v, zero T
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return zero, fmt.Errorf("the %s block is not one valid JSON object: %w", what, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return zero, fmt.Errorf("the %s block has content after the JSON object", what)
	}
	return v, nil
}
