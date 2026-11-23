package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCLI_FlagsAVerbDoesNotTakeAreRefused(t *testing.T) {
	e, id, statePath := stoppedPlainRun(t)
	before := readFile(t, statePath)
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"resume", id, "--goto", "plan"}, "crew run resume doesn't take --goto"},
		{[]string{"say", id, "hi", "--goto", "plan"}, "crew run say doesn't take --goto"},
		{[]string{"say", id, "hi", "--wait", "5s"}, "crew run say doesn't take --wait"},
		{[]string{"goto", id, "review", "--note", "x"}, "crew run goto doesn't take --note"},
		{[]string{"status", id, "--detach"}, "crew run status doesn't take --detach"},
		{[]string{"ship-change", "--input", "x", "--since", "3"}, "crew run <factory> doesn't take --since"},
	}
	for i, c := range cases {
		out, code := e.crew(append([]string{"run"}, c.args...)...)
		if code != 2 || !strings.Contains(out, "crew: "+c.want) {
			t.Errorf("%v: exit %d, want 2 and %q:\n%s", c.args, code, c.want, out)
		}
		if i == 0 && !bytes.Equal(before, readFile(t, statePath)) {
			t.Errorf("%v changed state.json", c.args)
		}
	}
}

// Every command form the orchestrator's prompt uses gets past the flag check.
func TestCLI_ThePromptsCommandsAreAllAccepted(t *testing.T) {
	cases := [][]string{
		{"ship-change", "--detach", "--input", "-"},
		{"status", "<id>", "--since", "0", "--wait", "1ms"},
		{"say", "<id>", "-"},
		{"goto", "<id>", "review"},
		{"approve", "<id>", "--detach", "--goto", "review", "--note", "-"},
		{"resume", "<id>", "--detach"},
		{"status", "--since", "3", "--project", "."},
	}
	for _, c := range cases {
		args := strings.Join(c, " ")
		pos, f, err := parseRunArgs(strings.Fields(strings.ReplaceAll(args, "<id>", "some-run")))
		if err != nil {
			t.Errorf("%s: parse: %v", args, err)
			continue
		}
		if err := checkVerbFlags(pos, f.set); err != nil {
			t.Errorf("%s: refused: %v", args, err)
		}
	}
}
