package mission

import "testing"

func TestMatchPath(t *testing.T) {
	cases := []struct {
		pattern, file string
		want          bool
	}{
		{"brand/**", "brand/a.svg", true},
		{"brand/**", "brand/x/y.svg", true},
		{"brand/**", "brandy/a.svg", false},
		{"**/*.md", "README.md", true},
		{"**/*.md", "docs/a/b.md", true},
		{"**/*.md", "docs/a/b.txt", false},
		{"app/*/main.go", "app/api/main.go", true},
		{"app/*/main.go", "app/api/v2/main.go", false},
		{"brand", "brand/a.svg", true},
		{"brand", "brand", true},
		{"brand", "brandy/a.svg", false},
		{"a/**/z", "a/z", true},
		{"a/**/z", "a/b/c/z", true},
		{"a/**/z", "a/b/c/y", false},
		{"[", "[", false},
		{"[", "a", false},
		{"./brand/", "brand/a.svg", true},
		{"brand/**", "./brand/a.svg", true},
		{"", "a", false},
	}
	for _, c := range cases {
		if got := MatchPath(c.pattern, c.file); got != c.want {
			t.Errorf("MatchPath(%q, %q) = %v, want %v", c.pattern, c.file, got, c.want)
		}
	}
}
