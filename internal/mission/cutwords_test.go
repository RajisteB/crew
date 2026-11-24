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
	if want := "Add an exported function Farewell(name string) string to the greet"; title != want {
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

func TestCutTitle(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"fits", "Add a greeting", "Add a greeting"},
		{"exactly 70", strings.Repeat("a", 70), strings.Repeat("a", 70)},
		{"cut at a word", longSentence, "Add an exported function Farewell(name string) string to the greet"},
		{"drops trailing punctuation", strings.Repeat("a", 60) + ", bbbbbbbbbbbbbbbbbbbb", strings.Repeat("a", 60)},
		{"one long word is cut hard", strings.Repeat("x", 90), strings.Repeat("x", 70)},
		{"space in the first half is ignored", "ab " + strings.Repeat("c", 80), "ab " + strings.Repeat("c", 67)},
		{"multi-byte runes", strings.Repeat("é", 80), strings.Repeat("é", 70)},
	}
	for _, tc := range tests {
		got := cutTitle(tc.in, maxTitleRunes)
		if got != tc.want {
			t.Errorf("%s: cutTitle = %q, want %q", tc.name, got, tc.want)
		}
		if strings.Contains(got, "…") || utf8.RuneCountInString(got) > maxTitleRunes || !utf8.ValidString(got) {
			t.Errorf("%s: %q has a mark, is too long or is invalid", tc.name, got)
		}
	}
}

func TestPRText_ShortTitleIsUnchanged(t *testing.T) {
	p := Piece{ID: "P1", Domain: "product", Factory: "feature", Brief: "Add a greeting\nmore"}
	if title, _ := PRText("m-1", Mission{Request: "r"}, p, run.StepRecord{}); title != "Add a greeting" {
		t.Errorf("title = %q", title)
	}
}
