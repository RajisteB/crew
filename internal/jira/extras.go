package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
)

// BoardInfo is one board of a project.
type BoardInfo struct {
	ID   int
	Name string
}

// Attach uploads body to the issue as a file called name.
func (c *Client) Attach(ctx context.Context, key, name string, body io.Reader) error {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormFile("file", name)
	if err != nil {
		return fmt.Errorf("jira attach %s: %w", key, err)
	}
	if _, err := io.Copy(part, body); err != nil {
		return fmt.Errorf("jira attach %s: read file: %w", key, err)
	}
	if err := mw.Close(); err != nil {
		return fmt.Errorf("jira attach %s: %w", key, err)
	}
	_, err = c.send(ctx, http.MethodPost, issuePath(key, "/attachments"), &buf,
		mw.FormDataContentType(), map[string]string{"X-Atlassian-Token": "no-check"})
	return err
}

// FlagField returns the id of the field named Flagged.
func (c *Client) FlagField(ctx context.Context) (id string, found bool, err error) {
	b, err := c.do(ctx, http.MethodGet, "/rest/api/3/field", nil)
	if err != nil {
		return "", false, err
	}
	var fields []struct{ ID, Name string }
	if err := json.Unmarshal(b, &fields); err != nil {
		return "", false, fmt.Errorf("jira GET /rest/api/3/field: decode reply: %w", err)
	}
	for _, f := range fields {
		if f.Name == "Flagged" {
			return f.ID, true, nil
		}
	}
	return "", false, nil
}

// SetFlag sets (Impediment) or clears the flag field of an issue.
func (c *Client) SetFlag(ctx context.Context, key, field string, on bool) error {
	var value any
	if on {
		value = []map[string]string{{"value": "Impediment"}}
	}
	_, err := c.do(ctx, http.MethodPut, issuePath(key), map[string]any{"fields": map[string]any{field: value}})
	return err
}

// ActiveSprint returns the board's active sprint, if it has one.
func (c *Client) ActiveSprint(ctx context.Context, board int) (id int, name string, found bool, err error) {
	path := "/rest/agile/1.0/board/" + strconv.Itoa(board) + "/sprint?state=active"
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, "", false, err
	}
	var r struct {
		Values []struct {
			ID   int
			Name string
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return 0, "", false, fmt.Errorf("jira GET %s: decode reply: %w", path, err)
	}
	if len(r.Values) == 0 {
		return 0, "", false, nil
	}
	return r.Values[0].ID, r.Values[0].Name, true, nil
}

// AddToSprint puts the issues in the sprint.
func (c *Client) AddToSprint(ctx context.Context, sprint int, keys []string) error {
	_, err := c.do(ctx, http.MethodPost, "/rest/agile/1.0/sprint/"+strconv.Itoa(sprint)+"/issue",
		map[string]any{"issues": keys})
	return err
}

// Boards lists the boards of a project.
func (c *Client) Boards(ctx context.Context, project string) ([]BoardInfo, error) {
	path := "/rest/agile/1.0/board?projectKeyOrId=" + url.QueryEscape(project)
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var r struct{ Values []BoardInfo }
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("jira GET %s: decode reply: %w", path, err)
	}
	return r.Values, nil
}

// Statuses returns the status names of each issue type of a project.
func (c *Client) Statuses(ctx context.Context, project string) (map[string][]string, error) {
	path := "/rest/api/3/project/" + url.PathEscape(project) + "/statuses"
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var types []struct {
		Name     string
		Statuses []struct{ Name string }
	}
	if err := json.Unmarshal(b, &types); err != nil {
		return nil, fmt.Errorf("jira GET %s: decode reply: %w", path, err)
	}
	out := make(map[string][]string, len(types))
	for _, t := range types {
		names := make([]string, 0, len(t.Statuses))
		for _, s := range t.Statuses {
			names = append(names, s.Name)
		}
		out[t.Name] = names
	}
	return out, nil
}
