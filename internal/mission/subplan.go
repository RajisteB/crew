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
	dec := json.NewDecoder(strings.NewReader(body))
	dec.DisallowUnknownFields()
	var sp SubPlan
	if err := dec.Decode(&sp); err != nil {
		return SubPlan{}, fmt.Errorf("the plan block is not one valid JSON object: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return SubPlan{}, errors.New("the plan block has content after the JSON object")
	}
	return sp, nil
}
