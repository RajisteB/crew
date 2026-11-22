package gh

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Check states.
const (
	CheckPass    = "pass"
	CheckFail    = "fail"
	CheckPending = "pending"
	CheckSkip    = "skip"
)

const (
	failedLogLines = 40
	failedLogRunes = 2000
)

// Check is one check run or commit status on a pull request's head.
// State is "pass", "fail", "pending" or "skip".
type Check struct{ Name, State, URL string }

// rollupEntry is one statusCheckRollup element: a CheckRun or a StatusContext.
type rollupEntry struct {
	Typename   string `json:"__typename"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	DetailsURL string `json:"detailsUrl"`
	Context    string `json:"context"`
	State      string `json:"state"`
	TargetURL  string `json:"targetUrl"`
}

var actionsJob = regexp.MustCompile(`/actions/runs/\d+/job/(\d+)`)

func checksFromRollup(entries []rollupEntry) []Check {
	var checks []Check
	for _, e := range entries {
		checks = append(checks, e.check())
	}
	return checks
}

func (e rollupEntry) check() Check {
	switch e.Typename {
	case "CheckRun":
		return Check{Name: e.Name, State: checkRunState(e.Status, e.Conclusion), URL: e.DetailsURL}
	case "StatusContext":
		return Check{Name: e.Context, State: statusState(e.State), URL: e.TargetURL}
	}
	name := e.Name
	if name == "" {
		name = e.Context
	}
	return Check{Name: name, State: CheckPending}
}

func checkRunState(status, conclusion string) string {
	if status != "COMPLETED" {
		return CheckPending
	}
	switch conclusion {
	case "SUCCESS":
		return CheckPass
	case "NEUTRAL", "SKIPPED":
		return CheckSkip
	}
	return CheckFail
}

func statusState(state string) string {
	switch state {
	case "SUCCESS":
		return CheckPass
	case "PENDING", "EXPECTED":
		return CheckPending
	}
	return CheckFail
}

// MergePR merges pull request number with method (squash, merge or rebase),
// only if its head is still headSHA.
func (c Client) MergePR(ctx context.Context, number int, method, headSHA string) error {
	if !slices.Contains([]string{"squash", "merge", "rebase"}, method) {
		return fmt.Errorf("gh pr merge: unknown merge method %q", method)
	}
	if headSHA == "" {
		return errors.New("gh pr merge: refusing to merge without a head commit")
	}
	_, err := c.run(ctx, nil, "pr", "merge", strconv.Itoa(number), "--"+method, "--match-head-commit", headSHA)
	return err
}

// FailedLog is the end of a failed GitHub Actions job's log: the last 40
// lines, cut to 2000 runes. It is "" and nil when check.URL is not an Actions job.
func (c Client) FailedLog(ctx context.Context, check Check) (string, error) {
	m := actionsJob.FindStringSubmatch(check.URL)
	if m == nil {
		return "", nil
	}
	out, err := c.run(ctx, nil, "run", "view", "--job", m[1], "--log-failed")
	if err != nil {
		return "", err
	}
	log := tail(string(out), failedLogLines)
	if r := []rune(log); len(r) > failedLogRunes {
		log = string(r[len(r)-failedLogRunes:])
	}
	return log, nil
}

// HasChecks reports whether branch's latest commit has a check run or a status.
func (c Client) HasChecks(ctx context.Context, branch string) (bool, error) {
	base := "repos/{owner}/{repo}/commits/" + branch
	runs, err := c.count(ctx, base+"/check-runs", ".total_count")
	if err != nil {
		return false, err
	}
	if runs > 0 {
		return true, nil
	}
	statuses, err := c.count(ctx, base+"/status", ".statuses | length")
	return statuses > 0, err
}

func (c Client) count(ctx context.Context, path, jq string) (int, error) {
	args := []string{"api", path, "--jq", jq}
	out, err := c.run(ctx, nil, args...)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(strings.TrimSpace(string(out)))
	if convErr != nil {
		return 0, unparseable(args, convErr)
	}
	return n, nil
}
