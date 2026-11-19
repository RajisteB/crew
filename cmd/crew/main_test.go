package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveDir_CrewHome(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CREW_HOME", tmp)
	got, err := resolveDir("", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != tmp {
		t.Fatalf("resolveDir with CREW_HOME = %q, want %q", got, tmp)
	}
}

func TestResolveDir_DefaultIsHomeCrew(t *testing.T) {
	t.Setenv("CREW_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	cwd := t.TempDir() // no crew.yaml here
	old, _ := os.Getwd()
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(old) })

	got, err := resolveDir("", false)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "crew"); got != want {
		t.Fatalf("resolveDir default = %q, want %q", got, want)
	}
}

func TestResolveDir_CwdWithCrewYAML(t *testing.T) {
	t.Setenv("CREW_HOME", "")
	cwd := t.TempDir()
	os.WriteFile(filepath.Join(cwd, "crew.yaml"), []byte("version: 1\n"), 0o644)
	old, _ := os.Getwd()
	os.Chdir(cwd)
	t.Cleanup(func() { os.Chdir(old) })

	got, err := resolveDir("", false)
	if err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(cwd)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != real {
		t.Fatalf("resolveDir in a dir with crew.yaml = %q, want %q", got, cwd)
	}
}
