package run

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/crewfile-dev/crew/internal/config"
)

// StationRequest is one station to run on one harness.
type StationRequest struct {
	Harness, Agent, Prompt, WorkDir string
	Allowed                         []string
	Timeout                         time.Duration
}

// Runner runs a station and returns its reply.
type Runner interface {
	Run(ctx context.Context, req StationRequest) (string, error)
}

// HeadlessRunner runs stations through each harness's headless: command.
type HeadlessRunner struct{ Registry *config.Registry }

// errCanceled marks a station stopped because the run itself was stopped.
var errCanceled = errors.New("station stopped: run interrupted")

// IsCanceled reports whether a Run error means the run was interrupted.
func IsCanceled(err error) bool { return errors.Is(err, errCanceled) }

func (r HeadlessRunner) Run(ctx context.Context, req StationRequest) (string, error) {
	h, ok := r.Registry.Harnesses[req.Harness]
	if !ok || h.Headless == nil {
		return "", fmt.Errorf("%s: no headless: command in harnesses.yaml", req.Harness)
	}
	argv := expandCommand(*h.Headless, req)
	if _, err := exec.LookPath(argv[0]); err != nil {
		return "", fmt.Errorf("%s: command %q not found on PATH (headless.command in harnesses.yaml)", req.Harness, argv[0])
	}
	tctx, cancel := context.WithTimeout(ctx, req.Timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, argv[0], argv[1:]...)
	cmd.Dir = req.WorkDir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	if h.Headless.PromptVia == "stdin" {
		cmd.Stdin = strings.NewReader(req.Prompt)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return "", errCanceled
	case tctx.Err() != nil:
		return "", fmt.Errorf("%s station timed out after %v", req.Harness, req.Timeout)
	case err != nil:
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("%s station failed (exit %d): %s", req.Harness, ee.ExitCode(), tail(stderr.String(), 20))
		}
		return "", fmt.Errorf("%s station: %w", req.Harness, err)
	}
	return stdout.String(), nil
}

// expandCommand fills in {agent}, {prompt} and {allowed}. An empty
// allowlist drops the {allowed} argument and the flag right before it.
func expandCommand(h config.HeadlessSpec, req StationRequest) []string {
	join := h.AllowJoin
	if join == "" {
		join = ","
	}
	allowed := strings.Join(req.Allowed, join)
	var out []string
	for _, a := range h.Command {
		if a == "{allowed}" && allowed == "" {
			if n := len(out); n > 0 && strings.HasPrefix(out[n-1], "-") {
				out = out[:n-1]
			}
			continue
		}
		a = strings.ReplaceAll(a, "{agent}", req.Agent)
		a = strings.ReplaceAll(a, "{allowed}", allowed)
		a = strings.ReplaceAll(a, "{prompt}", req.Prompt)
		out = append(out, a)
	}
	return out
}

func tail(s string, n int) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
