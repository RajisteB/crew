package mission

import "fmt"

// Blocker names the other mission's piece that a queued piece is kept behind.
type Blocker struct {
	Mission string `json:"mission"`
	Piece   string `json:"piece"`
	PathA   string `json:"path_a"` // the other piece's pattern
	PathB   string `json:"path_b"` // this piece's pattern
	Stopped bool   `json:"stopped,omitempty"`
}

// String reads "P1 of mission 20261122-ab12: their paths overlap (api/**,
// api/v2/**)", with ", which is stopped" after the mission id when it is.
func (b Blocker) String() string {
	stopped := ""
	if b.Stopped {
		stopped = ", which is stopped"
	}
	return fmt.Sprintf("%s of mission %s%s: their paths overlap (%s, %s)", b.Piece, b.Mission, stopped, b.PathA, b.PathB)
}

// BlockedBy returns the first claim whose paths overlap the given paths.
func BlockedBy(paths []string, claims []Claim) (Blocker, bool) {
	for _, c := range claims {
		if a, b, ok := PathsOverlap(c.Paths, paths); ok {
			return Blocker{Mission: c.Mission, Piece: c.Piece, PathA: a, PathB: b, Stopped: c.Stopped}, true
		}
	}
	return Blocker{}, false
}

// settleBehind brings the piece's Behind in line with the observation and
// says whether the piece is held back. Only a ready piece is held; any other
// keeps no Behind. A change of blocker is recorded as a note on the piece, and
// a stopped one is also sent to the principal.
func (d *decider) settleBehind(id string, ready bool) bool {
	p := d.ps(id)
	b, behind := d.o.Behind[id]
	if !ready || !behind {
		p.Behind = nil
		return false
	}
	changed := p.Behind == nil || p.Behind.Mission != b.Mission || p.Behind.Piece != b.Piece || p.Behind.Stopped != b.Stopped
	p.Behind = &b
	if !changed {
		return true
	}
	d.record(EvNote, id, "starts after "+b.String())
	if b.Stopped {
		d.notify(fmt.Sprintf("%s can't start: it overlaps %s of mission %s, which is stopped. Resume that mission, or cancel that piece with crew mission amend.", id, b.Piece, b.Mission))
	}
	return true
}
