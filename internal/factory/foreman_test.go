package factory_test

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/factory"
)

func TestForemanAgent(t *testing.T) {
	a := factory.ForemanAgent(templateFactory(t))
	if a.Name != "foreman-ship-change" || a.Model != "inherit" || strings.Join(a.Capabilities, ",") != "read,shell" {
		t.Fatalf("agent = %+v", a)
	}
	if !strings.Contains(a.Description, "ship-change") || strings.ContainsAny(a.Description, "\n<>") {
		t.Fatalf("description = %q", a.Description)
	}
}

func TestForemanBody_ContainsLoopAndDiagram(t *testing.T) {
	body := factory.ForemanBody(templateFactory(t))
	for _, want := range []string{
		"This factory takes one change request", // briefing
		"```mermaid\nflowchart LR\n",
		"crew run ship-change --detach --input",
		"crew run status <id> --since <cursor>",
		"crew run say <id>",
		"crew run goto <id> <station>",
		"crew run approve <id>",
		"Never run a station yourself",
		"Never edit files",
		"Never choose a route",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("foreman body missing %q", want)
		}
	}
}

func TestForemanBody_QuotesUserTextSafely(t *testing.T) {
	body := factory.ForemanBody(templateFactory(t))
	if strings.Contains(body, `--input "<input>"`) || strings.Contains(body, `say <id> "<message>"`) {
		t.Fatal("user text must never be put inside double quotes on a shell line")
	}
	for _, want := range []string{"<<'CREW_TEXT'", "--input @", "crew run say <id> @", "--note"} {
		if !strings.Contains(body, want) {
			t.Errorf("foreman body missing %q", want)
		}
	}
}
