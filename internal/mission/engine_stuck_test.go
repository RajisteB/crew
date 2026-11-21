package mission

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// clock is a settable time source for the engine.
type clock struct{ now time.Time }

func (c *clock) advance(d time.Duration) { c.now = c.now.Add(d) }

func stuckHarness(t *testing.T, remind string) (*harness, *clock) {
	t.Helper()
	h := newHarness(t, twoPieces()[0])
	c := &clock{now: testNow}
	h.e.Now = func() time.Time { return c.now }
	h.e.Settings.RemindAfter = remind
	h.runs.setRun(h.runPiece("P1"), doneState(), false)
	return h, c
}

func failingTicks(h *harness, c *clock, n int, step time.Duration) {
	h.t.Helper()
	for range n {
		h.fail.failNext("Push", 1)
		h.tick()
		c.advance(step)
	}
}

func TestEngine_FailureThatNeverClearsNotifiesOnce(t *testing.T) {
	h, c := stuckHarness(t, "1h")
	failingTicks(h, c, 10, 10*time.Minute) // 100 minutes of the same failure
	if len(h.notif.sent) != 1 {
		t.Fatalf("notifications = %v", h.notif.sent)
	}
	body := h.notif.sent[0]
	if !strings.Contains(body, h.id) || !strings.Contains(body, "P1") || !strings.Contains(body, "can't push") {
		t.Fatalf("notification = %q", body)
	}
	if n := len(h.events(EvNotified)); n != 1 {
		t.Fatalf("notified events = %d", n)
	}

	// The failure clears (the push works and the PR opens), then comes back.
	h.tick()
	if h.piece("P1").Status != PiecePROpen {
		t.Fatalf("piece = %+v", h.piece("P1"))
	}
	h.runs.setRun(h.runPiece("P1"), doneState(), false)
	failingTicks(h, c, 10, 10*time.Minute)
	if len(h.notif.sent) != 2 || len(h.events(EvNotified)) != 2 {
		t.Fatalf("notifications = %v, events = %d", h.notif.sent, len(h.events(EvNotified)))
	}
}

func TestEngine_FailureWithoutRemindAfterNotifiesAtFiveTicks(t *testing.T) {
	h, c := stuckHarness(t, "")
	failingTicks(h, c, 4, time.Minute)
	if len(h.notif.sent) != 0 {
		t.Fatalf("notified after 4 ticks: %v", h.notif.sent)
	}
	failingTicks(h, c, 3, time.Minute)
	if len(h.notif.sent) != 1 {
		t.Fatalf("notifications = %v", h.notif.sent)
	}
}

func TestEngine_RepeatingTickErrorNotifiesOnce(t *testing.T) {
	h, c := stuckHarness(t, "1h")
	if err := os.WriteFile(filepath.Join(h.store.Dir, "mission.md"), []byte("not a mission"), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 10 {
		if _, err := h.e.Tick(context.Background()); err == nil {
			t.Fatal("expected the tick to fail")
		}
		c.advance(10 * time.Minute)
	}
	if len(h.notif.sent) != 1 || !strings.Contains(h.notif.sent[0], h.id) {
		t.Fatalf("notifications = %v", h.notif.sent)
	}
}
