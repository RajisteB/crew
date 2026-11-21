package mission

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestPieceInput_CarriesRequestVerbatim(t *testing.T) {
	m := Mission{Request: "Launch referrals.\n\n  Pay $10 per friend (really).\n"}
	p := Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: "credits API"}
	got := PieceInput("m-1", m, p, sourceDomain("Product rules."))
	for _, want := range []string{m.Request, "credits API", "Product rules.", "for context; do only your piece",
		"Commit your work on this branch. Don't push, and don't open or merge a pull request."} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n## "); n != 3 || !strings.HasPrefix(got, "## ") {
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
