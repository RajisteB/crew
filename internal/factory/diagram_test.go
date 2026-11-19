package factory_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/crewfile-dev/crew/internal/factory"
	"github.com/crewfile-dev/crew/internal/source"
)

const shipChangeMermaid = `flowchart LR
  start(( )) --> s_plan
  s_plan("plan<br/><small>epic-planner</small>")
  s_build("build<br/><small>ticket-worker</small>")
  s_review{{"review<br/><small>code-explorer</small>"}}
  t_done(["done"])
  s_plan -->|"ready"| s_build
  s_build -->|"done"| s_review
  s_build -.->|"replan ≤2"| s_plan
  s_build -.->|"drift"| s_plan
  s_review -.->|"fix ≤*"| s_build
  s_review -->|"pass"| t_done
  s_review -.->|"redesign ≤1"| s_plan
`

func templateFactory(t *testing.T) source.Factory {
	t.Helper()
	_, here, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(here), "..", "scaffold", "template", "factories", "ship-change.md")
	f, err := source.ParseFactory(p)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestMermaid_ShipChange(t *testing.T) {
	got := factory.Mermaid(templateFactory(t))
	if got != shipChangeMermaid {
		t.Fatalf("Mermaid mismatch.\ngot:\n%s\nwant:\n%s", got, shipChangeMermaid)
	}
}

func TestMermaid_FailTargetDrawnOnlyWhenUsed(t *testing.T) {
	f := fac("a", map[string]st{"a": {Agent: "x", Routes: rs{"ok": to("done"), "bad": to("fail")}}})
	got := factory.Mermaid(f)
	want := `flowchart LR
  start(( )) --> s_a
  s_a("a<br/><small>x</small>")
  t_done(["done"])
  t_fail(["fail"])
  s_a -->|"bad"| t_fail
  s_a -->|"ok"| t_done
`
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
