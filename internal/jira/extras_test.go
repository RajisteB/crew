package jira

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func newIssue(t *testing.T, c *Client) string {
	t.Helper()
	key, err := c.Create(context.Background(), Issue{Project: "ACME", Type: "Task", Summary: "s"})
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func TestAttach_SendsTheFile(t *testing.T) {
	c, srv := newClient(t)
	key := newIssue(t, c)
	if err := c.Attach(context.Background(), key, "brief.md", strings.NewReader("hello")); err != nil {
		t.Fatal(err)
	}
	got, _ := srv.Issue(key)
	if !slices.Equal(got.Attachments, []string{"brief.md"}) {
		t.Fatalf("attachments = %q", got.Attachments)
	}
	if h := srv.Headers("X-Atlassian-Token"); !slices.Contains(h, "no-check") {
		t.Fatalf("X-Atlassian-Token values = %q", h)
	}
}

func TestFlag_SetAndClear(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	key := newIssue(t, c)
	field, found, err := c.FlagField(ctx)
	if err != nil || !found || field == "" {
		t.Fatalf("FlagField = %q, %v, %v", field, found, err)
	}
	if err := c.SetFlag(ctx, key, field, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.Issue(key); !got.Flagged || got.Flags != 1 {
		t.Fatalf("after set: %+v", got)
	}
	if err := c.SetFlag(ctx, key, field, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := srv.Issue(key); got.Flagged || got.Flags != 1 {
		t.Fatalf("after clear: %+v", got)
	}
}

func TestFlagField_Missing(t *testing.T) {
	c, srv := newClient(t)
	srv.DropFlagField()
	id, found, err := c.FlagField(context.Background())
	if err != nil || found || id != "" {
		t.Fatalf("FlagField = %q, %v, %v; want empty, false, nil", id, found, err)
	}
}

func TestSprint_ActiveAndNone(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	srv.SetActiveSprint(1, 7)
	id, name, found, err := c.ActiveSprint(ctx, 1)
	if err != nil || !found || id != 7 || name != "Sprint 7" {
		t.Fatalf("ActiveSprint = %d, %q, %v, %v", id, name, found, err)
	}
	srv.SetActiveSprint(1, 0)
	if _, _, found, err = c.ActiveSprint(ctx, 1); err != nil || found {
		t.Fatalf("no active sprint: found=%v err=%v", found, err)
	}
	srv.SetActiveSprint(1, 7)
	keys := []string{newIssue(t, c), newIssue(t, c)}
	if err := c.AddToSprint(ctx, 7, keys); err != nil {
		t.Fatal(err)
	}
	for _, k := range keys {
		if got, _ := srv.Issue(k); got.Sprint != 7 {
			t.Errorf("%s sprint = %d, want 7", k, got.Sprint)
		}
	}
}

func TestBoards(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	if got, err := c.Boards(ctx, "ACME"); err != nil || len(got) != 0 {
		t.Fatalf("new server boards = %v, %v", got, err)
	}
	srv.SetActiveSprint(3, 0)
	srv.SetActiveSprint(2, 5)
	got, err := c.Boards(ctx, "ACME")
	want := []BoardInfo{{2, "Board 2"}, {3, "Board 3"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Boards = %v, %v; want %v", got, err, want)
	}
}

func TestStatuses(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	got, err := c.Statuses(ctx, "ACME")
	want := []string{"To Do", "In Progress", "In Review", "Done"}
	if err != nil || !slices.Equal(got["Task"], want) || !slices.Equal(got["Epic"], want) {
		t.Fatalf("Statuses = %v, %v", got, err)
	}
	srv.DropStatus("In Review")
	got, _ = c.Statuses(ctx, "ACME")
	if slices.Contains(got["Task"], "In Review") {
		t.Fatalf("In Review still listed: %v", got)
	}
	key := newIssue(t, c)
	if err := c.MoveTo(ctx, key, "In Review"); !Permanent(err) {
		t.Fatalf("MoveTo dropped status err = %v, want permanent", err)
	}
}
