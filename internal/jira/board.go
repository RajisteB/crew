package jira

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// ErrNoFlag means the site has no Flagged field, so an issue cannot be flagged.
var ErrNoFlag = errors.New("this Jira site has no Flagged field")

// errUnknown is an unknown lane or kind: a bug in the caller, never worth a retry.
var errUnknown = errors.New("unknown value")

// IsPermanent is Permanent plus ErrNoFlag and an unknown lane or kind.
func IsPermanent(err error) bool {
	return Permanent(err) || errors.Is(err, ErrNoFlag) || errors.Is(err, errUnknown)
}

// Board is one repo's Jira project as crew uses it. It looks up the token's account
// and the Flagged field once each and remembers them.
type Board struct {
	Client *Client
	Repo   Repo

	mu        sync.Mutex
	account   string
	flagID    string
	flagKnown bool // the Flagged lookup succeeded; flagID == "" then means no such field
}

// Create makes one issue, or returns the one already carrying label. kind is "epic" or "piece".
// The issue's type is Repo.EpicType or Repo.IssueType, its labels are ["crew", label], its
// assignee is Repo.Assignee or the token's account, and a piece's parent is the epic's key.
func (b *Board) Create(ctx context.Context, kind, label, summary, body, parent string) (string, error) {
	var typ string
	switch kind {
	case "epic":
		typ = b.Repo.EpicType
	case "piece":
		typ = b.Repo.IssueType
	default:
		return "", fmt.Errorf("%w: issue kind %q", errUnknown, kind)
	}
	key, found, err := b.Client.FindByLabel(ctx, b.Repo.Project, label)
	if err != nil || found {
		return key, err
	}
	assignee, err := b.assignee(ctx)
	if err != nil {
		return "", err
	}
	return b.Client.Create(ctx, Issue{
		Project: b.Repo.Project, Type: typ, Summary: summary, Description: body,
		Assignee: assignee, Parent: parent, Labels: []string{"crew", label},
	})
}

func (b *Board) assignee(ctx context.Context) (string, error) {
	if b.Repo.Assignee != "" {
		return b.Repo.Assignee, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.account == "" {
		id, _, err := b.Client.Myself(ctx)
		if err != nil {
			return "", err
		}
		b.account = id
	}
	return b.account, nil
}

// Move puts the issue in a lane: "to-do" | "in-progress" | "in-review" | "done" -> Repo.Statuses.
func (b *Board) Move(ctx context.Context, key, lane string) error {
	s := b.Repo.Statuses
	var status string
	switch lane {
	case "to-do":
		status = s.ToDo
	case "in-progress":
		status = s.InProgress
	case "in-review":
		status = s.InReview
	case "done":
		status = s.Done
	default:
		return fmt.Errorf("%w: lane %q", errUnknown, lane)
	}
	return b.Client.MoveTo(ctx, key, status)
}

// Comment adds a comment to the issue.
func (b *Board) Comment(ctx context.Context, key, text string) error {
	return b.Client.Comment(ctx, key, text)
}

// Flag sets or clears the issue's flag. ErrNoFlag when the site has no Flagged field.
func (b *Board) Flag(ctx context.Context, key string, on bool) error {
	b.mu.Lock()
	if !b.flagKnown {
		id, found, err := b.Client.FlagField(ctx)
		if err != nil {
			b.mu.Unlock()
			return err
		}
		if !found {
			id = ""
		}
		b.flagID, b.flagKnown = id, true
	}
	id := b.flagID
	b.mu.Unlock()
	if id == "" {
		return ErrNoFlag
	}
	return b.Client.SetFlag(ctx, key, id, on)
}

// Attach uploads the regular file at path under its base name. A path that is not a regular file is an error.
func (b *Board) Attach(ctx context.Context, key, path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("attach %s: %w", filepath.Base(path), err)
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("attach %s: not a regular file", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("attach %s: %w", filepath.Base(path), err)
	}
	defer f.Close()
	return b.Client.Attach(ctx, key, filepath.Base(path), f)
}

// Done reports whether the issue is in Repo.Statuses.Done (case-insensitive).
func (b *Board) Done(ctx context.Context, key string) (bool, error) {
	st, err := b.Client.Status(ctx, key)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(st, b.Repo.Statuses.Done), nil
}

// Sprint puts the issues in the board's active sprint. With Repo.Board == 0 it does nothing
// and reports true. With no active sprint it does nothing and reports false, nil.
func (b *Board) Sprint(ctx context.Context, keys []string) (bool, error) {
	if b.Repo.Board == 0 {
		return true, nil
	}
	id, _, found, err := b.Client.ActiveSprint(ctx, b.Repo.Board)
	if err != nil || !found {
		return false, err
	}
	if err := b.Client.AddToSprint(ctx, id, keys); err != nil {
		return false, err
	}
	return true, nil
}

// Unavailable is a tracker that can't be reached: every method returns an error whose text is Reason.
type Unavailable struct{ Reason string }

func (u Unavailable) err() error { return errors.New(u.Reason) }

func (u Unavailable) Create(context.Context, string, string, string, string, string) (string, error) {
	return "", u.err()
}
func (u Unavailable) Move(context.Context, string, string) error    { return u.err() }
func (u Unavailable) Comment(context.Context, string, string) error { return u.err() }
func (u Unavailable) Flag(context.Context, string, bool) error      { return u.err() }
func (u Unavailable) Attach(context.Context, string, string) error  { return u.err() }
func (u Unavailable) Done(context.Context, string) (bool, error)    { return false, u.err() }
func (u Unavailable) Sprint(context.Context, []string) (bool, error) {
	return false, u.err()
}
