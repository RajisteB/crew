package mission

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/source"
)

func claimDomains() map[string]source.Domain {
	return map[string]source.Domain{
		"backend": {Name: "backend", Paths: []string{"api/**"}},
		"site":    {Name: "site", Paths: []string{"web/**"}},
		"empty":   {Name: "empty"},
	}
}

func domainPiece(id, domain string, paths ...string) Piece {
	return Piece{ID: id, Domain: domain, Factory: "feature", Brief: "do " + id, Paths: paths}
}

func setStatuses(h *harness, status Status, statuses ...PieceStatus) {
	h.edit(func(s *State) {
		s.Status = status
		for i, ps := range statuses {
			s.Pieces[i].Status = ps
		}
	})
}

func TestClaimsElsewhere(t *testing.T) {
	root := t.TempDir()
	this := newHarnessIn(t, root, independent("P1")...)
	this.runPiece("P1")
	running := newHarnessIn(t, root,
		domainPiece("P1", "backend", "api/v2/**"), domainPiece("P2", "backend"), domainPiece("P3", "backend"),
		domainPiece("P4", "site"), domainPiece("P5", "empty"))
	setStatuses(running, StatusRunning, PieceRunning, PieceQueued, PieceComplete, PiecePROpen, PieceDelivered)
	stopped := newHarnessIn(t, root, domainPiece("P1", "backend", "docs/**"))
	setStatuses(stopped, StatusStopped, PieceWaiting)
	planned := newHarnessIn(t, root, domainPiece("P1", "backend"))
	setStatuses(planned, StatusPlanned, PieceQueued)

	got, err := ClaimsElsewhere(root, this.id, claimDomains())
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Claim{
		running.id + "/P1": {Mission: running.id, Piece: "P1", Paths: []string{"api/v2/**"}},
		running.id + "/P4": {Mission: running.id, Piece: "P4", Paths: []string{"web/**"}},
		stopped.id + "/P1": {Mission: stopped.id, Piece: "P1", Paths: []string{"docs/**"}, Stopped: true},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d claims, want 3: %+v", len(got), got)
	}
	for i, c := range got {
		if w, ok := want[c.Mission+"/"+c.Piece]; !ok || !reflect.DeepEqual(c, w) {
			t.Errorf("unexpected claim %+v", c)
		}
		if i > 0 && got[i-1].Mission > c.Mission {
			t.Errorf("claims not in mission id order: %+v", got)
		}
	}
}

func TestClaimsElsewhere_UnreadableMissionIsAnError(t *testing.T) {
	root := t.TempDir()
	this := newHarnessIn(t, root, independent("P1")...)
	other := newHarnessIn(t, root, domainPiece("P1", "backend", "api/**"))
	if err := os.Remove(filepath.Join(other.store.Dir, "mission.md")); err != nil {
		t.Fatal(err)
	}
	setStatuses(other, StatusRunning, PieceQueued)
	if _, err := ClaimsElsewhere(root, this.id, claimDomains()); err != nil {
		t.Fatalf("all pieces queued: %v", err)
	}
	setStatuses(other, StatusRunning, PieceRunning)
	_, err := ClaimsElsewhere(root, this.id, claimDomains())
	if err == nil || !strings.Contains(err.Error(), other.id) {
		t.Fatalf("err = %v, want one naming %s", err, other.id)
	}
}

func TestClaimsElsewhere_UnreadableStateIsAnError(t *testing.T) {
	root := t.TempDir()
	this := newHarnessIn(t, root, independent("P1")...)
	other := newHarnessIn(t, root, independent("P1")...)
	if err := os.WriteFile(filepath.Join(other.store.Dir, "state.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := ClaimsElsewhere(root, this.id, claimDomains()); err == nil {
		t.Fatal("want an error for an unreadable state")
	}
}

func TestBlockedBy(t *testing.T) {
	claims := []Claim{
		{Mission: "A", Piece: "P1", Paths: []string{"docs/**"}},
		{Mission: "B", Piece: "P2", Paths: []string{"api/v2/**"}, Stopped: true},
	}
	got, ok := BlockedBy([]string{"web/**", "api/**"}, claims)
	want := Blocker{Mission: "B", Piece: "P2", PathA: "api/v2/**", PathB: "api/**", Stopped: true}
	if !ok || got != want {
		t.Fatalf("got %+v %v, want %+v", got, ok, want)
	}
	if _, ok := BlockedBy([]string{"web/**"}, claims); ok {
		t.Error("no overlap should not block")
	}
	if _, ok := BlockedBy(nil, claims); ok {
		t.Error("nil paths should not block")
	}
}

func TestBlocker_String(t *testing.T) {
	b := Blocker{Mission: "20261122-ab12", Piece: "P1", PathA: "api/**", PathB: "api/v2/**"}
	if got, want := b.String(), "P1 of mission 20261122-ab12: their paths overlap (api/**, api/v2/**)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	b.Stopped = true
	if got, want := b.String(), "P1 of mission 20261122-ab12, which is stopped: their paths overlap (api/**, api/v2/**)"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
