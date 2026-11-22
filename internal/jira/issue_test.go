package jira

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestCreate_SendsFields(t *testing.T) {
	c, srv := newClient(t)
	in := Issue{
		Project: "ACME", Type: "Epic", Summary: "Ship it",
		Description: "line one\n\nline three", Assignee: "acc-9", Parent: "ACME-100",
		Labels: []string{"crew-m1", "crew-m1-P1"},
	}
	key, err := c.Create(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := srv.Issue(key)
	if !ok {
		t.Fatalf("server has no issue %s", key)
	}
	if got.Type != in.Type || got.Summary != in.Summary || got.Description != in.Description ||
		got.Assignee != in.Assignee || got.Parent != in.Parent || !slices.Equal(got.Labels, in.Labels) {
		t.Fatalf("issue = %+v, want fields of %+v", got, in)
	}
	if got.Status != "To Do" {
		t.Fatalf("status = %q, want To Do", got.Status)
	}
}

func TestCreate_LeavesOutEmptyAssigneeAndParent(t *testing.T) {
	c, srv := newClient(t)
	if _, err := c.Create(context.Background(), Issue{Project: "ACME", Type: "Task", Summary: "s"}); err != nil {
		t.Fatal(err)
	}
	bodies := srv.Bodies()
	if len(bodies) != 1 {
		t.Fatalf("got %d bodies, want 1", len(bodies))
	}
	for _, key := range []string{`"assignee"`, `"parent"`} {
		if strings.Contains(bodies[0], key) {
			t.Errorf("body has %s: %s", key, bodies[0])
		}
	}
}

func TestFindByLabel(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()
	if _, err := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "a"}); err != nil {
		t.Fatal(err)
	}
	want, err := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "b", Labels: []string{"crew-m1-P1"}})
	if err != nil {
		t.Fatal(err)
	}
	key, found, err := c.FindByLabel(ctx, "ACME", "crew-m1-P1")
	if err != nil || !found || key != want {
		t.Fatalf("FindByLabel = %q, %v, %v; want %q, true, nil", key, found, err, want)
	}
	key, found, err = c.FindByLabel(ctx, "ACME", "nope")
	if err != nil || found || key != "" {
		t.Fatalf("FindByLabel(nope) = %q, %v, %v; want empty, false, nil", key, found, err)
	}
}

func TestMoveTo_PicksTransitionByTargetStatus(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	key, _ := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "s"})
	if err := c.MoveTo(ctx, key, "in review"); err != nil {
		t.Fatal(err)
	}
	got, _ := srv.Issue(key)
	if got.Status != "In Review" {
		t.Fatalf("status = %q, want In Review", got.Status)
	}
	if st, err := c.Status(ctx, key); err != nil || st != "In Review" {
		t.Fatalf("Status = %q, %v", st, err)
	}
}

func TestMoveTo_AlreadyThereSendsNothing(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	key, _ := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "s"})
	if err := c.MoveTo(ctx, key, "to do"); err != nil {
		t.Fatal(err)
	}
	for _, r := range srv.Requests() {
		if strings.HasPrefix(r, "POST ") && strings.HasSuffix(r, "/transitions") {
			t.Fatalf("unexpected request %q", r)
		}
	}
}

func TestMoveTo_NoSuchStatus(t *testing.T) {
	c, _ := newClient(t)
	ctx := context.Background()
	key, _ := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "s"})
	err := c.MoveTo(ctx, key, "Blocked")
	if !errors.Is(err, ErrNoTransition) || !Permanent(err) {
		t.Fatalf("err = %v, want a permanent ErrNoTransition", err)
	}
}

func TestComment_OneParagraphPerLine(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	key, _ := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "s"})
	if err := c.Comment(ctx, key, "a\n\nb"); err != nil {
		t.Fatal(err)
	}
	got, _ := srv.Issue(key)
	if !slices.Equal(got.Comments, []string{"a\n\nb"}) {
		t.Fatalf("comments = %q", got.Comments)
	}
}

func TestIssueCalls_UnknownIssueIsPermanent(t *testing.T) {
	c, _ := newClient(t)
	_, err := c.Status(context.Background(), "ACME-99")
	if err == nil || !Permanent(err) {
		t.Fatalf("err = %v, want a permanent error", err)
	}
}
