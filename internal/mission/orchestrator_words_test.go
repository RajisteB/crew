package mission

import (
	"strings"
	"testing"
)

func TestOrchestratorBody_SaysTheConfirmationIsNormalAndToReportItsAbsence(t *testing.T) {
	want := "Starting a run, `crew run goto` and `crew run approve` are not pre-approved in this session: the principal is normally asked to confirm each one. That is intended. Never look for another way to run them, and if one of them runs without the principal being asked, tell them so."
	if !strings.Contains(OrchestratorBody(), want) {
		t.Errorf("body lacks %q", want)
	}
}

func TestOrchestratorBody_OpeningAgreesWithPlainRuns(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{
		"You plan missions, start and watch plain runs, and report on both.",
		"You never edit files, run a station's work or merge anything, and you never decide an approval: you pass on the principal's decision. You have no need to do more: the engine does the work, and the principal makes the calls that matter.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "merge or approve anything") {
		t.Error("body still says it never approves anything")
	}
}

func TestOrchestratorAgent_DescriptionNamesPlainRuns(t *testing.T) {
	want := "Plans missions, starts and watches plain runs, and reports on both for the principal: splits a job into pieces, shows the plan, starts the mission and relays what is waiting. Launch it as the main session."
	if got := OrchestratorAgent().Description; got != want {
		t.Errorf("Description = %q", got)
	}
}
