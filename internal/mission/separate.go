package mission

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/source"
)

const crewBy = "crew"

// Separation says one piece is kept apart from another, and which patterns overlap.
type Separation struct{ Piece, From, PathA, PathB string } // PathA is From's pattern, PathB is Piece's

func (s Separation) String() string {
	return fmt.Sprintf("%s starts after %s: their paths overlap (%s: %s, %s: %s)",
		s.Piece, s.From, s.From, s.PathA, s.Piece, s.PathB)
}

// SeparateOverlaps clears every piece's Apart and sets it again. It takes each piece
// in file order and looks at the pieces before it, from the nearest back to the
// first: when the two could run together and their effective paths overlap, the
// later piece is kept apart from the earlier. settled names pieces that have
// already started: a settled piece is never given an entry, so the other piece of
// the pair is kept apart from it, and a pair of two settled pieces is left alone.
// It returns a new mission and the separations in the order made. It never changes m.
func SeparateOverlaps(m Mission, domains []source.Domain, settled map[string]bool) (Mission, []Separation) {
	out := m
	out.Pieces = make([]Piece, len(m.Pieces))
	index := make(map[string]int, len(m.Pieces))
	paths := make([][]string, len(m.Pieces))
	for i, p := range m.Pieces {
		p.Apart = nil
		out.Pieces[i] = p
		index[p.ID] = i
		d, _ := findDomain(domains, p.Domain)
		paths[i] = EffectivePaths(p, d)
	}
	var seps []Separation
	for j := range out.Pieces {
		for i := j - 1; i >= 0; i-- {
			if settled[out.Pieces[i].ID] && settled[out.Pieces[j].ID] {
				continue
			}
			if reaches(out.Pieces, index, i, j) || reaches(out.Pieces, index, j, i) {
				continue
			}
			pa, pb, ok := PathsOverlap(paths[i], paths[j])
			if !ok {
				continue
			}
			later, earlier := j, i
			if settled[out.Pieces[j].ID] {
				later, earlier = i, j
				pa, pb = pb, pa
			}
			out.Pieces[later].Apart = append(slices.Clone(out.Pieces[later].Apart), out.Pieces[earlier].ID)
			seps = append(seps, Separation{Piece: out.Pieces[later].ID, From: out.Pieces[earlier].ID, PathA: pa, PathB: pb})
		}
	}
	return out, seps
}

// reaches says whether piece from depends on piece to, through waits_for and
// apart_from, in any number of hops.
func reaches(pieces []Piece, index map[string]int, from, to int) bool {
	seen := make([]bool, len(pieces))
	stack := []int{from}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, id := range slices.Concat(pieces[cur].WaitsFor, pieces[cur].Apart) {
			k, ok := index[id]
			switch {
			case !ok || seen[k]:
			case k == to:
				return true
			default:
				seen[k] = true
				stack = append(stack, k)
			}
		}
	}
	return false
}

// SeparationChange is the change-log entry for separations.
func SeparationChange(seps []Separation, now time.Time) Change {
	texts := make([]string, len(seps))
	for i, s := range seps {
		texts[i] = s.String()
	}
	return Change{Time: now, By: crewBy, What: strings.Join(texts, "; "), Why: "pieces whose paths overlap don't run together"}
}

// apartPairs is the set of (piece, from) pairs the mission's Apart fields hold.
func apartPairs(m Mission) []string {
	var out []string
	for _, p := range m.Pieces {
		for _, from := range p.Apart {
			out = append(out, p.ID+"<"+from)
		}
	}
	slices.Sort(out)
	return out
}

// SeparatedWithLog separates m as SeparateOverlaps does and, when the pairs
// differ from what before held, appends SeparationChange to its changes. It
// returns the separations made and whether the pairs changed.
func SeparatedWithLog(before, m Mission, domains []source.Domain, settled map[string]bool, now time.Time) (Mission, []Separation, bool) {
	next, seps := SeparateOverlaps(m, domains, settled)
	if slices.Equal(apartPairs(before), apartPairs(next)) {
		return next, seps, false
	}
	if len(seps) > 0 {
		next = next.WithChange(SeparationChange(seps, now))
	}
	return next, seps, true
}

// StartedPieces names the pieces of s that have started: not queued, and not
// parked on a wait that goes back to queued.
func StartedPieces(s State) map[string]bool {
	out := map[string]bool{}
	for _, p := range s.Pieces {
		if p.Status != PieceQueued && !(p.Status == PieceWaiting && p.Wait != nil && p.Wait.Back == PieceQueued) {
			out[p.ID] = true
		}
	}
	return out
}
