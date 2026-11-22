package jiratest

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

func get(t *testing.T, s *Server, path string) (int, string) {
	t.Helper()
	resp, err := http.Get(s.URL() + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestNew_StartsEmpty(t *testing.T) {
	s := New(t)
	if len(s.Issues()) != 0 {
		t.Fatal("new server has issues")
	}
	if code, body := get(t, s, "/rest/agile/1.0/board?projectKeyOrId=ACME"); code != 200 || !strings.Contains(body, `"values":[]`) {
		t.Fatalf("boards = %d %s", code, body)
	}
	if code, body := get(t, s, "/rest/api/3/myself"); code != 200 || !strings.Contains(body, "acc-1") || !strings.Contains(body, "Test User") {
		t.Fatalf("myself = %d %s", code, body)
	}
}

func TestMissingIssueIs404(t *testing.T) {
	s := New(t)
	if code, _ := get(t, s, "/rest/api/3/issue/ACME-1?fields=status"); code != http.StatusNotFound {
		t.Fatalf("code = %d, want 404", code)
	}
}

func TestFail_AppliesToTheNextRequestsOnly(t *testing.T) {
	s := New(t)
	s.Fail(503, 2)
	for i, want := range []int{503, 503, 200} {
		if code, _ := get(t, s, "/rest/api/3/myself"); code != want {
			t.Fatalf("request %d: code = %d, want %d", i, code, want)
		}
	}
	if got := len(s.Requests()); got != 3 {
		t.Fatalf("recorded %d requests, want 3", got)
	}
}

func TestDropStatus_RemovesItEverywhere(t *testing.T) {
	s := New(t)
	s.DropStatus("In Review")
	_, body := get(t, s, "/rest/api/3/project/ACME/statuses")
	if strings.Contains(body, "In Review") {
		t.Fatalf("still listed: %s", body)
	}
}

func TestFailOn_FailsOnlyMatchingRequests(t *testing.T) {
	s := New(t)
	s.FailOn(http.MethodGet, "/rest/api/3/field", 403, 2)
	for i, want := range []int{403, 200, 403, 200} {
		path := "/rest/api/3/field"
		if i%2 == 1 {
			path = "/rest/api/3/myself"
		}
		if code, _ := get(t, s, path); code != want {
			t.Fatalf("request %d (%s): code = %d, want %d", i, path, code, want)
		}
	}
	if code, _ := get(t, s, "/rest/api/3/field"); code != 200 {
		t.Fatalf("after two failures: code = %d, want 200", code)
	}
}
