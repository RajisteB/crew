package jira

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// Issue is what crew sets on a new issue. Assignee and Parent may be empty.
type Issue struct {
	Project, Type, Summary, Description, Assignee, Parent string
	Labels                                                []string
}

// adf renders text as an Atlassian Document: one paragraph per line, an empty
// line being a paragraph with no content.
func adf(text string) map[string]any {
	lines := strings.Split(text, "\n")
	paras := make([]map[string]any, 0, len(lines))
	for _, l := range lines {
		p := map[string]any{"type": "paragraph"}
		if l != "" {
			p["content"] = []map[string]any{{"type": "text", "text": l}}
		}
		paras = append(paras, p)
	}
	return map[string]any{"type": "doc", "version": 1, "content": paras}
}

func issuePath(key string, rest ...string) string {
	return "/rest/api/3/issue/" + url.PathEscape(key) + strings.Join(rest, "")
}

// Create makes an issue and returns its key.
func (c *Client) Create(ctx context.Context, in Issue) (key string, err error) {
	fields := map[string]any{
		"project":     map[string]string{"key": in.Project},
		"issuetype":   map[string]string{"name": in.Type},
		"summary":     in.Summary,
		"description": adf(in.Description),
		"labels":      in.Labels,
	}
	if in.Labels == nil {
		fields["labels"] = []string{}
	}
	if in.Assignee != "" {
		fields["assignee"] = map[string]string{"accountId": in.Assignee}
	}
	if in.Parent != "" {
		fields["parent"] = map[string]string{"key": in.Parent}
	}
	b, err := c.do(ctx, http.MethodPost, "/rest/api/3/issue", map[string]any{"fields": fields})
	if err != nil {
		return "", err
	}
	var r struct{ Key string }
	if err := json.Unmarshal(b, &r); err != nil || r.Key == "" {
		return "", fmt.Errorf("jira POST /rest/api/3/issue: reply has no key")
	}
	return r.Key, nil
}

// FindByLabel returns the oldest issue in project that carries label.
func (c *Client) FindByLabel(ctx context.Context, project, label string) (key string, found bool, err error) {
	jql := fmt.Sprintf("project = %q AND labels = %q ORDER BY created ASC", project, label)
	b, err := c.do(ctx, http.MethodPost, "/rest/api/3/search/jql", map[string]any{
		"jql": jql, "fields": []string{"summary"}, "maxResults": 1,
	})
	if err != nil {
		return "", false, err
	}
	var r struct {
		Issues []struct{ Key string }
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", false, fmt.Errorf("jira POST /rest/api/3/search/jql: decode reply: %w", err)
	}
	if len(r.Issues) == 0 {
		return "", false, nil
	}
	return r.Issues[0].Key, true, nil
}

// Status returns the name of the issue's status.
func (c *Client) Status(ctx context.Context, key string) (string, error) {
	path := issuePath(key) + "?fields=status"
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		Fields struct{ Status struct{ Name string } }
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("jira GET %s: decode reply: %w", path, err)
	}
	return r.Fields.Status.Name, nil
}

// MoveTo moves the issue to status (case-insensitive); nil if it is already there.
func (c *Client) MoveTo(ctx context.Context, key, status string) error {
	cur, err := c.Status(ctx, key)
	if err != nil {
		return err
	}
	if strings.EqualFold(cur, status) {
		return nil
	}
	id, err := c.transitionTo(ctx, key, status)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, issuePath(key, "/transitions"),
		map[string]any{"transition": map[string]string{"id": id}})
	return err
}

func (c *Client) transitionTo(ctx context.Context, key, status string) (string, error) {
	path := issuePath(key, "/transitions")
	b, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return "", err
	}
	var r struct {
		Transitions []struct {
			ID string
			To struct{ Name string }
		}
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", fmt.Errorf("jira GET %s: decode reply: %w", path, err)
	}
	for _, t := range r.Transitions {
		if strings.EqualFold(t.To.Name, status) {
			return t.ID, nil
		}
	}
	return "", ErrNoTransition
}

// Comment adds a comment, one paragraph per line of text.
func (c *Client) Comment(ctx context.Context, key, text string) error {
	_, err := c.do(ctx, http.MethodPost, issuePath(key, "/comment"), map[string]any{"body": adf(text)})
	return err
}
