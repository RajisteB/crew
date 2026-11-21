package mission

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/source"
)

func TestOrchestratorAgent_HasNoEditCapabilities(t *testing.T) {
	a := OrchestratorAgent()
	if a.Name != OrchestratorName {
		t.Fatalf("name = %q", a.Name)
	}
	if want := []string{"read", "search", "shell"}; !slices.Equal(a.Capabilities, want) {
		t.Fatalf("capabilities = %v, want %v", a.Capabilities, want)
	}
	if a.Model != source.ModelInherit {
		t.Fatalf("model = %q, want inherit", a.Model)
	}
	if a.Description == "" || a.Body != OrchestratorBody() {
		t.Fatal("description and body must be set")
	}
}

func TestOrchestratorBody_NamesEveryCommandItMayRun(t *testing.T) {
	body := OrchestratorBody()
	for _, c := range []string{
		"crew mission status", "crew mission plan -", "crew mission start", "crew mission go",
		"crew mission amend", "crew mission answer", "crew mission stop", "crew mission resume", "crew domain list", "crew factory list",
	} {
		if !strings.Contains(body, c) {
			t.Errorf("body does not name %q", c)
		}
	}
}

func TestOrchestratorBody_ForbidsPrincipalCommands(t *testing.T) {
	body := OrchestratorBody()
	if !strings.Contains(body, "Never run `crew automerge`, `crew repo confirm` or `crew release approve`") {
		t.Error("body must forbid the three owner commands in one sentence")
	}
	if !strings.Contains(body, "own terminal") {
		t.Error("body must tell the principal to run them in their own terminal")
	}
	if strings.Contains(body, "crew refuses") {
		t.Error("body must not claim crew refuses the commands; the session's deny rules do")
	}
	if !strings.Contains(body, "only when the principal has said to in this conversation") {
		t.Error("body must tie crew mission go to the principal saying go")
	}
}

func TestOrchestratorBody_WordsArePassedInlineNotByFile(t *testing.T) {
	body := OrchestratorBody()
	if !strings.Contains(body, "--words '") {
		t.Error("body must show --words with single quotes")
	}
	if strings.Contains(body, "@FILE") {
		t.Error("body must not use the @FILE form")
	}
}

func TestOrchestratorBody_AnswersAndFailures(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{"kind", "--station", "--note", "`merge`", "instead of inventing"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestOrchestratorBody_OrderAndHeadlessParagraph(t *testing.T) {
	body := OrchestratorBody()
	order := []string{"crew mission status", "crew domain list", "crew mission start", "crew mission amend", "crew automerge", "crew:changes"}
	last := -1
	for _, s := range order {
		i := strings.Index(body, s)
		if i < 0 || i < last {
			t.Fatalf("%q is missing or out of order (at %d, previous %d)", s, i, last)
		}
		last = i
	}
	tail := body[strings.LastIndex(body, "\n\n"):]
	for _, want := range []string{"JSON", "stuck piece", "no commands"} {
		if !strings.Contains(tail, want) {
			t.Errorf("last paragraph lacks %q:\n%s", want, tail)
		}
	}
	if n := strings.Count(body, "\n"); n > 100 {
		t.Errorf("body is %d lines, want about 90 or fewer", n)
	}
}

var exampleBlock = regexp.MustCompile("(?s)<<'EOF'\n(.*?)\nEOF\n")

func TestOrchestratorBody_ExamplePlanParsesAndChecks(t *testing.T) {
	m := exampleBlock.FindStringSubmatch(OrchestratorBody())
	if m == nil {
		t.Fatal("no quoted heredoc example in the body")
	}
	mis, err := Parse([]byte(m[1] + "\n"))
	if err != nil {
		t.Fatalf("example does not parse: %v\n%s", err, m[1])
	}
	if len(mis.Pieces) != 2 || !slices.Equal(mis.Pieces[1].WaitsFor, []string{mis.Pieces[0].ID}) {
		t.Fatalf("want two pieces, the second waiting for the first: %+v", mis.Pieces)
	}
	station := source.Station{Agent: "a"}
	in := CheckInput{
		Domains:   []source.Domain{{Name: mis.Pieces[0].Domain, Factories: []string{mis.Pieces[0].Factory}}},
		Factories: []source.Factory{{Name: mis.Pieces[0].Factory, Start: "build", Harness: "h", Stations: map[string]source.Station{"build": station}}},
		Registry:  &config.Registry{Harnesses: map[string]config.Harness{"h": okHarness()}},
		Limits:    Limits{Pieces: source.Limit{Set: true, N: 10}},
	}
	if errs := Check(mis, in); len(errs) > 0 {
		t.Fatalf("example fails Check: %v", errs)
	}
}
