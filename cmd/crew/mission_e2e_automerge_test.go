package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/principal"
)

// switchOn writes the design domain's switch the way only crew automerge
// does in the product; a test may write it directly.
func (r *e2eRig) switchOn() {
	r.t.Helper()
	store := principal.Store{Dir: filepath.Join(r.crewDir, "principal")}
	rec := principal.SwitchRecord{Repo: "acme/app", Domain: "design", Mode: principal.AutoOn, SetAt: time.Now()}
	if err := store.SetSwitches([]principal.SwitchRecord{rec}); err != nil {
		r.t.Fatal(err)
	}
}

func (r *e2eRig) writeChecks(states ...string) {
	r.t.Helper()
	text := strings.Join(states, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(r.ghDir, "checks"), []byte(text), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// originRev is the commit a ref points to on the bare origin.
func (r *e2eRig) originRev(ref string) string {
	r.t.Helper()
	out, err := exec.Command("git", "--git-dir", r.origin, "rev-parse", "--verify", ref).Output()
	if err != nil {
		r.t.Fatalf("origin has no %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func (r *e2eRig) originHas(ancestor, ref string) bool {
	return exec.Command("git", "--git-dir", r.origin, "merge-base", "--is-ancestor", ancestor, ref).Run() == nil
}

func mergedByCrew(events []string) int {
	n := 0
	for _, e := range events {
		if strings.HasPrefix(e, "pr-merged ") && strings.Contains(e, "merged by crew") {
			n++
		}
	}
	return n
}

func TestMission_EndToEnd_AutoMergeOn(t *testing.T) {
	r := newE2ERig(t)
	r.confirmRepo()
	r.switchOn()
	r.writeChecks("pass")
	r.launch()

	var p1Head string
	st := r.tickUntil("both pieces complete and the mission done", func(s mission.State) bool {
		if p := piece(t, s, "P1"); p.Head != "" {
			p1Head = p.Head
		}
		return s.Status == mission.StatusDone
	})
	for _, id := range []string{"P1", "P2"} {
		if got := piece(t, st, id).Status; got != mission.PieceComplete {
			t.Fatalf("%s is %s, want complete", id, got)
		}
	}
	if n := mergedByCrew(r.events()); n != 2 {
		t.Fatalf("crew merged %d pull requests, want 2; events: %v", n, r.events())
	}
	if n := strings.Count(r.ghFile("calls"), "pr merge"); n != 2 {
		t.Fatalf("pr merge ran %d times, want 2; calls:\n%s", n, r.ghFile("calls"))
	}
	p2Branch := "crew/" + r.id + "/P2"
	if p1Head == "" || !r.originHas(p1Head, "refs/heads/"+p2Branch) {
		t.Fatalf("P2's branch does not hold P1's commit %q", p1Head)
	}
	if !r.originHas(r.originRev("refs/heads/"+p2Branch), "refs/heads/main") {
		t.Fatal("origin main does not hold P2's commit")
	}
}

func TestMission_EndToEnd_ChecksFailThenPass(t *testing.T) {
	r := newE2ERig(t)
	r.confirmRepo()
	r.switchOn()
	r.writeChecks("fail")
	r.launch()

	st := r.tickUntil("P1 sent back for failed checks", func(s mission.State) bool {
		return piece(t, s, "P1").SentBack["checks-failed"] == 1
	})
	firstHead := piece(t, st, "P1").Head
	if firstHead == "" {
		t.Fatal("P1 has no recorded head")
	}
	if got := strings.Count(r.ghFile("calls"), "pr merge"); got != 0 {
		t.Fatalf("crew tried to merge a pull request with failed checks:\n%s", r.ghFile("calls"))
	}

	r.writeChecks("pass")
	r.tickUntil("P1 complete", func(s mission.State) bool {
		return piece(t, s, "P1").Status == mission.PieceComplete
	})
	if n := strings.Count(r.ghFile("calls"), "pr create"); n != 1 {
		t.Fatalf("pr create ran %d times, want 1 (the same pull request)", n)
	}
	if mergedByCrew(r.events()) != 1 {
		t.Fatalf("P1 was not merged by crew: %v", r.events())
	}
	branch := "crew/" + r.id + "/P1"
	final := r.originRev("refs/heads/" + branch)
	if final == firstHead || !r.originHas(firstHead, final) {
		t.Fatalf("the piece did not commit again: first %s, final %s", firstHead, final)
	}
	if !r.originHas(final, "refs/heads/main") {
		t.Fatal("origin main does not hold the fixed commit")
	}
}
