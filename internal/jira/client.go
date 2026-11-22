// Package jira is a small Jira Cloud REST client: the calls crew needs to keep a
// board in step with a mission, and nothing more.
package jira

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	requestTimeout = 15 * time.Second
	maxReplyBytes  = 1 << 20
	maxErrorBytes  = 300
)

// secret holds the API token. It prints as "[redacted]" however it is formatted.
type secret struct{ value string }

func (*secret) String() string   { return "[redacted]" }
func (*secret) GoString() string { return "[redacted]" }

// Client talks to one Jira Cloud site. Its HTTP client times out after 15s and
// refuses every redirect, so credentials never travel past Base.
type Client struct {
	Base, Email string // Base has no trailing slash
	HTTP        *http.Client
	token       *secret // a pointer, so reflection-based printing shows an address
}

// New returns a client for the site at base.
func New(base, email, token string) *Client {
	return &Client{
		Base:  strings.TrimRight(base, "/"),
		Email: email,
		HTTP: &http.Client{
			Timeout: requestTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		token: &secret{value: token},
	}
}

// Error is a reply outside 200-299. Body is the first 300 bytes of the reply,
// cut on a rune boundary.
type Error struct {
	Method, Path string
	Status       int
	Body         string
}

func (e *Error) Error() string {
	return fmt.Sprintf("jira %s %s: HTTP %d: %s", e.Method, e.Path, e.Status, e.Body)
}

// ErrNoTransition means the issue cannot move to the wanted status from where it is.
var ErrNoTransition = errors.New("the issue has no transition to that status from where it is")

// Permanent reports whether retrying can't help: ErrNoTransition, or an *Error
// whose status is 400-499 except 401, 403, 408 and 429.
func Permanent(err error) bool {
	if errors.Is(err, ErrNoTransition) {
		return true
	}
	var je *Error
	if !errors.As(err, &je) {
		return false
	}
	switch je.Status {
	case 401, 403, 408, 429:
		return false
	}
	return je.Status >= 400 && je.Status < 500
}

// do sends payload (nil for none) as JSON and returns the reply body.
func (c *Client) do(ctx context.Context, method, path string, payload any) ([]byte, error) {
	if payload == nil {
		return c.send(ctx, method, path, nil, "", nil)
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("jira %s %s: encode body: %w", method, path, err)
	}
	return c.send(ctx, method, path, bytes.NewReader(b), "application/json", nil)
}

// send is the one place a request leaves the process.
func (c *Client) send(ctx context.Context, method, path string, body io.Reader, contentType string, extra map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, body)
	if err != nil {
		return nil, fmt.Errorf("jira %s %s: %w", method, path, err)
	}
	req.SetBasicAuth(c.Email, c.token.value)
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	reply, err := io.ReadAll(io.LimitReader(resp.Body, maxReplyBytes))
	if err != nil {
		return nil, fmt.Errorf("jira %s %s: read reply: %w", method, path, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, &Error{Method: method, Path: path, Status: resp.StatusCode, Body: clip(reply, maxErrorBytes)}
	}
	return reply, nil
}

// clip returns the first n bytes of b, cut back to a rune boundary.
func clip(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	b = b[:n]
	for len(b) > 0 && !utf8.Valid(b) {
		b = b[:len(b)-1]
	}
	return string(b)
}

// Myself returns the account the token belongs to.
func (c *Client) Myself(ctx context.Context) (accountID, name string, err error) {
	b, err := c.do(ctx, http.MethodGet, "/rest/api/3/myself", nil)
	if err != nil {
		return "", "", err
	}
	var r struct {
		AccountID   string `json:"accountId"`
		DisplayName string `json:"displayName"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return "", "", fmt.Errorf("jira GET /rest/api/3/myself: decode reply: %w", err)
	}
	return r.AccountID, r.DisplayName, nil
}
