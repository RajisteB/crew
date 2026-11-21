package flock

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func lockPath(t *testing.T) string { return filepath.Join(t.TempDir(), "lock") }

func TestAcquire_ExcludesAndReleases(t *testing.T) {
	path := lockPath(t)
	unlock, err := Acquire(path, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if pid, held := Held(path); !held || pid != os.Getpid() {
		t.Fatalf("Held = %d %v", pid, held)
	}
	if _, err := Acquire(path, 0o644); !errors.Is(err, ErrHeld) {
		t.Fatalf("second Acquire = %v", err)
	}
	unlock()
	unlock() // releasing twice is harmless
	if _, held := Held(path); held {
		t.Fatal("still held after unlock")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("the lock file must stay: %v", err)
	}
	again, err := Acquire(path, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	again()
}

func TestHeld_LeftoverLivePidIsNotHeld(t *testing.T) {
	path := lockPath(t)
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, held := Held(path); held {
		t.Fatal("a pid in the file without a flock is not a holder")
	}
	unlock, err := Acquire(path, 0o644)
	if err != nil {
		t.Fatalf("a leftover file must not block: %v", err)
	}
	unlock()
}

func TestHeld_MissingOrEmptyFile(t *testing.T) {
	path := lockPath(t)
	if pid, held := Held(path); held || pid != 0 {
		t.Fatalf("missing file: %d %v", pid, held)
	}
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, held := Held(path); held || Pid(path) != 0 {
		t.Fatal("an empty file holds nothing")
	}
}
