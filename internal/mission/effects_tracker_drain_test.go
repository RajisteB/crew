package mission

import (
	"context"
	"strings"
	"testing"
	"time"
)

// behindNote drains a finished one-piece mission with fail set up and returns
// the "Jira is behind" note.
func behindNote(t *testing.T, fail func(h *harness)) string {
	t.Helper()
	h := onePiece(t)
	h.withTracker()
	h.edit(func(s *State) { s.Pieces[0].Status = PieceComplete })
	fail(h)
	if _, err := h.e.Loop(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, e := range h.events(EvNote) {
		if strings.HasPrefix(e.Msg, "Jira is behind") {
			return strings.ReplaceAll(e.Msg, h.id, "<id>")
		}
	}
	t.Fatalf("no Jira is behind note in %v", h.events(EvNote))
	return ""
}

func TestLoop_BehindNoteWording(t *testing.T) {
	cases := map[string]struct {
		fail func(h *harness)
		want string
	}{
		"no epic": {func(h *harness) { h.fail.failNext("create-epic", 1000) },
			"Jira is behind: nothing reached the board; run crew jira sync <id>"},
		"one write": {func(h *harness) { h.fail.failNext("move:J-1", 1000) },
			"Jira is behind: 1 write was not sent; run crew jira sync <id>"},
		"two writes": {func(h *harness) { h.fail.failNext("move", 1000) },
			"Jira is behind: 2 writes were not sent; run crew jira sync <id>"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := behindNote(t, c.fail); got != c.want {
				t.Fatalf("note = %q, want %q", got, c.want)
			}
		})
	}
}
