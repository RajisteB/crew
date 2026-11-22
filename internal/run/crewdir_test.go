package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCrewDir_CreatesAndIsIdempotent(t *testing.T) {
	p := t.TempDir()
	for range 2 {
		if err := EnsureCrewDir(p); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(p, CrewDir, ".gitignore"))
	if err != nil || string(b) != "*\n" {
		t.Fatalf("gitignore %q %v", b, err)
	}
}

func TestEnsureCrewDir_KeepsAnExistingGitignore(t *testing.T) {
	p := t.TempDir()
	gi := filepath.Join(p, CrewDir, ".gitignore")
	if err := os.MkdirAll(filepath.Dir(gi), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gi, []byte("mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsureCrewDir(p); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(gi); string(b) != "mine\n" {
		t.Fatalf("gitignore %q", b)
	}
}

func TestEnsureCrewDir_RefusesALinkedCrewDir(t *testing.T) {
	p, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(p, CrewDir)); err != nil {
		t.Fatal(err)
	}
	err := EnsureCrewDir(p)
	if err == nil || !strings.Contains(err.Error(), ".crew is not a plain folder") {
		t.Fatalf("err = %v", err)
	}
	if ents, _ := os.ReadDir(outside); len(ents) != 0 {
		t.Fatalf("written through the link: %v", ents)
	}
}

func TestEnsureCrewDir_RefusesALinkedGitignore(t *testing.T) {
	p := t.TempDir()
	target := filepath.Join(t.TempDir(), "x")
	if err := os.MkdirAll(filepath.Join(p, CrewDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(p, CrewDir, ".gitignore")); err != nil {
		t.Fatal(err)
	}
	err := EnsureCrewDir(p)
	if err == nil || !strings.Contains(err.Error(), ".crew/.gitignore is not a regular file") {
		t.Fatalf("err = %v", err)
	}
	if _, statErr := os.Lstat(target); !os.IsNotExist(statErr) {
		t.Fatalf("the link target was created: %v", statErr)
	}
}
