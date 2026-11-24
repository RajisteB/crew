package mission

import (
	"strings"
	"testing"
)

func TestOrchestratorBody_PlainRunStartsDetachedFromStdin(t *testing.T) {
	want := "crew run <factory> --detach --input - <<'CREW_TEXT'"
	if !strings.Contains(OrchestratorBody(), want) {
		t.Errorf("body lacks %q", want)
	}
}

func TestOrchestratorBody_PlainRunIsWatchedWithWait(t *testing.T) {
	want := "crew run status <id> --since <cursor> --wait 90s"
	if !strings.Contains(OrchestratorBody(), want) {
		t.Errorf("body lacks %q", want)
	}
}

func TestOrchestratorBody_PlainRunApprovalNeedsThePrincipal(t *testing.T) {
	for _, want := range []string{
		"crew run approve <id> --detach",
		"only after the principal has said, in this conversation, to approve that stop",
	} {
		if !strings.Contains(OrchestratorBody(), want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestOrchestratorBody_PlainRunLooksForAMissionFirst(t *testing.T) {
	want := "does not check it against those pieces"
	if !strings.Contains(OrchestratorBody(), want) {
		t.Errorf("body lacks %q", want)
	}
}

func TestOrchestratorBody_LeavesAMissionsRunsAlone(t *testing.T) {
	if !strings.Contains(OrchestratorBody(), "owner: mission") {
		t.Error("body does not mention a mission's owner line")
	}
}

func TestOrchestratorBody_PlainRunsNeedNoSleepOrFiles(t *testing.T) {
	for _, bad := range []string{"sleep", "mktemp", `@"$f"`} {
		if strings.Contains(orchestratorRuns, bad) {
			t.Errorf("orchestratorRuns contains %q", bad)
		}
	}
}

func TestOrchestratorBody_HeadlessRuleIsStillLast(t *testing.T) {
	want := "reply only as it says and run no commands.\n"
	if !strings.HasSuffix(OrchestratorBody(), want) {
		t.Errorf("body does not end with %q", want)
	}
}

func TestOrchestratorBody_StillObeysTheSentencesItLeavesOut(t *testing.T) {
	want := "You still do what the sentences you left out say."
	if !strings.Contains(OrchestratorBody(), want) {
		t.Fatalf("the orchestrator prompt must contain %q", want)
	}
}
