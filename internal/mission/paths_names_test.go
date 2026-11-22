package mission

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestPatternsOverlap_BackslashCountsAsAWildcard(t *testing.T) {
	for _, r := range [][2]string{{`foo\bar`, "foobar"}, {`\a`, "a"}} {
		if !PatternsOverlap(r[0], r[1]) || !PatternsOverlap(r[1], r[0]) {
			t.Errorf("PatternsOverlap(%q, %q) = false; crew can't tell, so they overlap", r[0], r[1])
		}
	}
}

func TestPatternsOverlap_ManyDoubleStarsAnswerQuickly(t *testing.T) {
	a := strings.Repeat("**/a/", 12) + "x.go"
	b := strings.Repeat("**/a/", 12) + "y.go"
	start := time.Now()
	got := PatternsOverlap(a, b)
	if took := time.Since(start); took > 200*time.Millisecond {
		t.Fatalf("PatternsOverlap took %s", took)
	}
	if got {
		t.Fatalf("PatternsOverlap(%q, %q) = true, want false", a, b)
	}
}

func TestCheck_PiecePathWithAControlCharacterIsRefused(t *testing.T) {
	m := acme()
	m.Pieces[0].Paths = []string{"app/\rapi/**", "site/\u202e**"}
	want := `P1: paths: "app/?api/**": holds a control or invisible character`
	for _, err := range Check(m, baseInput()) {
		if err.Error() == want {
			return
		}
	}
	t.Fatalf("Check = %v, want %q", Check(m, baseInput()), want)
}

func TestPathsQuestion_OneFile(t *testing.T) {
	if got, want := pathsQuestion(PieceState{ID: "P1", Outside: []string{"go.mod"}}), "P1 changed 1 file outside its paths: go.mod"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	three := PieceState{ID: "P1", Outside: []string{"a", "b", "c"}}
	if got, want := pathsQuestion(three), "P1 changed 3 files outside its paths: a, b, c"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	more := PieceState{ID: "P1", Outside: []string{"a", "b", "c", "d"}}
	if got, want := pathsQuestion(more), "P1 changed 4 files outside its paths: a, b, c and 1 more"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSummary_OutsideFilesAreCapped(t *testing.T) {
	var files []string
	for i := range 30 {
		files = append(files, fmt.Sprintf("f%02d.go", i))
	}
	text := outsideText(Summary{}.ForPaths(PieceState{ID: "P1", Outside: files, OutsideMore: 5}))
	lines := strings.Split(text, "\n")
	if len(lines) != 21 || lines[19] != "f19.go" || lines[20] != "and 15 more" {
		t.Fatalf("outside text = %q", text)
	}
}

func TestUncommittedNote_CleansNames(t *testing.T) {
	noForgery(t, "uncommitted note", uncommittedNote([]string{forgedName, bidiName}))
}
