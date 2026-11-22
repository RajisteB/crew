package mission

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// hardLink links a file outside dir into dir as name: what a station would
// do with a key it can read.
func hardLink(t *testing.T, dir, name string) {
	t.Helper()
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	writeFile(t, secret, "private key")
	if err := os.Link(secret, filepath.Join(dir, name)); err != nil {
		t.Skip("no hard links:", err)
	}
}

func TestListDelivered_SkipsHardLinks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "real.md"), "r")
	hardLink(t, dir, "key")
	writeFile(t, filepath.Join(dir, "sub", "x.md"), "x")
	hardLink(t, filepath.Join(dir, "sub"), "key2")
	f, err := listDelivered(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"real.md", "sub/x.md"}; !slices.Equal(f.Files, want) {
		t.Fatalf("files = %v, want %v", f.Files, want)
	}
}

func TestCopyFiles_RefusesAHardLinkedSource(t *testing.T) {
	from, to := t.TempDir(), filepath.Join(t.TempDir(), "out")
	hardLink(t, from, "key")
	err := copyFiles(from, to, []string{"key"})
	if err == nil || !strings.Contains(err.Error(), "hard link") {
		t.Fatalf("want a hard-link error, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(to, "key")); err == nil {
		t.Fatal("the hard-linked file was copied")
	}
}
