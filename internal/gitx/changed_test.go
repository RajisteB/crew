package gitx

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestChangedFiles_MovedFileListsBothPaths(t *testing.T) {
	repo, _ := newRepo(t)
	commitFile(t, repo.Dir, "brand/logo.svg", "<svg>some longer logo content</svg>\n")
	run(t, repo.Dir, "checkout", "-b", "piece")
	if err := os.MkdirAll(filepath.Join(repo.Dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, repo.Dir, "mv", "brand/logo.svg", "app/logo.svg")
	run(t, repo.Dir, "add", "-A")
	run(t, repo.Dir, "commit", "-m", "move")
	got, err := repo.ChangedFiles(ctx, repo.Dir, "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"brand/logo.svg", "app/logo.svg"} {
		if !slices.Contains(got, want) {
			t.Fatalf("ChangedFiles = %v, missing %s", got, want)
		}
	}
}
