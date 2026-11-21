package principal

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"
)

const (
	switchFile    = "automerge.json"
	switchSumFile = "automerge.json.sum"
)

// AutoMode is how far crew may merge on its own for one domain.
type AutoMode string

const (
	AutoOff           AutoMode = "off"
	AutoOn            AutoMode = "on"
	AutoWithoutChecks AutoMode = "without-checks"
)

// SwitchRecord is the auto-merge mode the principal set for one repo and domain.
type SwitchRecord struct {
	Repo   string    `json:"repo"` // "owner/name"
	Domain string    `json:"domain"`
	Mode   AutoMode  `json:"mode"`
	SetAt  time.Time `json:"set_at"`
}

func (s Store) switchPath() string    { return filepath.Join(s.Dir, switchFile) }
func (s Store) switchSumPath() string { return filepath.Join(s.Dir, switchSumFile) }

type switchKey struct{ repo, domain string }

// loadSwitches returns every record. A missing file is an empty set.
func (s Store) loadSwitches() (map[switchKey]SwitchRecord, error) {
	data, err := readSealed(s.switchPath(), s.switchSumPath())
	if errors.Is(err, os.ErrNotExist) {
		return map[switchKey]SwitchRecord{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.switchPath(), err)
	}
	var recs []SwitchRecord
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, fmt.Errorf("%s: %w", s.switchPath(), err)
	}
	out := make(map[switchKey]SwitchRecord, len(recs))
	for _, r := range recs {
		if !validMode(r.Mode) {
			return nil, fmt.Errorf("%s: unknown mode %q: %w", s.switchPath(), r.Mode, ErrTampered)
		}
		out[switchKey{r.Repo, r.Domain}] = r
	}
	return out, nil
}

func validMode(m AutoMode) bool {
	return m == AutoOff || m == AutoOn || m == AutoWithoutChecks
}

// Switches returns repo's records by domain. A missing file is an empty map.
// ErrTampered (wrapped, naming the file) when automerge.json and its checksum
// disagree, the checksum is missing, or a record holds an unknown mode.
func (s Store) Switches(repo string) (map[string]SwitchRecord, error) {
	all, err := s.loadSwitches()
	if err != nil {
		return nil, err
	}
	out := map[string]SwitchRecord{}
	for k, r := range all {
		if k.repo == repo {
			out[k.domain] = r
		}
	}
	return out, nil
}

// SetSwitches replaces the record of each (repo, domain) given and keeps the rest.
func (s Store) SetSwitches(recs []SwitchRecord) error {
	for _, r := range recs {
		if !validMode(r.Mode) {
			return fmt.Errorf("unknown auto-merge mode %q", r.Mode)
		}
	}
	all, err := s.loadSwitches()
	if err != nil {
		return err
	}
	for _, r := range recs {
		all[switchKey{r.Repo, r.Domain}] = r
	}
	list := make([]SwitchRecord, 0, len(all))
	for _, r := range all {
		list = append(list, r)
	}
	slices.SortFunc(list, func(a, b SwitchRecord) int {
		return cmp.Or(cmp.Compare(a.Repo, b.Repo), cmp.Compare(a.Domain, b.Domain))
	})
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return writeSealed(s.switchPath(), s.switchSumPath(), data)
}

// ModeOf is the domain's mode, AutoOff when it has no record.
func ModeOf(recs map[string]SwitchRecord, domain string) AutoMode {
	if r, ok := recs[domain]; ok {
		return r.Mode
	}
	return AutoOff
}
