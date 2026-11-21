package notify

import (
	"reflect"
	"testing"
)

type call struct {
	name string
	args []string
}

func TestNew_DarwinPassesTextAsArgv(t *testing.T) {
	var calls []call
	n := New("darwin", func(name string, args ...string) error {
		calls = append(calls, call{name, append([]string(nil), args...)})
		return nil
	})
	title := `Mission "done"`
	body := `said "hi" \ and \" end`
	if err := n.Notify(title, body); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if len(calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(calls))
	}
	c := calls[0]
	if c.name != "osascript" {
		t.Errorf("name = %q, want osascript", c.name)
	}
	want := []string{
		"-e", "on run argv",
		"-e", "display notification (item 2 of argv) with title (item 1 of argv)",
		"-e", "end run",
		title, body,
	}
	if !reflect.DeepEqual(c.args, want) {
		t.Errorf("args = %q, want %q", c.args, want)
	}
}

func TestNew_OtherOSIsNop(t *testing.T) {
	called := false
	n := New("linux", func(string, ...string) error { called = true; return nil })
	if _, ok := n.(Nop); !ok {
		t.Fatalf("New(linux) = %T, want Nop", n)
	}
	if err := n.Notify("t", "b"); err != nil {
		t.Errorf("Notify: %v", err)
	}
	if called {
		t.Error("exec called on non-darwin")
	}
}
