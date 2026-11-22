package principal

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var switchTime = time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)

func sw(repo, domain string, mode AutoMode) SwitchRecord {
	return SwitchRecord{Repo: repo, Domain: domain, Mode: mode, SetAt: switchTime}
}

func TestSwitches_MissingFileIsEmpty(t *testing.T) {
	m, err := newStore(t).Switches("a/b")
	if err != nil || len(m) != 0 {
		t.Fatalf("m=%v err=%v", m, err)
	}
	if got := ModeOf(m, "product"); got != AutoOff {
		t.Fatalf("ModeOf = %q, want off", got)
	}
}

func TestSetSwitches_RoundTrip(t *testing.T) {
	s := newStore(t)
	if err := s.SetSwitches([]SwitchRecord{sw("a/b", "product", AutoOn), sw("a/b", "design", AutoWithoutChecks)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSwitches([]SwitchRecord{sw("c/d", "product", AutoOn)}); err != nil {
		t.Fatal(err)
	}
	ab, err := s.Switches("a/b")
	if err != nil || len(ab) != 2 {
		t.Fatalf("a/b = %v err=%v", ab, err)
	}
	if ModeOf(ab, "product") != AutoOn || ModeOf(ab, "design") != AutoWithoutChecks {
		t.Fatalf("a/b modes = %v", ab)
	}
	cd, err := s.Switches("c/d")
	if err != nil || len(cd) != 1 || ModeOf(cd, "product") != AutoOn {
		t.Fatalf("c/d = %v err=%v", cd, err)
	}
}

func TestSetSwitches_ReplacesOneKeepsOthers(t *testing.T) {
	s := newStore(t)
	if err := s.SetSwitches([]SwitchRecord{sw("a/b", "design", AutoOn), sw("a/b", "product", AutoOn)}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSwitches([]SwitchRecord{sw("a/b", "product", AutoOff)}); err != nil {
		t.Fatal(err)
	}
	m, err := s.Switches("a/b")
	if err != nil {
		t.Fatal(err)
	}
	if ModeOf(m, "product") != AutoOff {
		t.Fatalf("product = %q", ModeOf(m, "product"))
	}
	if m["design"] != sw("a/b", "design", AutoOn) {
		t.Fatalf("design changed: %v", m["design"])
	}
}

func TestSwitches_TamperedIsAnError(t *testing.T) {
	setup := func(t *testing.T) (Store, string) {
		s := newStore(t)
		if err := s.SetSwitches([]SwitchRecord{sw("a/b", "product", AutoOff)}); err != nil {
			t.Fatal(err)
		}
		return s, filepath.Join(s.Dir, "automerge.json")
	}
	check := func(t *testing.T, s Store) {
		t.Helper()
		_, err := s.Switches("a/b")
		if !errors.Is(err, ErrTampered) || !strings.Contains(err.Error(), "automerge.json") {
			t.Fatalf("err = %v, want ErrTampered naming automerge.json", err)
		}
	}
	t.Run("edited", func(t *testing.T) {
		s, path := setup(t)
		data, _ := os.ReadFile(path)
		if err := os.WriteFile(path, []byte(strings.Replace(string(data), `"off"`, `"on"`, 1)), 0o600); err != nil {
			t.Fatal(err)
		}
		check(t, s)
	})
	t.Run("sum deleted", func(t *testing.T) {
		s, path := setup(t)
		if err := os.Remove(path + ".sum"); err != nil {
			t.Fatal(err)
		}
		check(t, s)
	})
	t.Run("unknown mode", func(t *testing.T) {
		s, path := setup(t)
		data := []byte(`[{"repo":"a/b","domain":"product","mode":"always","set_at":"2026-11-21T09:00:00Z"}]`)
		if err := writeSealed(path, path+".sum", data); err != nil {
			t.Fatal(err)
		}
		check(t, s)
	})
}

func TestSetSwitches_FileModes(t *testing.T) {
	s := newStore(t)
	if err := s.SetSwitches([]SwitchRecord{sw("a/b", "product", AutoOn)}); err != nil {
		t.Fatal(err)
	}
	assertMode(t, filepath.Join(s.Dir, "automerge.json"), 0o600)
	assertMode(t, filepath.Join(s.Dir, "automerge.json.sum"), 0o600)
}

func TestSetSwitches_TamperedFileRefused(t *testing.T) {
	s := newStore(t)
	if err := s.SetSwitches([]SwitchRecord{sw("a/b", "product", AutoOff)}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "automerge.json")
	data, _ := os.ReadFile(path)
	edited := []byte(strings.Replace(string(data), `"off"`, `"on"`, 1))
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	err := s.SetSwitches([]SwitchRecord{sw("a/b", "design", AutoOn)})
	if !errors.Is(err, ErrTampered) {
		t.Fatalf("err = %v, want ErrTampered", err)
	}
	if after, _ := os.ReadFile(path); string(after) != string(edited) {
		t.Fatalf("file changed:\n%s", after)
	}
}
