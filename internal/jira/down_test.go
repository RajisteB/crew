package jira

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsDown(t *testing.T) {
	opErr := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"net.Error", opErr, true},
		{"url.Error", &url.Error{Op: "Post", URL: "https://example.atlassian.net", Err: errors.New("EOF")}, true},
		{"wrapped url.Error", fmt.Errorf("jira GET /x: %w", &url.Error{Op: "Get", Err: opErr}), true},
		{"deadline", fmt.Errorf("x: %w", context.DeadlineExceeded), true},
		{"401", &Error{Status: 401}, true},
		{"408", &Error{Status: 408}, true},
		{"429", &Error{Status: 429}, true},
		{"500", &Error{Status: 500}, true},
		{"503", fmt.Errorf("x: %w", &Error{Status: 503}), true},
		{"403", &Error{Status: 403}, false},
		{"404", &Error{Status: 404}, false},
		{"400", &Error{Status: 400}, false},
		{"no transition", ErrNoTransition, false},
		{"no flag", ErrNoFlag, false},
		{"unavailable", Unavailable{Reason: "no credentials"}.Move(context.Background(), "ACME-1", "done"), true},
		{"decode", errors.New("jira GET /rest/api/3/myself: decode reply: unexpected end of JSON input"), false},
	}
	for _, tt := range tests {
		if got := IsDown(tt.err); got != tt.want {
			t.Errorf("%s: IsDown = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// slowServer answers every request after d.
func slowServer(t *testing.T, d time.Duration) *Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(d)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	return New(srv.URL, testEmail, testToken)
}

func TestAttach_OutlivesTheDefaultTimeout(t *testing.T) {
	old := requestTimeout
	requestTimeout = 50 * time.Millisecond
	t.Cleanup(func() { requestTimeout = old })
	c := slowServer(t, 300*time.Millisecond)
	if _, _, err := c.Myself(context.Background()); err == nil || !IsDown(err) {
		t.Fatalf("an ordinary call outlived the shortened limit: %v", err)
	}
	if err := c.Attach(context.Background(), "ACME-1", "big.bin", strings.NewReader("x")); err != nil {
		t.Fatalf("attach was cut by the default limit: %v", err)
	}
}

func TestAttachTimeout_GrowsWithTheFile(t *testing.T) {
	if got := attachTimeout(0); got != 30*time.Second {
		t.Fatalf("attachTimeout(0) = %s", got)
	}
	if got := attachTimeout(1_000_000); got != 40*time.Second {
		t.Fatalf("attachTimeout(1 MB) = %s", got)
	}
}

func TestAttach_RefusesARedirect(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Redirect(w, r, "/elsewhere", http.StatusTemporaryRedirect)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, testEmail, testToken)
	err := c.Attach(context.Background(), "ACME-1", "a.txt", strings.NewReader("x"))
	var je *Error
	if !errors.As(err, &je) || je.Status != http.StatusTemporaryRedirect || hits.Load() != 1 {
		t.Fatalf("err %v hits %d", err, hits.Load())
	}
}
