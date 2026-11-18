package build

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// ---------- actions and ownership records ----------

type ActionKind string

const (
	Create    ActionKind = "create"
	Update    ActionKind = "update"
	Unchanged ActionKind = "same"
	Delete    ActionKind = "delete"
	Conflict  ActionKind = "CONFLICT"
	// Forget drops a record without touching the disk: the file is already
	// gone, or it now sits under a link and deleting it would reach the source.
	Forget ActionKind = "forget"
)

type Action struct {
	Kind   ActionKind
	Path   string
	Reason string
	File   *File // nil for deletes and forgets
}

type stateFile struct {
	Version int                   `json:"version"`
	Files   map[string]stateEntry `json:"files"`
}

type stateEntry struct {
	SHA256    string   `json:"sha256"` // content hash, or "link:<target>"
	Kind      string   `json:"kind"`
	Harnesses []string `json:"harnesses"`
	Root      string   `json:"root,omitempty"` // folder the output lives under (a harness's skills/ or agents/)
}

// StatePath keys ownership records by where output goes, so user-scope,
// each project and each --out staging dir are tracked independently.
func StatePath(dir string, o Opts) string {
	key := "user"
	if o.Scope == "project" {
		abs, _ := filepath.Abs(o.ProjectDir)
		key = "project-" + short(abs)
	}
	if o.Out != "" {
		abs, _ := filepath.Abs(o.Out)
		key += "-out-" + short(abs)
	}
	return filepath.Join(dir, ".crewfile", "state."+key+".json")
}

func short(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:4])
}

func loadState(path string) (*stateFile, error) {
	st := &stateFile{Version: 1, Files: map[string]stateEntry{}}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, st); err != nil {
		return nil, fmt.Errorf("%s is corrupt: %w", path, err)
	}
	if st.Files == nil {
		st.Files = map[string]stateEntry{}
	}
	return st, nil
}

func saveState(path string, st *stateFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'))
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

// ---------- what is on disk ----------

// onDisk is what currently sits at an output path. inspect never follows a
// link at the path itself.
type onDisk struct {
	exists, isDir bool
	fingerprint   string // same form as File.want()
}

func inspect(path string) (onDisk, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return onDisk{}, nil
	}
	if err != nil {
		return onDisk{}, err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		t, err := os.Readlink(path)
		return onDisk{exists: true, fingerprint: "link:" + t}, err
	case fi.IsDir():
		return onDisk{exists: true, isDir: true}, nil
	}
	b, err := os.ReadFile(path)
	return onDisk{exists: true, fingerprint: hashOf(b)}, err
}

// maxDepth bounds every upward walk, so a bad root can never send one far.
const maxDepth = 8

// linkedAncestor returns the first folder between root (exclusive) and path
// that is a symlink, or "". Writing or deleting below such a folder would
// reach whatever it points at, for example the crewfile source itself. root
// may itself be a link (a symlinked ~/.claude is fine). Records from before
// roots were tracked have root == ""; for those the walk stops at a folder
// named skills or agents.
func linkedAncestor(root, path string) string {
	dir := filepath.Dir(path)
	for i := 0; i < maxDepth; i++ {
		if dir == root || dir == filepath.Dir(dir) {
			return ""
		}
		if root == "" && isHarnessRoot(dir) {
			return ""
		}
		if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	return ""
}

func isHarnessRoot(dir string) bool {
	b := filepath.Base(dir)
	return b == "skills" || b == "agents"
}
