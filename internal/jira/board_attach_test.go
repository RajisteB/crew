package jira

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBoard_AttachRefusesAHardLink(t *testing.T) {
	b, srv := newBoard(t, nil)
	key, _ := b.Create(context.Background(), "piece", "l", "s", "", "")
	secret := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(secret, []byte("private key"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "key")
	if err := os.Link(secret, link); err != nil {
		t.Skip("no hard links:", err)
	}
	if err := b.Attach(context.Background(), key, link); err == nil {
		t.Fatal("want an error for a hard link")
	}
	if is, _ := srv.Issue(key); len(is.Attachments) != 0 {
		t.Fatalf("attachments = %v", is.Attachments)
	}
}
