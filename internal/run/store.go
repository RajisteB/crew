package run

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/crewfile-dev/crew/internal/source"
)

// CrewDir is the per-project folder crew keeps runs in.
const CrewDir = ".crew"

// Store is one run's folder: <project>/.crew/runs/<run-id>/.
type Store struct{ Dir string }

func runsDir(project string) string { return filepath.Join(project, CrewDir, "runs") }

func (s Store) path(name string) string { return filepath.Join(s.Dir, name) }

// NewRun creates the run folder, freezing a copy of the factory source so
// edits to the factory can't change a run already in progress.
func NewRun(project string, f source.Factory, factorySrc []byte, input string, now time.Time) (Store, State, error) {
	if err := ensureCrewDir(project); err != nil {
		return Store{}, State{}, err
	}
	id, err := newRunID(f.Name, now)
	if err != nil {
		return Store{}, State{}, err
	}
	s := Store{Dir: filepath.Join(runsDir(project), id)}
	if err := os.MkdirAll(filepath.Join(s.Dir, "steps"), 0o755); err != nil {
		return Store{}, State{}, err
	}
	for name, b := range map[string][]byte{"input.md": []byte(input), "factory.md": factorySrc} {
		if err := os.WriteFile(s.path(name), b, 0o644); err != nil {
			return Store{}, State{}, err
		}
	}
	st := Begin(f, id)
	return s, st, s.SaveState(st)
}

// ensureCrewDir creates <project>/.crew with a .gitignore that ignores it, so
// runs never end up in the project's history and its .gitignore is untouched.
func ensureCrewDir(project string) error {
	dir := filepath.Join(project, CrewDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	gi := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gi); errors.Is(err, os.ErrNotExist) {
		return os.WriteFile(gi, []byte("*\n"), 0o644)
	}
	return nil
}

func newRunID(factory string, now time.Time) (string, error) {
	b := make([]byte, 2)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%s-%s", factory, now.Format("20060102-150405"), hex.EncodeToString(b)), nil
}

func OpenRun(project, runID string) (Store, error) {
	if runID == "" || strings.ContainsAny(runID, `/\`) || strings.HasPrefix(runID, ".") {
		return Store{}, fmt.Errorf("invalid run id %q", runID)
	}
	s := Store{Dir: filepath.Join(runsDir(project), runID)}
	if _, err := os.Stat(s.path("state.json")); err != nil {
		return Store{}, fmt.Errorf("no run %q in %s", runID, runsDir(project))
	}
	return s, nil
}

// ListRuns returns every run's state, newest first.
func ListRuns(project string) ([]State, error) {
	entries, err := os.ReadDir(runsDir(project))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []State
	for _, e := range entries {
		if st, err := (Store{Dir: filepath.Join(runsDir(project), e.Name())}).LoadState(); err == nil {
			out = append(out, st)
		}
	}
	sort.Slice(out, func(i, j int) bool { return idTime(out[i].RunID) > idTime(out[j].RunID) })
	return out, nil
}

// idTime is the "YYYYMMDD-HHMMSS-hex" tail of a run id, which sorts by time.
func idTime(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		return id
	}
	return strings.Join(parts[len(parts)-3:], "-")
}

func (s Store) Factory() (source.Factory, error) { return source.ParseFactory(s.path("factory.md")) }

func (s Store) Input() (string, error) {
	b, err := os.ReadFile(s.path("input.md"))
	return string(b), err
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
	if st.RouteCounts == nil {
		st.RouteCounts = map[string]int{}
	}
	return st, nil
}

func (s Store) SaveState(st State) error {
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.path("state.json"), b)
}

func stepDir(s Store, n int, station string) string {
	return filepath.Join(s.Dir, "steps", fmt.Sprintf("%d-%s", n, station))
}

func (s Store) WriteStep(rec StepRecord, prompt, output string) error {
	dir := stepDir(s, rec.N, rec.Station)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	vb, err := json.MarshalIndent(rec.Verdict, "", "  ")
	if err != nil {
		return err
	}
	for name, b := range map[string][]byte{"prompt.md": []byte(prompt), "output.md": []byte(output), "verdict.json": vb} {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// Steps lists finished steps in order. A step folder without a verdict (a
// station that was interrupted) is skipped.
func (s Store) Steps() ([]StepRecord, error) {
	entries, err := os.ReadDir(filepath.Join(s.Dir, "steps"))
	if err != nil {
		return nil, err
	}
	var out []StepRecord
	for _, e := range entries {
		num, station, ok := strings.Cut(e.Name(), "-")
		n, err := strconv.Atoi(num)
		if !ok || err != nil {
			continue
		}
		dir := filepath.Join(s.Dir, "steps", e.Name())
		b, err := os.ReadFile(filepath.Join(dir, "verdict.json"))
		if err != nil {
			continue
		}
		rec := StepRecord{N: n, Station: station, Dir: dir}
		if err := json.Unmarshal(b, &rec.Verdict); err != nil {
			return nil, fmt.Errorf("%s: %w", dir, err)
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out, nil
}

// ---------- inbox and goto requests (written by other processes) ----------

func (s Store) AppendInbox(msg string) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.path("inbox.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(b, '\n')); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// DrainInbox returns undelivered messages and clears them. The file is
// renamed first, so a message appended meanwhile lands in a fresh inbox.
func (s Store) DrainInbox() ([]string, error) {
	taken := s.path("inbox.taking")
	if err := os.Rename(s.path("inbox.jsonl"), taken); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	defer os.Remove(taken)
	f, err := os.Open(taken)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	for sc.Scan() {
		var m string
		if err := json.Unmarshal(sc.Bytes(), &m); err == nil {
			out = append(out, m)
		}
	}
	return out, sc.Err()
}

func (s Store) RequestGoto(station string) error {
	return writeAtomic(s.path("goto"), []byte(station))
}

func (s Store) TakeGoto() (string, error) {
	b, err := os.ReadFile(s.path("goto"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), os.Remove(s.path("goto"))
}

// ---------- lock: one engine per run ----------

// Lock makes this process the run's only driver. A lock left by a process
// that no longer exists is taken over.
func (s Store) Lock() (func(), error) {
	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(s.path("lock"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			fmt.Fprint(f, os.Getpid())
			f.Close()
			return func() { os.Remove(s.path("lock")) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if pid, alive := s.LockedBy(); alive {
			return nil, fmt.Errorf("run %s is already being driven (pid %d)", filepath.Base(s.Dir), pid)
		}
		os.Remove(s.path("lock"))
	}
	return nil, fmt.Errorf("run %s: couldn't take the lock", filepath.Base(s.Dir))
}

// LockedBy reports the pid driving the run, if that process is alive.
func (s Store) LockedBy() (int, bool) {
	b, err := os.ReadFile(s.path("lock"))
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || pid <= 0 {
		return 0, false
	}
	return pid, syscall.Kill(pid, 0) == nil
}

func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
