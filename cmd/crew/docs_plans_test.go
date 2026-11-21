package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

// planBlocks returns the body of every fenced block in text that starts with
// a frontmatter fence and names pieces: those are example mission plans.
func planBlocks(text string) []string {
	var blocks []string
	var cur []string
	open := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case !open && strings.HasPrefix(line, "```"):
			open, cur = true, nil
		case open && strings.HasPrefix(line, "```"):
			open = false
			body := strings.Join(cur, "\n") + "\n"
			if strings.HasPrefix(body, "---\n") && strings.Contains(body, "pieces:") {
				blocks = append(blocks, body)
			}
		case open:
			cur = append(cur, line)
		}
	}
	return blocks
}

func TestDocs_ExamplePlansParse(t *testing.T) {
	found := 0
	for _, name := range []string{"docs/missions.md", "README.md"} {
		b, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		for i, block := range planBlocks(string(b)) {
			found++
			if _, err := mission.Parse([]byte(block)); err != nil {
				t.Errorf("%s: example plan %d is rejected: %v\n%s", name, i+1, err, block)
			}
		}
	}
	if found < 2 {
		t.Fatalf("found %d example plans, want at least 2; the test would pass by finding none", found)
	}
}
