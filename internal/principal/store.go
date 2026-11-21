// Package principal holds decisions only the human owner may make, recorded
// where an agent session cannot forge them without the tamper check noticing.
package principal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
)

const (
	reposFile = "repos.json"
	sumFile   = "repos.json.sum"
	dirMode   = 0o700
	fileMode  = 0o600
)

// ErrTampered means repos.json exists but does not match its checksum.
var ErrTampered = errors.New("repos.json does not match its checksum")

// RepoRecord is one repository the principal confirmed.
type RepoRecord struct {
	Repo        string    `json:"repo"` // "owner/name"
	Visibility  string    `json:"visibility"`
	ConfirmedAt time.Time `json:"confirmed_at"`
}

// Store keeps principal decisions in Dir (<crewDir>/principal).
type Store struct{ Dir string }

func (s Store) reposPath() string { return filepath.Join(s.Dir, reposFile) }

func checksum(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// load returns every record keyed by slug. A missing repos.json is an empty set.
func (s Store) load() (map[string]RepoRecord, error) {
	data, err := os.ReadFile(s.reposPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[string]RepoRecord{}, nil
	}
	if err != nil {
		return nil, err
	}
	sum, err := os.ReadFile(filepath.Join(s.Dir, sumFile))
	if err != nil || strings.TrimSpace(string(sum)) != checksum(data) {
		return nil, ErrTampered
	}
	var recs []RepoRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("%s: %w", s.reposPath(), err)
	}
	out := make(map[string]RepoRecord, len(recs))
	for _, r := range recs {
		out[r.Repo] = r
	}
	return out, nil
}

// Repo returns the confirmation for slug. ErrTampered when the store was edited.
func (s Store) Repo(slug string) (RepoRecord, bool, error) {
	recs, err := s.load()
	if err != nil {
		return RepoRecord{}, false, err
	}
	rec, ok := recs[slug]
	return rec, ok, nil
}

// ConfirmRepo records rec, replacing any earlier record for the same repo.
func (s Store) ConfirmRepo(rec RepoRecord) error {
	recs, err := s.load()
	if err != nil {
		return err
	}
	recs[rec.Repo] = rec
	list := make([]RepoRecord, 0, len(recs))
	for _, r := range recs {
		list = append(list, r)
	}
	sortRecords(list)
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.Dir, dirMode); err != nil {
		return err
	}
	if err := writeAtomic(s.reposPath(), data); err != nil {
		return err
	}
	return writeAtomic(filepath.Join(s.Dir, sumFile), []byte(checksum(data)+"\n"))
}

func sortRecords(list []RepoRecord) {
	slices.SortFunc(list, func(a, b RepoRecord) int { return strings.Compare(a.Repo, b.Repo) })
}

// writeAtomic writes through a temp file in the same directory, then renames.
func writeAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(fileMode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// CheckRepo returns nil only when info's repo was confirmed at its current visibility.
func CheckRepo(s Store, info gh.RepoInfo) error {
	rec, ok, err := s.Repo(info.Slug)
	if errors.Is(err, ErrTampered) {
		return fmt.Errorf("%s was edited outside crew; delete it and run crew repo confirm again in a terminal: %w", s.reposPath(), err)
	}
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("this repo isn't confirmed; run crew repo confirm in a terminal")
	}
	if rec.Visibility != info.Visibility {
		return fmt.Errorf("this repo was confirmed as %s and is now %s; run crew repo confirm again", rec.Visibility, info.Visibility)
	}
	return nil
}
