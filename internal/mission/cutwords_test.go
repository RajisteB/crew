package mission

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/crewfile-dev/crew/internal/run"
)

const longSentence = "Add an exported function Farewell(name string) string to the greet package and a test for it."

func TestCutWords(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 70, "short"},
		{"alpha beta gamma", 12, "alpha beta…"},
		{"alpha beta, gamma", 13, "alpha beta…"},
		{"abcdefghijklmnop", 8, "abcdefg…"},
		{"ab cdefghijklmnopqrstuvwxyz", 12, "ab cdefghij…"},
		{"ééééé ééééé ééééé", 12, "ééééé ééééé…"},
		{"Fix the bug. Then more words here", 14, "Fix the bug…"},
		{longSentence, 70, "Add an exported function Farewell(name string) string to the greet…"},
	}
	for _, tc := range tests {
		got := cutWords(tc.in, tc.n)
		if got != tc.want {
			t.Errorf("cutWords(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		}
		if utf8.RuneCountInString(got) > tc.n || !utf8.ValidString(got) {
			t.Errorf("cutWords(%q, %d) = %q is too long or invalid", tc.in, tc.n, got)
		}
	}
}

func TestPRText_TitleEndsAtAWord(t *testing.T) {
	p := Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: longSentence + "\nmore"}
	title, _ := PRText("m-1", Mission{Request: "r"}, p, run.StepRecord{})
	if want := "Add an exported function Farewell(name string) string to the greet…"; title != want {
		t.Errorf("title = %q, want %q", title, want)
	}
}

func TestTrackerText_SummariesEndAtAWord(t *testing.T) {
	long := strings.Repeat("word ", 40)
	m := Mission{Request: long, Pieces: []Piece{{ID: "P1", Domain: "product", Factory: "feature", Brief: long}}}
	epic, _ := EpicText("m-1", m)
	issue, _ := IssueText("m-1", m, m.Pieces[0])
	for name, s := range map[string]string{"epic": epic, "issue": issue} {
		if !strings.HasSuffix(s, "…") || utf8.RuneCountInString(s) > maxSummaryRunes {
			t.Errorf("%s summary = %q (%d runes)", name, s, utf8.RuneCountInString(s))
		}
	}
}
