package run_test

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestApprovedEvent(t *testing.T) {
	p := run.Pending{Station: "review", Route: "pass"}
	cases := []struct{ goTo, note, want string }{
		{"", "", ""},
		{"build", "", "sent to build"},
		{"", "ship it", "ship it"},
		{"build", "fix the typo", "sent to build: fix the typo"},
	}
	for _, c := range cases {
		ev := run.ApprovedEvent(p, c.goTo, c.note)
		if ev.Kind != run.EvApproved || ev.Station != "review" || ev.Route != "pass" || ev.Msg != c.want {
			t.Errorf("(%q, %q) = %+v, want msg %q", c.goTo, c.note, ev, c.want)
		}
	}
	long := strings.Repeat("é", 600)
	if got := []rune(run.ApprovedEvent(p, "", long).Msg); len(got) != 500 {
		t.Errorf("long note has %d runes, want 500", len(got))
	}
}

func TestFormatEvent_Approved(t *testing.T) {
	ev := run.Event{Kind: run.EvApproved, Station: "review", Route: "pass", Msg: "ship it"}
	if got := run.FormatEvent(ev); !strings.HasSuffix(got, "✔ approved review's pass: ship it") {
		t.Errorf("line = %q", got)
	}
	ev.Msg = ""
	if got := run.FormatEvent(ev); !strings.HasSuffix(got, "✔ approved review's pass") {
		t.Errorf("line = %q", got)
	}
}

func TestApprove_DoneKeepsTheVerdictSummaryAsReason(t *testing.T) {
	f := approvalFactory()
	paused, _ := walk(t, f, "ready", "done", "pass")
	s, d, err := run.Approve(f, paused, "")
	if err != nil || d.Kind != run.Finished || s.Status != run.StatusDone || s.Reason != "s-pass" {
		t.Fatalf("s=%+v d=%+v err=%v", s, d, err)
	}
	if _, d := run.Resume(f, s); d.Reason != "s-pass" {
		t.Fatalf("resume of an approved done run: %+v", d)
	}
	if _, d := run.Resume(f, run.State{Status: run.StatusDone}); d.Kind != run.Finished || d.Reason != "" {
		t.Fatalf("a done state with no reason: %+v", d)
	}
}
