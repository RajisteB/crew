package mission

import (
	"fmt"
	"time"
)

// AmendPreview is what Amend would do with a, one line per change, for the
// person who queues it: "will cancel P3.2 (running)", "will re-brief P4",
// "will change P4: waits_for changed", "will add P5", and "keeps P3.1, P3.2
// under P3: the plan named no piece under it" for a carry-over. On an error
// it returns what Amend would.
func AmendPreview(m Mission, s State, a Amendment, in CheckInput, now time.Time) ([]string, error) {
	plan, err := planAmendment(m, s, a, in, now)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(plan.changes)+len(plan.kept))
	for _, c := range plan.changes {
		out = append(out, previewLine(s, c))
	}
	for _, c := range plan.kept {
		out = append(out, "keeps "+c.text()+": the plan named no piece under it")
	}
	return out, nil
}

func previewLine(s State, c pieceChange) string {
	switch c.Kind {
	case chAdd:
		return "will add " + c.Piece
	case chRebrief:
		return "will re-brief " + c.Piece
	case chCancel:
		ps, _ := s.Piece(c.Piece)
		return fmt.Sprintf("will cancel %s (%s)", c.Piece, ps.Status)
	}
	return "will change " + c.Edit
}
