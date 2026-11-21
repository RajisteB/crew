package mission

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/config"
	"github.com/crewfile-dev/crew/internal/source"
)

func okHarness() config.Harness {
	return config.Harness{Headless: &config.HeadlessSpec{DenyVia: "settings-file"}}
}

func baseInput() CheckInput {
	station := source.Station{Agent: "a"}
	fac := func(name string) source.Factory {
		return source.Factory{Name: name, Start: "build", Harness: "claude-code",
			Stations: map[string]source.Station{"build": station}}
	}
	return CheckInput{
		Domains: []source.Domain{
			{Name: "product", Factories: []string{"feature"}},
			{Name: "marketing", Factories: []string{"campaign", "feature"}},
			{Name: "design", Factories: []string{"visuals"}},
		},
		Factories: []source.Factory{fac("feature"), fac("campaign"), fac("visuals")},
		Registry:  &config.Registry{Harnesses: map[string]config.Harness{"claude-code": okHarness(), "plain": {}}},
		Limits:    Limits{Pieces: source.Limit{Set: true, N: 10}},
	}
}

func acme() Mission {
	return Mission{
		Request: "Launch referrals: $10 credit per invited friend who signs up, plus a landing page and an ad campaign.",
		Pieces: []Piece{
			{ID: "P1", Domain: "product", Factory: "feature", Brief: "credits API", Paths: []string{"app/api/**"}},
			{ID: "P2", Domain: "product", Factory: "feature", Brief: "invite screen", Paths: []string{"app/ui/**"}, WaitsFor: []string{"P1"}},
			{ID: "P4", Domain: "marketing", Factory: "feature", Brief: "landing page", Paths: []string{"site/**"}},
			{ID: "P5", Domain: "marketing", Factory: "campaign", Brief: "campaign brief"},
			{ID: "P6", Domain: "marketing", Factory: "campaign", Brief: "ad campaign", Paths: []string{"ads/**"}, WaitsFor: []string{"P1", "P2", "P5"}},
		},
	}
}

func messages(errs []error) string {
	var s []string
	for _, e := range errs {
		s = append(s, e.Error())
	}
	return strings.Join(s, "\n")
}

func wantError(t *testing.T, errs []error, parts ...string) {
	t.Helper()
	for _, e := range errs {
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(e.Error(), p)
		}
		if ok {
			return
		}
	}
	t.Errorf("no error containing %q in:\n%s", parts, messages(errs))
}

func TestCheck_Valid(t *testing.T) {
	if errs := Check(acme(), baseInput()); len(errs) != 0 {
		t.Errorf("errors:\n%s", messages(errs))
	}
}

func TestCheck_UnlimitedPieces(t *testing.T) {
	in := baseInput()
	in.Limits.Pieces = source.Limit{Set: true, Unlimited: true}
	if errs := Check(acme(), in); len(errs) != 0 {
		t.Errorf("errors:\n%s", messages(errs))
	}
}

func TestCheck_IDs(t *testing.T) {
	m := acme()
	m.Pieces[0].ID = ""
	m.Pieces[1].ID = "bad id!"
	m.Pieces[2].ID = "P5"
	errs := Check(m, baseInput())
	wantError(t, errs, "piece 1", "no id")
	wantError(t, errs, "bad id!", "id must be")
	wantError(t, errs, "piece P5", "more than once")
}

func TestCheck_UnknownDomainFactoryAndBrief(t *testing.T) {
	m := acme()
	m.Pieces[0].Domain = "legal"
	m.Pieces[1].Factory = "ghost"
	m.Pieces[2].Brief = "  "
	m.Request = ""
	errs := Check(m, baseInput())
	wantError(t, errs, "piece P1", "unknown domain")
	wantError(t, errs, "piece P2", "unknown factory")
	wantError(t, errs, "piece P4", "brief is empty")
	wantError(t, errs, "request is empty")
}

func TestCheck_FactoryNotInDomain(t *testing.T) {
	m := acme()
	m.Pieces[0].Factory = "visuals"
	wantError(t, Check(m, baseInput()), "piece P1", "visuals", "product")
}

func TestCheck_DelegateRefused(t *testing.T) {
	m := acme()
	m.Pieces[0] = Piece{ID: "P3", Domain: "design", Delegate: true, Brief: "b"}
	errs := Check(m, baseInput())
	wantError(t, errs, "piece P3: delegation isn't available yet")
	m.Pieces[0].Factory = "visuals"
	wantError(t, Check(m, baseInput()), "piece P3", "not both")
	m.Pieces[0] = Piece{ID: "P3", Domain: "design", Brief: "b"}
	wantError(t, Check(m, baseInput()), "piece P3", "set a factory or delegate")
}

func TestCheck_WaitsForProblems(t *testing.T) {
	m := acme()
	m.Pieces[0].WaitsFor = []string{"P1", "P9"}
	errs := Check(m, baseInput())
	wantError(t, errs, "piece P1", "itself")
	wantError(t, errs, "piece P1", "P9", "not a piece")
}

func TestCheck_WaitCycle(t *testing.T) {
	m := acme()
	m.Pieces[1].WaitsFor = []string{"P4"} // P2 -> P4
	m.Pieces[2].WaitsFor = []string{"P2"} // P4 -> P2
	errs := Check(m, baseInput())
	wantError(t, errs, "P2 and P4 wait on each other")
	m.Pieces[1].WaitsFor = []string{"P6"}
	m.Pieces[2].WaitsFor = []string{"P2"}
	m.Pieces[4].WaitsFor = []string{"P4"}
	wantError(t, Check(m, baseInput()), "P2, P4 and P6 wait on each other")
}

func TestCheck_TooManyPieces(t *testing.T) {
	in := baseInput()
	in.Limits.Pieces = source.Limit{Set: true, N: 4}
	wantError(t, Check(acme(), in), "5 pieces", "limit of 4")
}

func TestCheck_HarnessCannotDeny(t *testing.T) {
	in := baseInput()
	in.Factories[0].Harness = "plain"
	errs := Check(acme(), in)
	wantError(t, errs, "piece P1", "build", "plain", "deny")
	in.Factories[0].Harness = "missing"
	wantError(t, Check(acme(), in), "piece P1", "missing")
	in.Registry = nil
	wantError(t, Check(acme(), in), "piece P1", "deny")
}

func TestCheck_WatchOutsideRepo(t *testing.T) {
	for _, root := range []string{"/etc", "..", "../x", "a/../../x"} {
		in := baseInput()
		in.Factories[0].Watch = []string{"src", root}
		wantError(t, Check(acme(), in), "piece P1", root, "outside the repo")
	}
	in := baseInput()
	in.Factories[0].Watch = []string{"..foo", "a/b/.."}
	if errs := Check(acme(), in); len(errs) != 0 {
		t.Errorf("errors:\n%s", messages(errs))
	}
}

func TestCheck_ReturnsEveryErrorInFileOrder(t *testing.T) {
	m := acme()
	m.Pieces[0].Brief = ""
	m.Pieces[1].Brief = ""
	errs := Check(m, baseInput())
	if len(errs) != 2 || !strings.Contains(errs[0].Error(), "P1") || !strings.Contains(errs[1].Error(), "P2") {
		t.Errorf("errors:\n%s", messages(errs))
	}
}

func TestCheck_MixedErrorsComeOutPieceByPiece(t *testing.T) {
	m := acme()
	m.Request = ""
	m.Pieces[0].Domain = "legal"
	m.Pieces[0].WaitsFor = []string{"P9"}
	m.Pieces[1].ID = "P1"
	m.Pieces[2].WaitsFor = []string{"P5"}
	m.Pieces[3].WaitsFor = []string{"P4"}
	in := baseInput()
	in.Limits.Pieces = source.Limit{Set: true, N: 4}
	want := []string{
		"the request is empty",
		"5 pieces is more than the limit of 4",
		`piece P1: unknown domain "legal"`,
		`piece P1: waits_for "P9", which is not a piece`,
		"piece P1: id is used more than once",
		"piece P1: waits for itself",
		`piece P6: waits_for "P2", which is not a piece`,
		"P4 and P5 wait on each other",
	}
	got := strings.Split(messages(Check(m, in)), "\n")
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
