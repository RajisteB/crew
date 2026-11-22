package mission

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestPieceInput_CarriesRequestVerbatim(t *testing.T) {
	m := Mission{Request: "Launch referrals.\n\n  Pay $10 per friend (really).\n"}
	p := Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: "credits API"}
	got := PieceInput("m-1", m, p, sourceDomain("Product rules."), nil)
	for _, want := range []string{m.Request, "credits API", "Product rules.", "for context; do only your piece",
		"Commit your work on this branch. Don't push, and don't open or merge a pull request."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n## "); n != 4 || !strings.HasPrefix(got, "## ") {
		t.Errorf("sections: %d\n%s", n, got)
	}
}

func TestPRText_TitleBodyAndNoExtras(t *testing.T) {
	m := Mission{Request: "Line one.\nLine two."}
	p := Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: strings.Repeat("x", 100) + "\nsecond line"}
	last := run.StepRecord{Dir: "/secret/path", Verdict: run.Verdict{Summary: "all green", Notes: []string{"private note"}}}
	title, body := PRText("m-1", m, p, last)
	if len([]rune(title)) != 70 {
		t.Fatalf("title = %q", title)
	}
	for _, want := range []string{"m-1", "P1", "product", "feature", "> Line one.", "> Line two.", "all green"} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q:\n%s", want, body)
		}
	}
	for _, bad := range []string{"/secret/path", "private note", "second line"} {
		if strings.Contains(body, bad) || strings.Contains(title, bad) {
			t.Errorf("leaked %q", bad)
		}
	}
	if !strings.HasSuffix(strings.TrimSpace(body), "all green") {
		t.Errorf("body should end with the summary:\n%s", body)
	}
}

func amendedMission() Mission {
	return Mission{
		Request: "Launch referrals.\nPay $10 per friend.",
		Changes: []Change{
			{Time: t0, By: "principal", What: "re-briefed P2", Why: "make it $15"},
			{Time: t0, By: "principal", What: "added P7", Why: "make it $15"}, // same amendment, second change
			{Time: t0.Add(time.Hour), By: "orchestrator", What: "add piece P8", Why: "the model's own reason"},
			{Time: t0.Add(2 * time.Hour), By: "principal", What: "cancelled P3", Why: "drop the landing page"},
		},
	}
}

func TestMission_PrincipalChangesAreAmendmentWordsInOrder(t *testing.T) {
	got := amendedMission().PrincipalChanges()
	want := []string{"make it $15", "drop the landing page"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q want %q", got, want)
	}
	if len((Mission{}).PrincipalChanges()) != 0 {
		t.Fatal("no changes, no words")
	}
}

func TestPieceInput_AmendmentsFollowTheRequest(t *testing.T) {
	m := amendedMission()
	p := Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: "credits API"}
	got := PieceInput("m-1", m, p, sourceDomain("Product rules."), nil)
	heading := "## " + principalChangesTitle
	for _, want := range []string{heading, "make it $15", "drop the landing page",
		"(where they differ from the request, the latest change wins)"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "the model's own reason") {
		t.Error("the orchestrator's own changes must not appear")
	}
	i, j, k := strings.Index(got, m.Request), strings.Index(got, "make it $15"), strings.Index(got, "drop the landing page")
	if !(i < j && j < k) || strings.Index(got, heading) < i {
		t.Errorf("order wrong:\n%s", got)
	}
	if plain := PieceInput("m-1", Mission{Request: "x"}, p, sourceDomain("d"), nil); strings.Contains(plain, principalChangesTitle) {
		t.Errorf("no amendments, no section:\n%s", plain)
	}
}
