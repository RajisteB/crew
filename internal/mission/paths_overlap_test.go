package mission

import (
	"slices"
	"testing"
)

var overlapRows = []struct {
	a, b string
	want bool
}{
	{"api/**", "api/v2/**", true},
	{"api", "api/handlers/x.go", true},
	{"**/*.md", "docs/readme.md", true},
	{"src/*.go", "src/main.go", true},
	{"**", "anything/at/all", true},
	{"a/**/z", "a/b/c/z", true},
	{"docs", "docs", true},
	{"api/**", "web/**", false},
	{"src/*.go", "src/*.md", false},
	{"a/b", "a/c", false},
	{"api/*", "api/v2/x.go", false},
	{"a*b", "a", false},
	{"", "**", false},
	{"[", "**", false},
}

func TestPatternsOverlap(t *testing.T) {
	for _, r := range overlapRows {
		if got := PatternsOverlap(r.a, r.b); got != r.want {
			t.Errorf("PatternsOverlap(%q, %q) = %v, want %v", r.a, r.b, got, r.want)
		}
	}
}

func TestPatternsOverlap_IsSymmetric(t *testing.T) {
	for _, r := range overlapRows {
		if got := PatternsOverlap(r.b, r.a); got != r.want {
			t.Errorf("PatternsOverlap(%q, %q) = %v, want %v", r.b, r.a, got, r.want)
		}
	}
}

func TestPathsOverlap_ReturnsThePair(t *testing.T) {
	pa, pb, ok := PathsOverlap([]string{"web/**", "api/**"}, []string{"docs/**", "api/v2/**"})
	if !ok || pa != "api/**" || pb != "api/v2/**" {
		t.Fatalf("got %q, %q, %v", pa, pb, ok)
	}
	if _, _, ok := PathsOverlap([]string{"a/**"}, []string{"b/**"}); ok {
		t.Fatal("want no overlap")
	}
}

func TestOutside(t *testing.T) {
	files := []string{"api/a.go", "README.md", "web/x.ts", "go.mod"}
	got := Outside([]string{"api/**", "README.md"}, files)
	if want := []string{"web/x.ts", "go.mod"}; !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := Outside(nil, files); got != nil {
		t.Fatalf("no patterns: got %v, want nil", got)
	}
}
