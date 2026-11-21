package principal

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

type fakeTTY struct {
	in  *strings.Reader
	out bytes.Buffer
}

func newTTY(input string) *fakeTTY { return &fakeTTY{in: strings.NewReader(input)} }

func (f *fakeTTY) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeTTY) Write(p []byte) (int, error) { return f.out.Write(p) }

func newStore(t *testing.T) Store { return Store{Dir: filepath.Join(t.TempDir(), "principal")} }

func record(visibility string) RepoRecord {
	return RepoRecord{Repo: "acme/app", Visibility: visibility, ConfirmedAt: time.Date(2026, 11, 21, 9, 0, 0, 0, time.UTC)}
}

func TestStore_ConfirmThenRead(t *testing.T) {
	s := newStore(t)
	if _, ok, err := s.Repo("acme/app"); ok || err != nil {
		t.Fatalf("empty store: ok=%v err=%v", ok, err)
	}
	if err := s.ConfirmRepo(record("private")); err != nil {
		t.Fatal(err)
	}
	if err := s.ConfirmRepo(record("public")); err != nil {
		t.Fatal(err)
	}
	other := record("private")
	other.Repo = "acme/other"
	if err := s.ConfirmRepo(other); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Repo("acme/app")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.Visibility != "public" || !got.ConfirmedAt.Equal(record("").ConfirmedAt) {
		t.Fatalf("got %+v, want the replaced public record", got)
	}
	if _, ok, _ := s.Repo("acme/other"); !ok {
		t.Fatal("other repo's record lost")
	}
	assertMode(t, s.Dir, 0o700)
	assertMode(t, filepath.Join(s.Dir, "repos.json"), 0o600)
	assertMode(t, filepath.Join(s.Dir, "repos.json.sum"), 0o600)
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != want {
		t.Fatalf("%s mode %o want %o", path, got, want)
	}
}

func TestStore_TamperedReadsAsUnconfirmed(t *testing.T) {
	s := newStore(t)
	if err := s.ConfirmRepo(record("private")); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(s.Dir, "repos.json")
	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), "private", "public ", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Repo("acme/app"); !errors.Is(err, ErrTampered) {
		t.Fatalf("Repo err = %v, want ErrTampered", err)
	}
	if err := s.ConfirmRepo(record("private")); !errors.Is(err, ErrTampered) {
		t.Fatalf("ConfirmRepo err = %v, want ErrTampered", err)
	}
	err := CheckRepo(s, gh.RepoInfo{Slug: "acme/app", Visibility: "private"})
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "crew repo confirm") {
		t.Fatalf("CheckRepo err = %v, want it to name %s", err, path)
	}
	os.Remove(path + ".sum")
	if _, _, err := s.Repo("acme/app"); !errors.Is(err, ErrTampered) {
		t.Fatalf("missing sum: err = %v, want ErrTampered", err)
	}
}

func TestConfirm_WrongTextFails(t *testing.T) {
	for _, in := range []string{"acme/other\n", "yes\n", ""} {
		tty := newTTY(in)
		if err := Confirm(tty, "Type the repo:", "acme/app"); err == nil {
			t.Fatalf("input %q: want error", in)
		}
		if !strings.Contains(tty.out.String(), "Type the repo:") {
			t.Fatalf("question not printed: %q", tty.out.String())
		}
	}
}

func TestConfirm_ExactTextPasses(t *testing.T) {
	if err := Confirm(newTTY("  acme/app \n"), "Type the repo:", "acme/app"); err != nil {
		t.Fatal(err)
	}
	if err := Confirm(newTTY("acme/app"), "Type the repo:", "acme/app"); err != nil {
		t.Fatalf("no trailing newline: %v", err)
	}
}

func TestCheckRepo_VisibilityChanged(t *testing.T) {
	s := newStore(t)
	info := gh.RepoInfo{Slug: "acme/app", Visibility: "private"}
	if err := CheckRepo(s, info); err == nil || err.Error() != "this repo isn't confirmed; run crew repo confirm in a terminal" {
		t.Fatalf("unconfirmed: %v", err)
	}
	if err := s.ConfirmRepo(record("private")); err != nil {
		t.Fatal(err)
	}
	if err := CheckRepo(s, info); err != nil {
		t.Fatalf("confirmed: %v", err)
	}
	info.Visibility = "public"
	want := "this repo was confirmed as private and is now public; run crew repo confirm again"
	if err := CheckRepo(s, info); err == nil || err.Error() != want {
		t.Fatalf("got %v want %q", err, want)
	}
}
