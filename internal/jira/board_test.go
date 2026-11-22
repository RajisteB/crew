package jira

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/jira/jiratest"
)

func newBoard(t *testing.T, mod func(*Repo)) (*Board, *jiratest.Server) {
	t.Helper()
	c, srv := newClient(t)
	r := Repo{Site: "https://example.atlassian.net", Project: "ACME"}.withDefaults()
	if mod != nil {
		mod(&r)
	}
	return &Board{Client: c, Repo: r}, srv
}

func TestBoard_CreateEpicAndPiece(t *testing.T) {
	b, srv := newBoard(t, nil)
	ctx := context.Background()
	epic, err := b.Create(ctx, "epic", "crew-m1-epic", "Mission", "body", "")
	if err != nil {
		t.Fatal(err)
	}
	piece, err := b.Create(ctx, "piece", "crew-m1-P1", "Piece", "body", epic)
	if err != nil {
		t.Fatal(err)
	}
	e, _ := srv.Issue(epic)
	if e.Type != "Epic" || !slices.Equal(e.Labels, []string{"crew", "crew-m1-epic"}) || e.Parent != "" || e.Assignee != "acc-1" {
		t.Fatalf("epic = %+v", e)
	}
	p, _ := srv.Issue(piece)
	if p.Type != "Task" || p.Parent != epic || p.Assignee != "acc-1" {
		t.Fatalf("piece = %+v", p)
	}
}

func TestBoard_CreateUsesConfiguredAssignee(t *testing.T) {
	b, srv := newBoard(t, func(r *Repo) { r.Assignee = "acc-9" })
	key, err := b.Create(context.Background(), "piece", "crew-m1-P1", "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if is, _ := srv.Issue(key); is.Assignee != "acc-9" {
		t.Fatalf("assignee = %q", is.Assignee)
	}
	if slices.Contains(srv.Requests(), "GET /rest/api/3/myself") {
		t.Fatal("asked who the token belongs to")
	}
}

func TestBoard_CreateFindsItsOwnIssue(t *testing.T) {
	b, srv := newBoard(t, nil)
	ctx := context.Background()
	k1, err := b.Create(ctx, "piece", "crew-m1-P1", "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	k2, err := b.Create(ctx, "piece", "crew-m1-P1", "s", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 || len(srv.Issues()) != 1 {
		t.Fatalf("keys %s, %s; %d issues", k1, k2, len(srv.Issues()))
	}
}

func TestBoard_CreateUnknownKind(t *testing.T) {
	b, srv := newBoard(t, nil)
	_, err := b.Create(context.Background(), "story", "l", "s", "", "")
	if err == nil || !IsPermanent(err) || len(srv.Requests()) != 0 {
		t.Fatalf("err = %v, requests = %v", err, srv.Requests())
	}
}

func TestBoard_AccountLookupRetriesAfterFailure(t *testing.T) {
	b, srv := newBoard(t, nil)
	srv.Fail(500, 2) // the search and nothing else would fail first; fail enough to hit myself
	ctx := context.Background()
	if _, err := b.Create(ctx, "piece", "l", "s", "", ""); err == nil {
		t.Fatal("want an error")
	}
	srv.Fail(0, 0)
	if _, err := b.Create(ctx, "piece", "l", "s", "", ""); err != nil {
		t.Fatalf("second try: %v", err)
	}
}

func TestBoard_MoveMapsLanes(t *testing.T) {
	b, srv := newBoard(t, nil)
	key, _ := b.Create(context.Background(), "piece", "l", "s", "", "")
	for lane, want := range map[string]string{
		"in-progress": "In Progress", "in-review": "In Review", "done": "Done", "to-do": "To Do",
	} {
		if err := b.Move(context.Background(), key, lane); err != nil {
			t.Fatalf("%s: %v", lane, err)
		}
		if is, _ := srv.Issue(key); is.Status != want {
			t.Fatalf("%s: status %q, want %q", lane, is.Status, want)
		}
	}
	n := len(srv.Requests())
	err := b.Move(context.Background(), key, "nope")
	if err == nil || !IsPermanent(err) || len(srv.Requests()) != n {
		t.Fatalf("err = %v, new requests = %d", err, len(srv.Requests())-n)
	}
}

func TestBoard_CommentAndFlag(t *testing.T) {
	b, srv := newBoard(t, nil)
	ctx := context.Background()
	key, _ := b.Create(ctx, "piece", "l", "s", "", "")
	if err := b.Comment(ctx, key, "hello"); err != nil {
		t.Fatal(err)
	}
	if err := b.Flag(ctx, key, true); err != nil {
		t.Fatal(err)
	}
	is, _ := srv.Issue(key)
	if !is.Flagged || !slices.Equal(is.Comments, []string{"hello"}) {
		t.Fatalf("issue = %+v", is)
	}
	if err := b.Flag(ctx, key, false); err != nil {
		t.Fatal(err)
	}
	if is, _ := srv.Issue(key); is.Flagged {
		t.Fatal("still flagged")
	}
}

func TestBoard_FlagWithoutAField(t *testing.T) {
	b, srv := newBoard(t, nil)
	ctx := context.Background()
	key, _ := b.Create(ctx, "piece", "l", "s", "", "")
	srv.DropFlagField()
	err := b.Flag(ctx, key, true)
	if !errors.Is(err, ErrNoFlag) || !IsPermanent(err) {
		t.Fatalf("err = %v", err)
	}
	n := len(srv.Requests())
	if err := b.Flag(ctx, key, true); !errors.Is(err, ErrNoFlag) {
		t.Fatalf("second err = %v", err)
	}
	if len(srv.Requests()) != n {
		t.Fatal("second Flag sent a request")
	}
}

func TestBoard_AttachUploadsARegularFile(t *testing.T) {
	b, srv := newBoard(t, nil)
	key, _ := b.Create(context.Background(), "piece", "l", "s", "", "")
	p := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := b.Attach(context.Background(), key, p); err != nil {
		t.Fatal(err)
	}
	if is, _ := srv.Issue(key); !slices.Equal(is.Attachments, []string{"report.md"}) {
		t.Fatalf("attachments = %v", is.Attachments)
	}
}

func TestBoard_AttachRefusesASymlink(t *testing.T) {
	b, srv := newBoard(t, nil)
	key, _ := b.Create(context.Background(), "piece", "l", "s", "", "")
	dir := t.TempDir()
	real, link := filepath.Join(dir, "real.md"), filepath.Join(dir, "link.md")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	if err := b.Attach(context.Background(), key, link); err == nil {
		t.Fatal("want an error for a symlink")
	}
	if err := b.Attach(context.Background(), key, dir); err == nil {
		t.Fatal("want an error for a directory")
	}
	if is, _ := srv.Issue(key); len(is.Attachments) != 0 {
		t.Fatalf("attachments = %v", is.Attachments)
	}
}

func TestBoard_Done(t *testing.T) {
	b, srv := newBoard(t, nil)
	ctx := context.Background()
	key, _ := b.Create(ctx, "piece", "l", "s", "", "")
	srv.SetStatus(key, "Done")
	if got, err := b.Done(ctx, key); err != nil || !got {
		t.Fatalf("Done = %v, %v", got, err)
	}
	srv.SetStatus(key, "In Review")
	if got, err := b.Done(ctx, key); err != nil || got {
		t.Fatalf("In Review: Done = %v, %v", got, err)
	}
	b.Repo.Statuses.Done = "done"
	srv.SetStatus(key, "Done")
	if got, err := b.Done(ctx, key); err != nil || !got {
		t.Fatalf("lower-case config: Done = %v, %v", got, err)
	}
}

func TestBoard_SprintCases(t *testing.T) {
	ctx := context.Background()
	b, srv := newBoard(t, nil)
	if ok, err := b.Sprint(ctx, []string{"ACME-1"}); !ok || err != nil || len(srv.Requests()) != 0 {
		t.Fatalf("board 0: %v, %v, %v", ok, err, srv.Requests())
	}

	b, srv = newBoard(t, func(r *Repo) { r.Board = 1 })
	srv.SetActiveSprint(1, 7)
	key, _ := b.Create(ctx, "piece", "l", "s", "", "")
	if ok, err := b.Sprint(ctx, []string{key}); !ok || err != nil {
		t.Fatalf("sprint 7: %v, %v", ok, err)
	}
	if is, _ := srv.Issue(key); is.Sprint != 7 {
		t.Fatalf("sprint = %d", is.Sprint)
	}

	b, srv = newBoard(t, func(r *Repo) { r.Board = 1 })
	srv.SetActiveSprint(1, 0)
	if ok, err := b.Sprint(ctx, []string{"ACME-1"}); ok || err != nil {
		t.Fatalf("none active: %v, %v", ok, err)
	}
}

func TestUnavailable(t *testing.T) {
	var u Unavailable = Unavailable{Reason: "no credentials"}
	ctx := context.Background()
	_, e1 := u.Create(ctx, "epic", "l", "s", "b", "")
	_, e5 := u.Done(ctx, "K-1")
	_, e6 := u.Sprint(ctx, nil)
	for i, err := range []error{e1, u.Move(ctx, "K-1", "done"), u.Comment(ctx, "K-1", "t"),
		u.Flag(ctx, "K-1", true), u.Attach(ctx, "K-1", "p"), e5, e6} {
		if err == nil || err.Error() != "no credentials" || IsPermanent(err) {
			t.Fatalf("method %d: err = %v", i, err)
		}
	}
}
