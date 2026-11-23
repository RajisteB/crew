package mission

import (
	"testing"
)

func planBlock(tag, body string) string { return "```" + tag + "\n" + body + "\n```" }

func TestParseSubPlan(t *testing.T) {
	good := planBlock("plan", `{"pieces":[{"id":"a","brief":"x","waits_for":[]}],"reason":"r"}`)
	sp, err := ParseSubPlan("text\n" + good)
	if err != nil || len(sp.Pieces) != 1 || sp.Pieces[0].ID != "a" || sp.Reason != "r" {
		t.Fatalf("sp = %+v, err = %v", sp, err)
	}
	two := planBlock("plan", `{"question":"first"}`) + "\nmore\n" + planBlock("plan", `{"question":"second"}`)
	if sp, err := ParseSubPlan(two); err != nil || sp.Question != "second" {
		t.Errorf("last block: %+v, %v", sp, err)
	}
	bad := map[string]string{
		"no block":      "nothing here",
		"unknown field": planBlock("plan", `{"pieces":[],"extra":1}`),
		"two objects":   planBlock("plan", `{"question":"a"}{"question":"b"}`),
		"decision":      planBlock("decision", `{"action":"retry"}`),
	}
	for name, reply := range bad {
		if _, err := ParseSubPlan(reply); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
}
