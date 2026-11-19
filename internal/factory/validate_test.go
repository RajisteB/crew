package factory_test

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/factory"
	"github.com/crewfile-dev/crew/internal/source"
)

func to(t string) source.Route { return source.Route{To: t} }
func capped(t string, n int) source.Route {
	return source.Route{To: t, Max: source.Limit{Set: true, N: n}}
}
func star(t string) source.Route {
	return source.Route{To: t, Max: source.Limit{Set: true, Unlimited: true}}
}

type st = source.Station
type rs = map[string]source.Route

func fac(start string, stations map[string]source.Station) source.Factory {
	return source.Factory{Name: "f", Start: start, Stations: stations}
}

// shipChange is the spec's example: valid as written.
func shipChange() source.Factory {
	return fac("plan", map[string]source.Station{
		"plan":   {Agent: "p", Routes: rs{"ready": to("build")}},
		"build":  {Agent: "b", OnDrift: "plan", Routes: rs{"done": to("review"), "replan": capped("plan", 2)}},
		"review": {Agent: "r", Approve: true, Routes: rs{"pass": to("done"), "fix": star("build"), "redesign": capped("plan", 1)}},
	})
}

func errorsOf(is []factory.Issue) []string {
	var out []string
	for _, i := range is {
		if !i.Info {
			out = append(out, i.Msg)
		}
	}
	return out
}

func infosOf(is []factory.Issue) []string {
	var out []string
	for _, i := range is {
		if i.Info {
			out = append(out, i.Msg)
		}
	}
	return out
}

func TestValidate_ShipChangeIsClean(t *testing.T) {
	is := factory.Validate(shipChange())
	if e := errorsOf(is); len(e) != 0 {
		t.Fatalf("unexpected errors: %v", e)
	}
	infos := strings.Join(infosOf(is), "\n")
	if !strings.Contains(infos, `review.fix loops without limit ("*")`) {
		t.Fatalf("want star info, got %q", infos)
	}
}

func TestValidate(t *testing.T) {
	cases := []struct {
		name string
		f    source.Factory
		want string // substring of an error; "" = no errors
	}{
		{"missing start", fac("", map[string]st{"a": {Routes: rs{"ok": to("done")}}}), "start is required"},
		{"unknown start", fac("x", map[string]st{"a": {Routes: rs{"ok": to("done")}}}), `start "x" is not a station`},
		{"unknown target", fac("a", map[string]st{"a": {Routes: rs{"ok": to("nowhere")}}}), `a.ok goes to "nowhere"`},
		{"fail route must go to fail", fac("a", map[string]st{"a": {Routes: rs{"ok": to("done"), "fail": to("a")}}}), `route "fail" is reserved`},
		{"no routes", fac("a", map[string]st{"a": {Routes: rs{"ok": to("b")}}, "b": {}}), "station b has no routes"},
		{"unreachable", fac("a", map[string]st{"a": {Routes: rs{"ok": to("done")}}, "b": {Routes: rs{"ok": to("done")}}}), "station b can't be reached from start"},
		{"drift makes reachable", fac("a", map[string]st{"a": {OnDrift: "b", Routes: rs{"ok": to("done")}}, "b": {Routes: rs{"ok": to("done")}}}), ""},
		{"unknown on_drift", fac("a", map[string]st{"a": {OnDrift: "zz", Routes: rs{"ok": to("done")}}}), `on_drift "zz" is not a station`},
		{"reserved station name", fac("done", map[string]st{"done": {Routes: rs{"ok": to("fail")}}}), `"done" is reserved`},
		{"bad station name", fac("A_b", map[string]st{"A_b": {Routes: rs{"ok": to("done")}}}), `station name "A_b"`},
		{"self-loop needs max", fac("build", map[string]st{"build": {Routes: rs{"retry": to("build"), "ok": to("done")}}}), "loop with no limit: build →retry→ build"},
		{"uncapped two-step loop", fac("a", map[string]st{
			"a": {Routes: rs{"go": to("b")}},
			"b": {Routes: rs{"back": to("a"), "ok": to("done")}},
		}), "loop with no limit"},
		{"cap on either edge is enough (back edge)", fac("a", map[string]st{
			"a": {Routes: rs{"x": to("b"), "y": to("c")}},
			"b": {Routes: rs{"x": to("c")}},
			"c": {Routes: rs{"x": capped("b", 2), "ok": to("done")}},
		}), ""},
		{"cap on either edge is enough (forward edge)", fac("a", map[string]st{
			"a": {Routes: rs{"x": to("b"), "y": to("c")}},
			"b": {Routes: rs{"x": capped("c", 2)}},
			"c": {Routes: rs{"x": to("b"), "ok": to("done")}},
		}), ""},
		{"two cycles one capped", fac("a", map[string]st{
			"a": {Routes: rs{"x": to("b")}},
			"b": {Routes: rs{"back": capped("a", 1), "self": to("b"), "ok": to("done")}},
		}), "loop with no limit: b →self→ b"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := errorsOf(factory.Validate(c.f))
			if c.want == "" {
				if len(errs) > 0 {
					t.Fatalf("unexpected errors: %v", errs)
				}
				return
			}
			for _, e := range errs {
				if strings.Contains(e, c.want) {
					return
				}
			}
			t.Fatalf("want error containing %q, got %v", c.want, errs)
		})
	}
}

func TestValidate_UselessMaxIsInfo(t *testing.T) {
	f := fac("a", map[string]st{"a": {Routes: rs{"ok": capped("done", 3)}}})
	is := factory.Validate(f)
	if e := errorsOf(is); len(e) > 0 {
		t.Fatalf("errors: %v", e)
	}
	if !strings.Contains(strings.Join(infosOf(is), "\n"), "max on a.ok has no effect") {
		t.Fatalf("infos: %v", infosOf(is))
	}
}

func TestIsBackEdge(t *testing.T) {
	f := shipChange()
	if !factory.IsBackEdge(f, "review", "fix") {
		t.Fatal("review.fix closes a cycle")
	}
	if factory.IsBackEdge(f, "review", "pass") {
		t.Fatal("review.pass goes to done")
	}
	if !factory.IsBackEdge(f, "plan", "ready") {
		t.Fatal("plan.ready is on the plan→build→plan cycle")
	}
}
