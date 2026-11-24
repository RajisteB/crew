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

func TestOrchestratorBody_AnswersOnlyOnPrincipalsSay(t *testing.T) {
	body := OrchestratorBody()
	i := strings.Index(body, "lists each item waiting")
	if i < 0 {
		t.Fatal("no waiting-items paragraph")
	}
	para := body[i:]
	if j := strings.Index(para, "\n\n"); j > 0 {
		para = para[:j]
	}
	for _, want := range []string{
		"only after the principal has said, in this conversation, what to do about that item",
		"ask them instead of choosing for them",
		"the decision is theirs",
		"a question about a piece that hasn't started takes only cancel",
	} {
		if !strings.Contains(para, want) {
			t.Errorf("waiting-items paragraph lacks %q", want)
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
	for _, want := range []string{"JSON", "stuck piece", "split a piece into pieces", "no commands"} {
		if !strings.Contains(tail, want) {
			t.Errorf("last paragraph lacks %q:\n%s", want, tail)
		}
	}
	if n := strings.Count(body, "\n"); n > 100 {
		t.Errorf("body is %d lines, want 100 or fewer", n)
	}
}

var exampleBlock = regexp.MustCompile("(?s)<<'CREW_PLAN'\n(.*?)\nCREW_PLAN\n")

func TestOrchestratorBody_ExamplePlanParsesAndChecks(t *testing.T) {
	m := exampleBlock.FindStringSubmatch(OrchestratorBody())
	if m == nil {
		t.Fatal("no quoted CREW_PLAN heredoc example in the body")
	}
	mis, err := Parse([]byte(m[1] + "\n"))
	if err != nil {
		t.Fatalf("example does not parse: %v\n%s", err, m[1])
	}
	if len(mis.Pieces) != 3 || !slices.Equal(mis.Pieces[1].WaitsFor, []string{mis.Pieces[0].ID}) {
		t.Fatalf("want three pieces, the second waiting for the first: %+v", mis.Pieces)
	}
	station := source.Station{Agent: "a"}
	in := CheckInput{
		Domains:             []source.Domain{{Name: mis.Pieces[0].Domain, Factories: []string{mis.Pieces[0].Factory}}},
		Factories:           []source.Factory{{Name: mis.Pieces[0].Factory, Start: "build", Harness: "h", Stations: map[string]source.Station{"build": station}}},
		Registry:            &config.Registry{Harnesses: map[string]config.Harness{"h": okHarness()}},
		Limits:              Limits{Pieces: source.Limit{Set: true, N: 10}, Depth: source.Limit{Set: true, N: 2}},
		OrchestratorHarness: "h",
	}
	if errs := Check(mis, in); len(errs) > 0 {
		t.Fatalf("example fails Check: %v", errs)
	}
}

func TestOrchestratorBody_StartOfSessionHandlesADeadEngine(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{"`engine not running`", "offer `crew mission resume <id>`", "when they say so"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Index(body, "engine not running") > strings.Index(body, "To plan, run") {
		t.Error("the dead-engine rule belongs to point 1, before planning")
	}
}

func TestOrchestratorBody_AmendKeepsTheRequestAndNamesTheFile(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{
		"keeps the request text exactly as it is in the current plan, changing only the pieces",
		"`.crew/missions/<id>/mission.md`",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestOrchestratorBody_ExplainsEachChoiceAndDoesNotLetItGuess(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{
		"retry runs the failed station again", "goto restarts from the station named",
		"cancel gives up on the piece", "approve lets a station that asked for approval continue",
		"If crew says the choice doesn't fit the item, tell the principal which choices it lists.",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	if strings.Contains(body, "If you pick a wrong choice") {
		t.Error("the agent must not be told to pick a choice and correct it")
	}
}

func TestOrchestratorBody_HeredocDelimiterIsChosenNotEOF(t *testing.T) {
	body := OrchestratorBody()
	if strings.Contains(body, "<<'EOF'") {
		t.Error("the example must not use EOF as its delimiter")
	}
	for _, want := range []string{"<<'CREW_PLAN'", "pick a delimiter that doesn't appear as a line in the request"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestOrchestratorBody_AutoMerge(t *testing.T) {
	body := OrchestratorBody()
	for _, s := range strings.FieldsFunc(body, func(r rune) bool { return r == '.' || r == '\n' }) {
		if strings.Contains(s, "crew automerge") && !strings.Contains(s, "never run") && !strings.Contains(s, "Never run") && !strings.Contains(s, "not run") {
			t.Errorf("a sentence names crew automerge without saying not to run it: %q", s)
		}
	}
	for _, want := range []string{"without-checks", "own terminal", "says why", "two more tries", "nothing is asked", "still running"} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestOrchestratorBody_MentionsDeliverables(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{
		"--do accept", "deliverable", "waits_for", "never write to Jira", "crew jira setup", "moving its Jira issue to Done",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
}

func TestOrchestratorBody_JiraRefusalGoesToThePrincipal(t *testing.T) {
	body := OrchestratorBody()
	want := "When `crew mission go`, `start` or `resume` is refused because the Jira variables are not set, don't try to set them and don't touch `jira.yaml`: give the principal the exact command to run in their own terminal."
	if !strings.Contains(body, want) {
		t.Errorf("body lacks %q", want)
	}
}

func TestOrchestratorBody_MentionsPathsAndBudgets(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{"paths", "accept", "on hold", "missions.yaml", "another mission"} {
		if !strings.Contains(body, want) {
			t.Errorf("orchestrator body does not mention %q", want)
		}
	}
}

func TestOrchestratorBody_MentionsDelegation(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{
		"`delegate: true`", "P3.1", "`plan` wait", "`retry`", "`cancel`",
		"Never write an id with a `.`", "crew mission amend",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body lacks %q", want)
		}
	}
	m := exampleBlock.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no example")
	}
	mis, err := Parse([]byte(m[1] + "\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(mis.Pieces) != 3 || !mis.Pieces[2].Delegate || mis.Pieces[2].Factory != "" {
		t.Fatalf("the example's third piece must be delegated, with no factory: %+v", mis.Pieces)
	}
}

func TestOrchestratorBody_ApprovalTakesGoto(t *testing.T) {
	if !strings.Contains(OrchestratorBody(), "An approval takes approve, goto or cancel") {
		t.Error("body does not say an approval takes goto")
	}
}

func TestOrchestratorBody_HandlingInstructionsStayOutOfTheRequest(t *testing.T) {
	body := OrchestratorBody()
	for _, want := range []string{
		"Leave out, whole, any sentence that only tells you how to handle the mission",
		"word for word",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body does not contain %q", want)
		}
	}
}
