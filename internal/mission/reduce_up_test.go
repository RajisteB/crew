package mission

import (
	"testing"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

// A climb interrupted part way leaves Up raised; a stop of a pr-open piece
// that then ends with no decision puts it back to 0, so the next stop asks
// the nearest orchestrator first.
func TestUp_GoesBackToZeroWhenAStopEndsWithNoDecision(t *testing.T) {
	m, s, o := prCase(0, principal.AutoOn, chk(gh.CheckPending))
	s.Pieces[0].Up = 1
	if got := piece(t, SentBackDone(s, "a", reasonConflict, ""), "a").Up; got != 0 {
		t.Errorf("SentBackDone: Up = %d", got)
	}
	if got := piece(t, BranchUpdated(s, "a", "def", t0), "a").Up; got != 0 {
		t.Errorf("BranchUpdated: Up = %d", got)
	}
	if s1, _ := decide(m, s, o); piece(t, s1, "a").Up != 0 {
		t.Errorf("prOpen with no stop: Up = %d", piece(t, s1, "a").Up)
	}
	o = withPR(o, func(pr *gh.PR) { pr.Mergeable = "CONFLICTING" })
	s.Pieces[0].SentBack = map[string]int{reasonConflict: maxPRSendBacks}
	if s1, as := decide(m, s, o); piece(t, s1, "a").Up != 1 || len(of[AskOrchestrator](as)) != 1 {
		t.Errorf("a stop still there keeps climbing: Up = %d, actions %+v", piece(t, s1, "a").Up, as)
	}
}
