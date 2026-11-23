package mission

import "testing"

func TestPatternWithin(t *testing.T) {
	yes := [][2]string{
		{"brand/logos/**", "brand/**"}, {"brand", "brand/**"}, {"brand/**", "brand"},
		{"brand/*.svg", "brand/**"}, {"brand/*.svg", "brand/*"}, {"brand/**/x", "brand/**"},
		{"a/*.md", "a/*.md"}, {"x", "**"},
	}
	no := [][2]string{
		{"site/**", "brand/**"}, {"**", "brand/**"}, {"brand/**", "brand/**/x"}, {"a/b", "a/*"},
		{"a/b.md", "a/*.md"}, // a bare name also stands for what is under it
		{"brand/a*.svg", "brand/*.svg"}, {"brand/[", "brand/**"}, {"", "brand/**"},
		{`brand/a\*`, "brand/*x"},
	}
	for _, c := range yes {
		if !PatternWithin(c[0], c[1]) {
			t.Errorf("PatternWithin(%q, %q) = false, want true", c[0], c[1])
		}
	}
	for _, c := range no {
		if PatternWithin(c[0], c[1]) {
			t.Errorf("PatternWithin(%q, %q) = true, want false", c[0], c[1])
		}
	}
}

func TestPathsWithin(t *testing.T) {
	if out, ok := PathsWithin([]string{"brand/a/**", "brand/b/**"}, []string{"brand/**"}); !ok || out != "" {
		t.Errorf("got %q %v", out, ok)
	}
	if out, ok := PathsWithin([]string{"brand/a/**", "site/**"}, []string{"brand/**"}); ok || out != "site/**" {
		t.Errorf("got %q %v", out, ok)
	}
	if out, ok := PathsWithin([]string{"anything/**"}, nil); !ok || out != "" {
		t.Errorf("empty parent: got %q %v", out, ok)
	}
}
