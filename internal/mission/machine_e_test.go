package mission

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecide_UnreadableTickKeepsBehind(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceQueued)
	stopped := behindA
	stopped.Stopped = true
	notified := 0
	s, as := Decide(m, s, obsBehind("P1", stopped), limits(3, 3), 0)
	notified += len(of[Notify](as))
	unknown := obs(t0)
	unknown.ElsewhereUnknown = true
	s, as = Decide(m, s, unknown, limits(3, 3), 0)
	notified += len(of[Notify](as))
	if p := piece(t, s, "P1"); p.Behind == nil || len(of[StartPiece](as)) != 0 {
		t.Fatalf("unreadable tick: Behind = %+v, actions = %+v", p.Behind, as)
	}
	_, as = Decide(m, s, obsBehind("P1", stopped), limits(3, 3), 0)
	notified += len(of[Notify](as))
	if notified != 1 {
		t.Fatalf("notified %d times, want once", notified)
	}
}

func TestEngine_StartLockFailureIsNoted(t *testing.T) {
	h := newHarness(t, independent("P1")...)
	// A folder where the lock file goes: the lock can't be opened at all.
	if err := os.Mkdir(filepath.Join(missionsDir(h.e.RepoRoot), startsLockName), 0o755); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if got := h.piece("P1").Status; got != PieceQueued {
		t.Fatalf("P1 = %s, want queued", got)
	}
	for _, e := range h.events(EvNote) {
		if strings.HasPrefix(e.Msg, startLockNotePrefix) {
			return
		}
	}
	t.Fatalf("no %q note in %+v", startLockNotePrefix, h.events(EvNote))
}

const startLockNotePrefix = "can't take the repo's start lock: "

func TestDecide_AcceptedFilesAreTold(t *testing.T) {
	_, _, as, _, err := answer(pathsAnswerState(0, "go.mod", "Makefile", "a\nb", "d.txt", "e.txt"), Answer{Do: "accept", Words: "fine"})
	if err != nil {
		t.Fatal(err)
	}
	ns := of[Notify](as)
	want := "P1: 5 files outside its paths were accepted: go.mod, Makefile, a?b and 2 more"
	if len(ns) != 1 || ns[0].Body != want {
		t.Fatalf("notifications = %+v, want one %q", ns, want)
	}
	_, _, as, _, _ = answer(pathsAnswerState(0, "go.mod"), Answer{Do: "accept", Words: "fine"})
	if ns := of[Notify](as); len(ns) != 1 || ns[0].Body != "P1: 1 file outside its paths was accepted: go.mod" {
		t.Fatalf("notifications = %+v", ns)
	}
}
