package jiratest

import (
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

var jqlPattern = regexp.MustCompile(`^project = "(.*)" AND labels = "(.*)" ORDER BY created ASC$`)

// serve records the request, applies Fail, then dispatches.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	r.Body = io.NopCloser(strings.NewReader(string(body)))

	s.mu.Lock()
	s.requests = append(s.requests, r.Method+" "+r.URL.Path)
	s.bodies = append(s.bodies, string(body))
	s.headers = append(s.headers, r.Header.Clone())
	failing := s.fail.times > 0
	status := s.fail.status
	if failing {
		s.fail.times--
	}
	s.mu.Unlock()

	if failing {
		http.Error(w, "injected failure", status)
		return
	}
	s.routes().ServeHTTP(w, r)
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /rest/api/3/myself", s.myself)
	mux.HandleFunc("POST /rest/api/3/issue", s.create)
	mux.HandleFunc("POST /rest/api/3/search/jql", s.search)
	mux.HandleFunc("GET /rest/api/3/issue/{key}", s.status)
	mux.HandleFunc("PUT /rest/api/3/issue/{key}", s.edit)
	mux.HandleFunc("GET /rest/api/3/issue/{key}/transitions", s.transitions)
	mux.HandleFunc("POST /rest/api/3/issue/{key}/transitions", s.transition)
	mux.HandleFunc("POST /rest/api/3/issue/{key}/comment", s.comment)
	mux.HandleFunc("POST /rest/api/3/issue/{key}/attachments", s.attach)
	mux.HandleFunc("GET /rest/api/3/field", s.fields)
	mux.HandleFunc("GET /rest/api/3/project/{project}/statuses", s.projectStatuses)
	mux.HandleFunc("GET /rest/agile/1.0/board", s.listBoards)
	mux.HandleFunc("GET /rest/agile/1.0/board/{board}/sprint", s.activeSprint)
	mux.HandleFunc("POST /rest/agile/1.0/sprint/{sprint}/issue", s.addToSprint)
	return mux
}

func reply(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func fail(w http.ResponseWriter, code int, msg string) {
	reply(w, code, map[string]any{"errorMessages": []string{msg}})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		fail(w, http.StatusBadRequest, "bad JSON: "+err.Error())
		return false
	}
	return true
}

// withIssue runs fn on the issue named in the path, under the lock; 404 if absent.
func (s *Server) withIssue(w http.ResponseWriter, r *http.Request, fn func(*Issue)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	is := s.find(r.PathValue("key"))
	if is == nil {
		fail(w, http.StatusNotFound, "Issue does not exist")
		return
	}
	fn(is)
}

func (s *Server) myself(w http.ResponseWriter, _ *http.Request) {
	reply(w, http.StatusOK, map[string]string{"accountId": accountID, "displayName": accountName})
}

type createBody struct {
	Fields struct {
		Project     struct{ Key string }
		IssueType   struct{ Name string } `json:"issuetype"`
		Summary     string
		Description adfDoc
		Labels      []string
		Assignee    *struct {
			AccountID string `json:"accountId"`
		}
		Parent *struct{ Key string }
	}
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var b createBody
	if !decode(w, r, &b) {
		return
	}
	f := b.Fields
	if f.Project.Key != project {
		fail(w, http.StatusBadRequest, "unknown project "+f.Project.Key)
		return
	}
	if !slices.Contains(s.issueType, f.IssueType.Name) {
		fail(w, http.StatusBadRequest, "unknown issue type "+f.IssueType.Name)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	is := s.newIssue()
	is.Type, is.Summary, is.Description, is.Labels = f.IssueType.Name, f.Summary, f.Description.text(), f.Labels
	if f.Assignee != nil {
		is.Assignee = f.Assignee.AccountID
	}
	if f.Parent != nil {
		is.Parent = f.Parent.Key
	}
	reply(w, http.StatusCreated, map[string]string{"id": strconv.Itoa(len(s.issues)), "key": is.Key})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	var b struct{ JQL string }
	if !decode(w, r, &b) {
		return
	}
	m := jqlPattern.FindStringSubmatch(b.JQL)
	if m == nil {
		fail(w, http.StatusBadRequest, "unsupported JQL")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	found := []map[string]string{}
	for _, is := range s.issues {
		if m[1] == project && slices.Contains(is.Labels, m[2]) {
			found = append(found, map[string]string{"key": is.Key})
			break
		}
	}
	reply(w, http.StatusOK, map[string]any{"issues": found})
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	s.withIssue(w, r, func(is *Issue) {
		reply(w, http.StatusOK, map[string]any{
			"key":    is.Key,
			"fields": map[string]any{"status": map[string]string{"name": is.Status}},
		})
	})
}

func (s *Server) edit(w http.ResponseWriter, r *http.Request) {
	var b struct{ Fields map[string]json.RawMessage }
	if !decode(w, r, &b) {
		return
	}
	s.withIssue(w, r, func(is *Issue) {
		raw, ok := b.Fields[flagFieldID]
		if !ok || !s.flagField {
			fail(w, http.StatusBadRequest, "unknown field")
			return
		}
		if string(raw) == "null" {
			is.Flagged = false
		} else {
			is.Flagged = true
			is.Flags++
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

type transition struct {
	ID string            `json:"id"`
	To map[string]string `json:"to"`
}

func (s *Server) transitions(w http.ResponseWriter, r *http.Request) {
	s.withIssue(w, r, func(*Issue) {
		out := make([]transition, 0, len(s.statuses))
		for i, st := range s.statuses {
			out = append(out, transition{ID: strconv.Itoa(i + 1), To: map[string]string{"name": st}})
		}
		reply(w, http.StatusOK, map[string]any{"transitions": out})
	})
}

func (s *Server) transition(w http.ResponseWriter, r *http.Request) {
	var b struct{ Transition struct{ ID string } }
	if !decode(w, r, &b) {
		return
	}
	s.withIssue(w, r, func(is *Issue) {
		n, err := strconv.Atoi(b.Transition.ID)
		if err != nil || n < 1 || n > len(s.statuses) {
			fail(w, http.StatusBadRequest, "no such transition")
			return
		}
		is.setStatus(s.statuses[n-1])
		w.WriteHeader(http.StatusNoContent)
	})
}

func (s *Server) comment(w http.ResponseWriter, r *http.Request) {
	var b struct{ Body adfDoc }
	if !decode(w, r, &b) {
		return
	}
	s.withIssue(w, r, func(is *Issue) {
		is.Comments = append(is.Comments, b.Body.text())
		reply(w, http.StatusCreated, map[string]string{"id": strconv.Itoa(len(is.Comments))})
	})
}

func (s *Server) attach(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Atlassian-Token") != "no-check" {
		fail(w, http.StatusForbidden, "XSRF check failed")
		return
	}
	if err := r.ParseMultipartForm(8 << 20); err != nil {
		fail(w, http.StatusBadRequest, "bad multipart: "+err.Error())
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) != 1 {
		fail(w, http.StatusBadRequest, "want one part named file")
		return
	}
	s.withIssue(w, r, func(is *Issue) {
		is.Attachments = append(is.Attachments, files[0].Filename)
		reply(w, http.StatusOK, []map[string]string{{"filename": files[0].Filename}})
	})
}

func (s *Server) fields(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []map[string]string{{"id": "summary", "name": "Summary"}}
	if s.flagField {
		out = append(out, map[string]string{"id": flagFieldID, "name": "Flagged"})
	}
	reply(w, http.StatusOK, out)
}

func (s *Server) projectStatuses(w http.ResponseWriter, r *http.Request) {
	if r.PathValue("project") != project {
		fail(w, http.StatusNotFound, "No project could be found")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	names := make([]map[string]string, 0, len(s.statuses))
	for _, st := range s.statuses {
		names = append(names, map[string]string{"name": st})
	}
	out := make([]map[string]any, 0, len(s.issueType))
	for _, t := range s.issueType {
		out = append(out, map[string]any{"name": t, "statuses": names})
	}
	reply(w, http.StatusOK, out)
}

func (s *Server) listBoards(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]int, 0, len(s.boards))
	for id := range s.boards {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	values := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		values = append(values, map[string]any{"id": id, "name": "Board " + strconv.Itoa(id)})
	}
	reply(w, http.StatusOK, map[string]any{"values": values})
}

func (s *Server) activeSprint(w http.ResponseWriter, r *http.Request) {
	board, _ := strconv.Atoi(r.PathValue("board"))
	s.mu.Lock()
	defer s.mu.Unlock()
	sprint, ok := s.boards[board]
	if !ok {
		fail(w, http.StatusNotFound, "board does not exist")
		return
	}
	values := []map[string]any{}
	if sprint != 0 {
		values = append(values, map[string]any{"id": sprint, "name": "Sprint " + strconv.Itoa(sprint), "state": "active"})
	}
	reply(w, http.StatusOK, map[string]any{"values": values})
}

func (s *Server) addToSprint(w http.ResponseWriter, r *http.Request) {
	sprint, err := strconv.Atoi(r.PathValue("sprint"))
	var b struct{ Issues []string }
	if err != nil || !decode(w, r, &b) {
		if err != nil {
			fail(w, http.StatusBadRequest, "bad sprint")
		}
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	targets := make([]*Issue, 0, len(b.Issues))
	for _, k := range b.Issues {
		is := s.find(k)
		if is == nil {
			fail(w, http.StatusBadRequest, "issue does not exist: "+k)
			return
		}
		targets = append(targets, is)
	}
	for _, is := range targets {
		is.Sprint = sprint
	}
	w.WriteHeader(http.StatusNoContent)
}
