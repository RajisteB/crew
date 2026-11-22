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

func TestTrackerComment_BudgetHit(t *testing.T) {
	ev := Event{Kind: EvBudgetHit, Piece: "P1", Count: 3, Max: "3", Msg: "orchestrator calls for this piece"}
	piece, text, ok := TrackerComment(ev, State{})
	if !ok || piece != "P1" || text != "Budget reached: orchestrator calls for this piece (3 of 3)" {
		t.Fatalf("piece %q text %q ok %v", piece, text, ok)
	}
	ev = Event{Kind: EvBudgetHit, Count: 6, Max: "6", Msg: "station steps for this mission"}
	piece, text, ok = TrackerComment(ev, State{})
	if !ok || piece != "" || text != "Budget reached: station steps for this mission (6 of 6)" {
		t.Fatalf("piece %q text %q ok %v", piece, text, ok)
	}
}
