package mission

import (
	"fmt"
	"path/filepath"
	"slices"

	"github.com/crewfile-dev/crew/internal/source"
)

// Claim is a piece of another mission in this repo that has started and is not
// finished, with the paths it may change.
type Claim struct {
	Mission string // the other mission's id
	Piece   string
	Paths   []string // its effective paths; never empty
	Stopped bool     // that mission is not being driven: stopped, or its engine is gone
}

// holdsPaths says whether the piece has started (see hasStarted) and is not
// finished. A delegated piece never has a run, so it holds nothing.
func holdsPaths(p PieceState) bool {
	return hasStarted(p) && p.Status != PieceComplete && p.Status != PieceCancelled && p.Status != PieceDelegated
}

// ClaimsElsewhere lists what the repo's other missions hold, by mission id, then
// in piece order. A piece holds its effective paths from the moment it starts
// until it is complete or cancelled, whatever its mission's state. A piece
// with no effective paths holds nothing. A mission that holds a piece but
// whose mission.md can't be read is an error: crew can't tell what it holds.
func ClaimsElsewhere(repoRoot, missionID string, domains map[string]source.Domain) ([]Claim, error) {
	var claims []Claim
	err := walkOtherMissions(repoRoot, missionID, func(o otherMission) error {
		mine, err := claimsOf(repoRoot, o, domains)
		claims = append(claims, mine...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return claims, nil
}

func claimsOf(repoRoot string, o otherMission, domains map[string]source.Domain) ([]Claim, error) {
	if !slices.ContainsFunc(o.State.Pieces, holdsPaths) {
		return nil, nil
	}
	m, err := Store{Dir: filepath.Join(missionsDir(repoRoot), o.ID)}.Mission()
	if err != nil {
		return nil, fmt.Errorf("mission %s: %w", o.ID, err)
	}
	var out []Claim
	for _, ps := range o.State.Pieces {
		if !holdsPaths(ps) {
			continue
		}
		p, ok := m.Piece(ps.ID)
		if !ok {
			return nil, fmt.Errorf("mission %s: piece %s is not in mission.md", o.ID, ps.ID)
		}
		if paths := EffectivePaths(p, domains[p.Domain]); len(paths) > 0 {
			out = append(out, Claim{Mission: o.ID, Piece: ps.ID, Paths: paths, Stopped: !o.Driven})
		}
	}
	return out, nil
}

// observeBehind fills Behind in the tick's observation. When the other
// missions can't be read, nothing starts this tick.
func (t *tick) observeBehind() {
	claims, err := ClaimsElsewhere(t.e.RepoRoot, t.s.MissionID, t.e.Domains)
	if err != nil {
		t.note("", "can't read the repo's other missions: %v", err)
		t.obs.ElsewhereUnknown = true
		return
	}
	for _, ps := range t.s.Pieces {
		if ps.Status != PieceQueued {
			continue
		}
		p, ok := t.m.Piece(ps.ID)
		if !ok {
			continue
		}
		if b, blocked := BlockedBy(EffectivePaths(p, t.e.Domains[p.Domain]), claims); blocked {
			if t.obs.Behind == nil {
				t.obs.Behind = map[string]Blocker{}
			}
			t.obs.Behind[ps.ID] = b
		}
	}
}

// startClear checks the piece against the repo's other missions again, under
// the start lock, and says whether it may start. Blocked leaves the piece
// queued with no note: the next tick's Decide records why.
func (t *tick) startClear(p Piece) bool {
	claims, err := ClaimsElsewhere(t.e.RepoRoot, t.s.MissionID, t.e.Domains)
	if err != nil {
		t.note("", "can't read the repo's other missions: %v", err)
		return false
	}
	_, blocked := BlockedBy(EffectivePaths(p, t.e.Domains[p.Domain]), claims)
	return !blocked
}
