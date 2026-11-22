// Package notify shows desktop notifications. Only macOS is supported;
// every other platform gets a no-op notifier.
package notify

import (
	"fmt"
	"os/exec"

	"github.com/crewfile-dev/crew/internal/config"
)

// Notifier shows a short message to the person running crew.
type Notifier interface {
	Notify(title, body string) error
}

// Nop is a Notifier that does nothing.
type Nop struct{}

// Notify implements Notifier and always returns nil.
func (Nop) Notify(string, string) error { return nil }

type osascript struct {
	run func(name string, args ...string) error
}

// New returns a Notifier for goos. "darwin" uses osascript through run;
// any other value returns Nop. A nil run uses a real os/exec runner.
func New(goos string, run func(name string, args ...string) error) Notifier {
	if goos != "darwin" {
		return Nop{}
	}
	if run == nil {
		run = realRun
	}
	return osascript{run: run}
}

// Notify passes title and body as trailing argv items so they are never
// part of the AppleScript text.
func (o osascript) Notify(title, body string) error {
	err := o.run("osascript",
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		title, body,
	)
	if err != nil {
		return fmt.Errorf("notify: osascript: %w", err)
	}
	return nil
}

func realRun(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = config.CleanEnv()
	return cmd.Run()
}
