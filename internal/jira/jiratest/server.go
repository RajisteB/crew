// Package jiratest is an in-memory Jira Cloud behind httptest, serving the
// endpoints the jira client calls. It is for tests only.
package jiratest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	project       = "ACME"
	flagFieldID   = "customfield_10021"
	accountID     = "acc-1"
	accountName   = "Test User"
	defaultStatus = "To Do"
)

// Issue is one issue as the server holds it.
type Issue struct {
	Key, Type, Summary, Description, Assignee, Parent, Status string
	Labels, Comments, Attachments                             []string
	History                                                   []string // every status the issue has been in, in order
	Flagged                                                   bool
	Flags                                                     int // how many times the flag was set
	Sprint                                                    int
}

type failure struct{ status, times int }

// rule fails the requests whose method and path match.
type rule struct {
	method, prefix string
	failure
}

// Server is an in-memory Jira.
type Server struct {
	t   *testing.T
	srv *httptest.Server

	mu        sync.Mutex
	issues    []*Issue
	statuses  []string
	issueType []string
	flagField bool
	boards    map[int]int // board id -> active sprint id, 0 for none
	fail      failure
	rules     []rule
	requests  []string
	bodies    []string
	headers   []http.Header
}

// New starts a server with project ACME; statuses To Do, In Progress, In Review
// and Done for every issue type; a Flagged field; and no board.
func New(t *testing.T) *Server {
	t.Helper()
	s := &Server{
		t:         t,
		statuses:  []string{"To Do", "In Progress", "In Review", "Done"},
		issueType: []string{"Task", "Epic"},
		flagField: true,
		boards:    map[int]int{},
	}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.srv.Close)
	return s
}

// URL is the base URL of the server.
func (s *Server) URL() string { return s.srv.URL }

// Issue returns a copy of the issue with the given key.
func (s *Server) Issue(key string) (Issue, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if is := s.find(key); is != nil {
		return is.clone(), true
	}
	return Issue{}, false
}

// Issues returns copies of every issue, in creation order.
func (s *Server) Issues() []Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Issue, 0, len(s.issues))
	for _, is := range s.issues {
		out = append(out, is.clone())
	}
	return out
}

// SetStatus moves an issue the way a person does on the board.
func (s *Server) SetStatus(key, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if is := s.find(key); is != nil {
		is.setStatus(status)
	}
}

// SetActiveSprint makes board exist. Sprint 0 means the board has sprints and none is active.
func (s *Server) SetActiveSprint(board, sprint int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.boards[board] = sprint
}

// DropFlagField removes the Flagged field.
func (s *Server) DropFlagField() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.flagField = false
}

// DropStatus removes a status from every issue type; no transition leads to it.
func (s *Server) DropStatus(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.statuses = slices.DeleteFunc(slices.Clone(s.statuses), func(st string) bool {
		return strings.EqualFold(st, name)
	})
}

// Fail makes the next times requests reply with the HTTP status.
func (s *Server) Fail(status, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fail = failure{status, times}
}

// FailOn makes the next times requests whose method is method and whose path
// starts with pathPrefix reply with the HTTP status. Other requests are served.
func (s *Server) FailOn(method, pathPrefix string, status, times int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rules = append(s.rules, rule{method, pathPrefix, failure{status, times}})
}

// failing reports the status the request must fail with, if any, and counts
// the failure. The caller holds the lock.
func (s *Server) failing(r *http.Request) (int, bool) {
	if s.fail.times > 0 {
		s.fail.times--
		return s.fail.status, true
	}
	for i := range s.rules {
		ru := &s.rules[i]
		if ru.times > 0 && ru.method == r.Method && strings.HasPrefix(r.URL.Path, ru.prefix) {
			ru.times--
			return ru.status, true
		}
	}
	return 0, false
}

// Requests lists "METHOD path" for every request, in order. Query strings are left out.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.requests)
}

// Bodies lists the body of every request, in order, parallel to Requests.
func (s *Server) Bodies() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.bodies)
}

// Headers lists the named header's value for every request, in order.
func (s *Server) Headers(name string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.headers))
	for _, h := range s.headers {
		out = append(out, h.Get(name))
	}
	return out
}

// SawAuth lists the Authorization header of each request.
func (s *Server) SawAuth() []string { return s.Headers("Authorization") }

func (s *Server) find(key string) *Issue {
	for _, is := range s.issues {
		if is.Key == key {
			return is
		}
	}
	return nil
}

func (is *Issue) setStatus(status string) {
	is.Status = status
	is.History = append(is.History, status)
}

func (is *Issue) clone() Issue {
	c := *is
	c.Labels = slices.Clone(is.Labels)
	c.Comments = slices.Clone(is.Comments)
	c.Attachments = slices.Clone(is.Attachments)
	c.History = slices.Clone(is.History)
	return c
}

func (s *Server) newIssue() *Issue {
	is := &Issue{Key: fmt.Sprintf("%s-%d", project, len(s.issues)+1), Status: defaultStatus, History: []string{defaultStatus}}
	s.issues = append(s.issues, is)
	return is
}
