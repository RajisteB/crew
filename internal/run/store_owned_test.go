package run_test

import (
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/run"
)

func TestNewOwnedRun_SavesTheOwnerInTheFirstWrite(t *testing.T) {
	project := t.TempDir()
	store, st, err := run.NewOwnedRun(project, shipChange(), []byte("src"), "do it", "mission m-1 piece P1", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if st.Owner != "mission m-1 piece P1" {
		t.Fatalf("returned owner = %q", st.Owner)
	}
	saved, err := store.LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Owner != st.Owner || saved.RunID != st.RunID || saved.Status != st.Status {
		t.Fatalf("saved state %+v differs from returned %+v", saved, st)
	}
}

func TestNewRun_HasNoOwner(t *testing.T) {
	store, st, err := run.NewRun(t.TempDir(), shipChange(), []byte("src"), "do it", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := store.LoadState()
	if err != nil || st.Owner != "" || saved.Owner != "" {
		t.Fatalf("owner %q / saved %q (%v), want none", st.Owner, saved.Owner, err)
	}
}
