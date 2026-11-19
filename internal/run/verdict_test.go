package run_test

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
)

var allowed = []string{"pass", "fix"}

func TestParseReply_OK(t *testing.T) {
	reply := "Looked at it.\nAll good.\n\n```verdict\n{\"route\": \"pass\", \"summary\": \"looks right\", \"notes\": [\"assumed UTC\"]}\n```\n"
	out, v, err := run.ParseReply(reply, allowed)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Looked at it.\nAll good.\n" || v.Route != "pass" || v.Summary != "looks right" || len(v.Notes) != 1 || v.Notes[0] != "assumed UTC" {
		t.Fatalf("out=%q v=%+v", out, v)
	}
}

func TestParseReply_LastBlockWins(t *testing.T) {
	reply := "Example of the format:\n```verdict\n{\"route\": \"fix\", \"summary\": \"example\"}\n```\nReal one:\n```verdict\n{\"route\": \"pass\", \"summary\": \"real\"}\n```"
	out, v, err := run.ParseReply(reply, allowed)
	if err != nil || v.Route != "pass" {
		t.Fatalf("v=%+v err=%v", v, err)
	}
	if !strings.Contains(out, "\"summary\": \"example\"") || strings.Contains(out, "\"real\"") {
		t.Fatalf("only the last block should be removed; out=%q", out)
	}
}

func TestParseReply_Errors(t *testing.T) {
	cases := map[string]string{
		"no block":     "just text",
		"bad json":     "```verdict\n{route: pass}\n```",
		"unknown":      "```verdict\n{\"route\": \"ship\"}\n```",
		"empty route":  "```verdict\n{\"route\": \"\"}\n```",
		"unterminated": "```verdict\n{\"route\": \"pass\"}\n",
	}
	want := map[string]string{
		"no block":     "no verdict block",
		"bad json":     "verdict is not valid JSON",
		"unknown":      `verdict route "ship" is not one of: pass, fix, fail`,
		"empty route":  "verdict route is empty",
		"unterminated": "no verdict block",
	}
	for name, reply := range cases {
		_, _, err := run.ParseReply(reply, allowed)
		if err == nil || !strings.HasPrefix(err.Error(), want[name]) {
			t.Errorf("%s: got %v, want prefix %q", name, err, want[name])
		}
	}
}

func TestParseReply_FailAlwaysAllowed(t *testing.T) {
	_, v, err := run.ParseReply("```verdict\n{\"route\": \"fail\", \"summary\": \"input unusable\"}\n```", allowed)
	if err != nil || v.Route != "fail" {
		t.Fatalf("v=%+v err=%v", v, err)
	}
}
