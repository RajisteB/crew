package source_test

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func TestLoadDomains_RejectsBadPaths(t *testing.T) {
	bad := map[string]string{
		"/brand/**": "must be relative to the repo root (no leading /)",
		"a//b":      "has an empty segment",
		"../x":      "must not contain ..",
		"[":         "is not a valid pattern",
		"":          "is empty",
	}
	for pat, why := range bad {
		t.Run("bad "+pat, func(t *testing.T) {
			root := writeDomain(t, "d.md", "---\nname: d\npaths: [\""+pat+"\"]\n---\nBody\n")
			_, errs := source.LoadDomains(root)
			want := `paths: "` + pat + `": ` + why
			if len(errs) != 1 || !strings.Contains(errs[0].Error(), want) || !strings.HasPrefix(errs[0].Error(), "domains/d.md: ") {
				t.Fatalf("errs = %v, want one containing %q", errs, want)
			}
		})
	}
	for _, pat := range []string{"brand/**", "**/*.md", "brand/", "./brand"} {
		t.Run("good "+pat, func(t *testing.T) {
			root := writeDomain(t, "d.md", "---\nname: d\npaths: [\""+pat+"\"]\n---\nBody\n")
			ds, errs := source.LoadDomains(root)
			if len(errs) != 0 || len(ds) != 1 {
				t.Fatalf("errs = %v domains = %v", errs, ds)
			}
		})
	}
}
