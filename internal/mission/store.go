package mission

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/flock"
	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/run"
)

const (
	fileMode = 0o600 // inbox items and state carry the principal's words
	dirMode  = 0o700
)

var missionIDPattern = regexp.MustCompile(`^m-[0-9]{8}-[0-9]{6}-[0-9a-f]{4}$`)

// Store is one mission's folder: <repo>/.crew/missions/<id>/.
type Store struct{ Dir string }

func missionsDir(repoRoot string) string {
	return filepath.Join(repoRoot, run.CrewDir, "missions")
}

func (s Store) path(name string) string { return filepath.Join(s.Dir, name) }

func validID(id string) error {
	if !missionIDPattern.MatchString(id) {
		return fmt.Errorf("invalid mission id %q", id)
	}
	return nil
}

func newID(now time.Time) (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("m-%s-%s", now.UTC().Format("20060102-150405"), hex.EncodeToString(b)), nil
}

// New creates the mission folder holding mission.md and a planned state.
func New(repoRoot string, m Mission, info gh.RepoInfo, now time.Time) (Store, State, error) {
	if err := run.EnsureCrewDir(repoRoot); err != nil {
		return Store{}, State{}, err
	}
	id, err := newID(now)
	if err != nil {
		return Store{}, State{}, err
	}
	s := Store{Dir: filepath.Join(missionsDir(repoRoot), id)}
	if err := os.MkdirAll(filepath.Join(s.Dir, "inbox"), dirMode); err != nil {
		return Store{}, State{}, err
	}
	if err := s.SaveMission(m); err != nil {
		return Store{}, State{}, err
	}
	st := NewState(id, m, info)
	return s, st, s.SaveState(st)
}

// Open returns the store of an existing mission.
func Open(repoRoot, id string) (Store, error) {
	if err := validID(id); err != nil {
		return Store{}, err
	}
	s := Store{Dir: filepath.Join(missionsDir(repoRoot), id)}
	if _, err := os.Stat(s.path("state.json")); err != nil {
		return Store{}, fmt.Errorf("no mission %q in %s", id, missionsDir(repoRoot))
	}
	return s, nil
}

// List returns every readable mission's state, newest first. A folder whose
// state can't be read is skipped.
func List(repoRoot string) ([]State, error) {
	entries, err := os.ReadDir(missionsDir(repoRoot))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []State
	for _, e := range entries {
		s, err := Open(repoRoot, e.Name())
		if err != nil {
			continue
		}
		if st, err := s.LoadState(); err == nil {
			out = append(out, st)
		}
	}
	slices.SortFunc(out, func(a, b State) int { return strings.Compare(b.MissionID, a.MissionID) })
	return out, nil
}

// Mission reads mission.md.
func (s Store) Mission() (Mission, error) {
	b, err := os.ReadFile(s.path("mission.md"))
	if err != nil {
		return Mission{}, err
	}
	return Parse(b)
}

// SaveMission replaces mission.md.
func (s Store) SaveMission(m Mission) error {
	return run.WriteAtomic(s.path("mission.md"), m.Render())
}

// SaveApproved copies mission.md to approved.md. Nothing else writes it.
func (s Store) SaveApproved() error {
	b, err := os.ReadFile(s.path("mission.md"))
	if err != nil {
		return err
	}
	return run.WriteAtomic(s.path("approved.md"), b)
}

func (s Store) LoadState() (State, error) {
	b, err := os.ReadFile(s.path("state.json"))
	if err != nil {
		return State{}, err
	}
	var st State
	if err := json.Unmarshal(b, &st); err != nil {
		return State{}, fmt.Errorf("%s: %w", s.path("state.json"), err)
	}
	return st, nil
}

func (s Store) SaveState(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return run.WriteAtomic(s.path("state.json"), b)
}

// ---------- lock: one writer per mission ----------

// Lock makes this process the mission's only writer by holding an flock on
// the lock file until unlock is called or the process dies, so a crashed
// driver never leaves a lock behind. The file itself is never removed, so no
// process can lock a stale inode.
func (s Store) Lock() (func(), error) {
	unlock, err := flock.Acquire(s.path("lock"), fileMode)
	if errors.Is(err, flock.ErrHeld) {
		holder := "pid unknown"
		if pid := flock.Pid(s.path("lock")); pid > 0 {
			holder = fmt.Sprintf("pid %d", pid)
		}
		return nil, fmt.Errorf("mission %s is already being driven (%s)", filepath.Base(s.Dir), holder)
	}
	return unlock, err
}

// LockedBy reports the pid in the lock file and whether a driver is alive.
// If nobody holds the flock, the pid left in the file is not a live driver
// (the process crashed, or its pid was reused).
func (s Store) LockedBy() (int, bool) { return flock.Held(s.path("lock")) }

// ---------- inbox: requests from other commands ----------

// Item is one inbox file. Kind is "answer" or "amend".
type Item struct {
	Name, Kind string
	Body       json.RawMessage
}

var inboxKinds = []string{"answer", "amend"}

// Drop writes a request into the inbox, whole or not at all.
func (s Store) Drop(kind string, v any) error {
	if !slices.Contains(inboxKinds, kind) {
		return fmt.Errorf("unknown inbox kind %q", kind)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dir := s.path("inbox")
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return err
	}
	name := fmt.Sprintf("%d-%s.json", time.Now().UnixNano(), kind)
	return run.WriteAtomic(filepath.Join(dir, name), b)
}

// Inbox returns the waiting items, oldest first. Nothing is deleted.
func (s Store) Inbox() ([]Item, error) {
	entries, err := os.ReadDir(s.path("inbox"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Item
	for _, e := range entries {
		name := e.Name()
		stem, ok := strings.CutSuffix(name, ".json")
		if !ok || strings.HasPrefix(name, ".") {
			continue
		}
		_, kind, ok := strings.Cut(stem, "-")
		if !ok {
			continue
		}
		b, err := os.ReadFile(filepath.Join(s.path("inbox"), name))
		if err != nil {
			return nil, err
		}
		out = append(out, Item{Name: name, Kind: kind, Body: b})
	}
	slices.SortFunc(out, func(a, b Item) int { return compareItems(a.Name, b.Name) })
	return out, nil
}

// compareItems orders by the numeric timestamp prefix, so names of
// different digit counts still sort by time.
func compareItems(a, b string) int {
	na, _, _ := strings.Cut(a, "-")
	nb, _, _ := strings.Cut(b, "-")
	ia, _ := strconv.ParseInt(na, 10, 64)
	ib, _ := strconv.ParseInt(nb, 10, 64)
	if ia != ib {
		if ia < ib {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// Ack deletes the given items.
func (s Store) Ack(items []Item) error {
	for _, it := range items {
		if it.Name != filepath.Base(it.Name) {
			return fmt.Errorf("invalid inbox item %q", it.Name)
		}
		if err := os.Remove(filepath.Join(s.path("inbox"), it.Name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}
