package mission

import (
	"strings"
	"testing"
)

// cancelMsg cancels P1 in status with PR #7 and returns the event's text.
func cancelMsg(t *testing.T, status PieceStatus, wait *Wait) string {
	t.Helper()
	s := exState()
	s.Pieces[0] = PieceState{ID: "P1", Status: status, PR: 7, Wait: wait}
	_, _, as, _ := ApplyDecision(acme(), s, "P1", "failed", OrchDecision{Action: "cancel"}, "orchestrator", exInput(), t0)
	for _, r := range of[Record](as) {
		if r.Event.Kind == EvPieceCancelled {
			return r.Event.Msg
		}
	}
	t.Fatalf("no cancel record in %v", as)
	return ""
}

func TestCancel_AMergedPullRequestIsNotSaidToStayOpen(t *testing.T) {
	if msg := cancelMsg(t, PieceDelivered, nil); strings.Contains(msg, "stays open") {
		t.Errorf("delivered: msg = %q", msg)
	}
	for _, st := range []PieceStatus{PiecePROpen, PieceRunning, PieceWaiting} {
		if msg := cancelMsg(t, st, &Wait{ID: "w1", Kind: WaitMerge}); !strings.Contains(msg, "PR #7 stays open") {
			t.Errorf("%s: msg = %q", st, msg)
		}
	}
}
