// Package migrate moves an existing crewfile setup to crew's names:
// ~/crewfile → ~/crew, crewfile.yaml → crew.yaml, .crewfile/ → .crew/.
//
// The harness links (~/.claude/agents/x.md → <old dir>/build/...) are left
// alone on purpose: their state records still match what is on disk, so the
// next `crew compile` classifies them as updates and re-points them. Only the
// state keys and roots of files that lived inside the moved folder are
// rewritten; fingerprints are never touched.
package migrate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	OldConfig   = "crewfile.yaml"
	NewConfig   = "crew.yaml"
	OldStateDir = ".crewfile"
	NewStateDir = ".crew"
)

// Step kinds.
const (
	MoveDir      = "move-dir"
	RenameFile   = "rename-file"
	RewriteState = "rewrite-state"
)

// Step is one change Apply makes. For RewriteState, From and To are the old
// and new directory prefixes.
type Step struct {
	Kind     string
	From, To string
}

func (s Step) String() string {
	switch s.Kind {
	case RewriteState:
		return fmt.Sprintf("%-13s paths under %s → %s", s.Kind, s.From, s.To)
	default:
		return fmt.Sprintf("%-13s %s → %s", s.Kind, s.From, s.To)
	}
}

// Plan works out the steps without touching the disk.
func Plan(from, to string) ([]Step, error) {
	from, err := filepath.Abs(from)
	if err != nil {
		return nil, err
	}
	to, err = filepath.Abs(to)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(from, OldConfig)); err != nil {
		return nil, fmt.Errorf("%s has no %s; nothing to migrate", from, OldConfig)
	}
	if _, err := os.Lstat(to); err == nil {
		return nil, fmt.Errorf("%s already exists; move it aside first", to)
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	steps := []Step{
		{MoveDir, from, to},
		{RenameFile, filepath.Join(to, OldConfig), filepath.Join(to, NewConfig)},
	}
	if fi, err := os.Stat(filepath.Join(from, OldStateDir)); err == nil && fi.IsDir() {
		steps = append(steps,
			Step{RenameFile, filepath.Join(to, OldStateDir), filepath.Join(to, NewStateDir)},
			Step{RewriteState, from, to})
	}
	return steps, nil
}

// Apply runs the steps in order and stops at the first failure.
func Apply(steps []Step) error {
	for _, s := range steps {
		var err error
		switch s.Kind {
		case MoveDir, RenameFile:
			err = os.Rename(s.From, s.To)
		case RewriteState:
			err = rewriteAll(s.From, s.To)
		default:
			err = fmt.Errorf("unknown step %q", s.Kind)
		}
		if err != nil {
			return fmt.Errorf("%s: %w", s, err)
		}
	}
	return nil
}

func rewriteAll(from, to string) error {
	files, err := filepath.Glob(filepath.Join(to, NewStateDir, "state.*.json"))
	if err != nil {
		return err
	}
	for _, f := range files {
		if err := rewriteState(f, from, to); err != nil {
			return fmt.Errorf("%s: %w", f, err)
		}
	}
	return nil
}

// rewriteState keeps every field it doesn't know about by decoding entries
// as generic maps.
func rewriteState(path, from, to string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var st map[string]json.RawMessage
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	var files map[string]map[string]any
	if raw, ok := st["files"]; ok {
		if err := json.Unmarshal(raw, &files); err != nil {
			return err
		}
	}
	moved := make(map[string]map[string]any, len(files))
	for k, e := range files {
		ne := make(map[string]any, len(e))
		for f, v := range e {
			ne[f] = v
		}
		if r, ok := ne["root"].(string); ok {
			ne["root"] = swapPrefix(r, from, to)
		}
		moved[swapPrefix(k, from, to)] = ne
	}
	raw, err := json.Marshal(moved)
	if err != nil {
		return err
	}
	st["files"] = raw
	out, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, out)
}

func swapPrefix(p, from, to string) string {
	if p == from {
		return to
	}
	if strings.HasPrefix(p, from+string(filepath.Separator)) {
		return to + p[len(from):]
	}
	return p
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
