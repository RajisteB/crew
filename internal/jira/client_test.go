package jira

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/crewfile-dev/crew/internal/jira/jiratest"
)

const (
	testEmail = "dev@example.com"
	testToken = "s3cr3t-token"
)

func newClient(t *testing.T) (*Client, *jiratest.Server) {
	t.Helper()
	srv := jiratest.New(t)
	return New(srv.URL(), testEmail, testToken), srv
}

func TestPermanent(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"400", &Error{Status: 400}, true},
		{"404", &Error{Status: 404}, true},
		{"422", &Error{Status: 422}, true},
		{"401", &Error{Status: 401}, false},
		{"403", &Error{Status: 403}, false},
		{"408", &Error{Status: 408}, false},
		{"429", &Error{Status: 429}, false},
		{"500", &Error{Status: 500}, false},
		{"503", &Error{Status: 503}, false},
		{"wrapped 404", fmt.Errorf("x: %w", &Error{Status: 404}), true},
		{"no transition", ErrNoTransition, true},
		{"network", errors.New("dial tcp: refused"), false},
		{"nil", nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Permanent(tc.err); got != tc.want {
				t.Fatalf("Permanent(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestClient_TokenNeverPrinted(t *testing.T) {
	c, srv := newClient(t)
	srv.Fail(500, 1)
	_, _, err := c.Myself(context.Background())
	if err == nil {
		t.Fatal("want an error from the failing server")
	}
	for name, text := range map[string]string{
		"error": err.Error(),
		"%+v":   fmt.Sprintf("%+v", c),
		"%#v":   fmt.Sprintf("%#v", c),
		"%v":    fmt.Sprintf("%v", *c),
	} {
		if strings.Contains(text, testToken) {
			t.Errorf("%s output contains the token: %s", name, text)
		}
	}
}

func TestClient_RefusesRedirects(t *testing.T) {
	var hits atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer other.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+"/rest/api/3/myself", http.StatusFound)
	}))
	defer first.Close()

	_, _, err := New(first.URL, testEmail, testToken).Myself(context.Background())
	if err == nil {
		t.Fatal("want an error for a redirect")
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("the redirect target saw %d requests, want 0", n)
	}
}

func TestClient_SendsBasicAuth(t *testing.T) {
	c, srv := newClient(t)
	ctx := context.Background()
	if _, _, err := c.Myself(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, Issue{Project: "ACME", Type: "Task", Summary: "s"}); err != nil {
		t.Fatal(err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte(testEmail+":"+testToken))
	auths := srv.SawAuth()
	if len(auths) != 2 {
		t.Fatalf("got %d requests, want 2", len(auths))
	}
	for _, a := range auths {
		if a != want {
			t.Errorf("Authorization = %q, want %q", a, want)
		}
	}
}

func TestError_TruncatesOnRuneBoundary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(strings.Repeat("é", 400)))
	}))
	defer srv.Close()
	_, _, err := New(srv.URL, testEmail, testToken).Myself(context.Background())
	var je *Error
	if !errors.As(err, &je) {
		t.Fatalf("want *Error, got %v", err)
	}
	if len(je.Body) != 300 || strings.ContainsRune(je.Body, '�') {
		t.Fatalf("body is %d bytes, want 300 clean bytes", len(je.Body))
	}
	if !strings.HasPrefix(err.Error(), "jira GET /rest/api/3/myself: HTTP 400: ") {
		t.Fatalf("unexpected error text %q", err.Error())
	}
}

func TestMyself(t *testing.T) {
	c, _ := newClient(t)
	id, name, err := c.Myself(context.Background())
	if err != nil || id != "acc-1" || name != "Test User" {
		t.Fatalf("Myself = %q, %q, %v", id, name, err)
	}
}
