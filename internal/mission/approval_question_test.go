package mission

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestApprovalQuestion_CutsOnARuneBoundary(t *testing.T) {
	// Three-byte runes, so a cut by bytes at an odd offset splits one.
	summary := strings.Repeat("€", maxSummaryLen+50)
	rs := run.State{Current: "plan", Pending: &run.Pending{Verdict: run.Verdict{Summary: summary}}}
	got := approvalQuestion(rs)
	if !utf8.ValidString(got) || utf8.RuneCountInString(got) != maxSummaryLen {
		t.Fatalf("question = %q (%d runes), want %d valid runes", got, utf8.RuneCountInString(got), maxSummaryLen)
	}
}
