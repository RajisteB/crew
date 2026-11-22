package mission

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTrackerText_LongBodiesAreCut(t *testing.T) {
	long := strings.Repeat("é", 40_000)
	m := Mission{Request: long, Pieces: []Piece{{ID: "P1", Domain: "product", Factory: "feature", Brief: long}}}
	_, epic := EpicText("m-1", m)
	_, issue := IssueText("m-1", m, m.Pieces[0])
	const mark = "\n… (cut: the full text is in the mission folder)"
	for name, body := range map[string]string{"epic": epic, "issue": issue} {
		head, ok := strings.CutSuffix(body, mark)
		if !ok || utf8.RuneCountInString(head) != maxBodyRunes {
			t.Errorf("%s: %d runes, ends with the mark: %v", name, utf8.RuneCountInString(head), ok)
		}
		if n := utf8.RuneCountInString(body); n > 32_767 {
			t.Errorf("%s: %d runes is over Jira's limit", name, n)
		}
	}
	_, short := EpicText("m-1", Mission{Request: "short"})
	if strings.Contains(short, "cut:") {
		t.Errorf("a short body was marked: %q", short)
	}
}
