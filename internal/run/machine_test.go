package run_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

func to(t string) source.Route { return source.Route{To: t} }
func capped(t string, n int) source.Route {
	return source.Route{To: t, Max: source.Limit{Set: true, N: n}}
}
func star(t string) source.Route {
	return source.Route{To: t, Max: source.Limit{Set: true, Unlimited: true}}
}

type rs = map[string]source.Route

// shipChange: plan → build → review(approve) with capped back-routes.
func shipChange() source.Factory {
	return source.Factory{Name: "ship-change", Start: "plan", Stations: map[string]source.Station{
		"plan":   {Agent: "p", Routes: rs{"ready": to("build")}},
		"build":  {Agent: "b", OnDrift: "plan", Routes: rs{"done": to("review"), "replan": capped("plan", 2)}},
		"review": {Agent: "r", Routes: rs{"pass": to("done"), "fix": star("build"), "redesign": capped("plan", 1)}},
	}}
}

func v(route string) run.Verdict { return run.Verdict{Route: route, Summary: "s-" + route} }

func walk(t *testing.T, f source.Factory, routes ...string) (run.State, run.Decision) {
	t.Helper()
	s := run.Begin(f, "id")
	var d run.Decision
	for _, r := range routes {
		s, d = run.AfterStation(f, s, v(r))
	}
	return s, d
}

func TestBegin(t *testing.T) {
	s := run.Begin(shipChange(), "id-1")
	if s.Status != run.StatusRunning || s.Current != "plan" || s.RunID != "id-1" || s.Factory != "ship-change" || s.Step != 0 {
		t.Fatalf("Begin = %+v", s)
	}
}

func TestAfterStation_ForwardAndBack(t *testing.T) {
	f := shipChange()
	s, d := walk(t, f, "ready", "done", "fix")
	if d.Kind != run.RunStation || d.Station != "build" || s.Current != "build" || s.Step != 3 {
		t.Fatalf("after fix: s=%+v d=%+v", s, d)
	}
	if s.RouteCounts["review.fix"] != 1 || s.RouteCounts["plan.ready"] != 1 {
		t.Fatalf("counts = %v", s.RouteCounts)
	}
	s, d = run.AfterStation(f, s, v("done"))
	s, d = run.AfterStation(f, s, v("pass"))
	if d.Kind != run.Finished || s.Status != run.StatusDone {
		t.Fatalf("after pass: s=%+v d=%+v", s, d)
	}
}

func TestAfterStation_LimitExceeded(t *testing.T) {
	f := shipChange()
	s, d := walk(t, f, "ready", "replan", "ready", "replan")
	if d.Kind != run.RunStation {
		t.Fatalf("second replan should still be allowed: %+v", d)
	}
	s, d = run.AfterStation(f, s, v("ready"))
	s, d = run.AfterStation(f, s, v("replan"))
	if d.Kind != run.Failed || s.Status != run.StatusFailed {
		t.Fatalf("third replan must fail: s=%+v d=%+v", s, d)
	}
	if !strings.Contains(s.Reason, "build.replan hit its limit (2)") || s.Retryable {
		t.Fatalf("reason = %q retryable=%v", s.Reason, s.Retryable)
	}
}

func TestAfterStation_UnlimitedNeverFails(t *testing.T) {
	f := shipChange()
	s, _ := walk(t, f, "ready", "done")
	var d run.Decision
	for i := 0; i < 1000; i++ {
		s, d = run.AfterStation(f, s, v("fix"))
		s, d = run.AfterStation(f, s, v("done"))
	}
	if d.Kind != run.RunStation || s.RouteCounts["review.fix"] != 1000 {
		t.Fatalf("after 1000 fixes: d=%+v counts=%v", d, s.RouteCounts)
	}
}

func TestAfterStation_FailRouteImplicit(t *testing.T) {
	s, d := walk(t, shipChange(), "ready", "fail")
	if d.Kind != run.Failed || s.Status != run.StatusFailed || s.Reason != "s-fail" || s.Retryable {
		t.Fatalf("s=%+v d=%+v", s, d)
	}
}

func approvalFactory() source.Factory {
	f := shipChange()
	r := f.Stations["review"]
	r.Approve = true
	f.Stations["review"] = r
	return f
}

func TestAfterStation_ApprovalPauses(t *testing.T) {
	s, d := walk(t, approvalFactory(), "ready", "done", "fix")
	if d.Kind != run.AwaitApproval || s.Status != run.StatusAwaitingApproval {
		t.Fatalf("s=%+v d=%+v", s, d)
	}
	if s.Pending == nil || s.Pending.Station != "review" || s.Pending.Route != "fix" || s.Current != "review" {
		t.Fatalf("pending = %+v current=%s", s.Pending, s.Current)
	}
}

func TestApprove_FollowsOrRedirects(t *testing.T) {
	f := approvalFactory()
	paused, _ := walk(t, f, "ready", "done", "fix")
	s, d, err := run.Approve(f, paused, "")
	if err != nil || d.Kind != run.RunStation || d.Station != "build" || s.Pending != nil || s.Status != run.StatusRunning {
		t.Fatalf("approve: s=%+v d=%+v err=%v", s, d, err)
	}
	s, d, err = run.Approve(f, paused, "plan")
	if err != nil || d.Station != "plan" || s.Current != "plan" {
		t.Fatalf("approve --goto plan: s=%+v d=%+v err=%v", s, d, err)
	}
	if _, _, err := run.Approve(f, paused, "nowhere"); err == nil {
		t.Fatal("goto unknown station must error")
	}
	if _, _, err := run.Approve(f, run.Begin(f, "x"), ""); err == nil {
		t.Fatal("approve on a running run must error")
	}
}

func TestApprove_DoesNotDoubleCount(t *testing.T) {
	f := approvalFactory()
	paused, _ := walk(t, f, "ready", "done", "fix")
	s, _, _ := run.Approve(f, paused, "")
	if s.RouteCounts["review.fix"] != 1 {
		t.Fatalf("count = %d, want 1", s.RouteCounts["review.fix"])
	}
}

func TestApprove_PassToDoneFinishes(t *testing.T) {
	f := approvalFactory()
	paused, _ := walk(t, f, "ready", "done", "pass")
	s, d, err := run.Approve(f, paused, "")
	if err != nil || d.Kind != run.Finished || s.Status != run.StatusDone {
		t.Fatalf("s=%+v d=%+v err=%v", s, d, err)
	}
}

func TestBeforeStation_DriftRedirectsAndIsExempt(t *testing.T) {
	f := shipChange()
	s, _ := walk(t, f, "ready") // current = build, which has on_drift: plan
	before := s.RouteCounts["build.replan"]
	s2, d := run.BeforeStation(f, s, []string{"."})
	if d.Kind != run.RunStation || d.Station != "plan" || s2.Current != "plan" {
		t.Fatalf("drift: s=%+v d=%+v", s2, d)
	}
	if !strings.Contains(s2.Reason, "drift in . before build") || s2.RouteCounts["build.replan"] != before {
		t.Fatalf("reason=%q counts=%v", s2.Reason, s2.RouteCounts)
	}
	_, d = run.BeforeStation(f, s, nil)
	if d.Station != "build" {
		t.Fatalf("no drift should run build, got %+v", d)
	}
}

func TestBeforeStation_NoGuardIgnoresDrift(t *testing.T) {
	f := shipChange()
	s := run.Begin(f, "x") // plan has no on_drift
	_, d := run.BeforeStation(f, s, []string{"."})
	if d.Station != "plan" {
		t.Fatalf("got %+v", d)
	}
}

func TestBeforeStation_SkipDriftOnceAfterResume(t *testing.T) {
	f := shipChange()
	s, _ := walk(t, f, "ready")
	s = run.Interrupt(s, "ctrl-c")
	s, d := run.Resume(f, s)
	if d.Kind != run.RunStation || d.Station != "build" || !s.SkipDriftOnce {
		t.Fatalf("resume: s=%+v d=%+v", s, d)
	}
	s, d = run.BeforeStation(f, s, []string{"."})
	if d.Station != "build" || s.SkipDriftOnce {
		t.Fatalf("first station after resume must skip drift once: s=%+v d=%+v", s, d)
	}
}

func TestResume_RetryableFailureRerunsCurrent(t *testing.T) {
	f := shipChange()
	s, _ := walk(t, f, "ready")
	s = run.FailRetryable(s, "claude: not found")
	if s.Status != run.StatusFailed || !s.Retryable {
		t.Fatalf("FailRetryable = %+v", s)
	}
	s, d := run.Resume(f, s)
	if d.Kind != run.RunStation || d.Station != "build" || s.Status != run.StatusRunning || s.Retryable {
		t.Fatalf("resume: s=%+v d=%+v", s, d)
	}
}

func TestResume_LimitFailureStaysFailed(t *testing.T) {
	f := shipChange()
	s, _ := walk(t, f, "ready", "fail")
	_, d := run.Resume(f, s)
	if d.Kind != run.Failed {
		t.Fatalf("got %+v", d)
	}
	done, _ := walk(t, f, "ready", "done", "pass")
	if _, d := run.Resume(f, done); d.Kind != run.Finished {
		t.Fatalf("done run resume = %+v", d)
	}
}

func TestResume_AwaitingApproval(t *testing.T) {
	paused, _ := walk(t, approvalFactory(), "ready", "done", "fix")
	if _, d := run.Resume(approvalFactory(), paused); d.Kind != run.AwaitApproval {
		t.Fatalf("got %+v", d)
	}
}

func TestGoto(t *testing.T) {
	f := shipChange()
	s, _ := walk(t, f, "ready")
	s2, err := run.Goto(f, s, "review")
	if err != nil || s2.Current != "review" {
		t.Fatalf("goto: %+v %v", s2, err)
	}
	if _, err := run.Goto(f, s, "zz"); err == nil {
		t.Fatal("unknown station must error")
	}
}

func TestMachine_DoesNotMutateInput(t *testing.T) {
	f := approvalFactory()
	s, _ := walk(t, f, "ready", "done")
	snapshot := func(x run.State) run.State {
		c := x
		c.RouteCounts = map[string]int{}
		for k, v := range x.RouteCounts {
			c.RouteCounts[k] = v
		}
		c.Snapshot = map[string]string{}
		for k, v := range x.Snapshot {
			c.Snapshot[k] = v
		}
		return c
	}
	s.Snapshot = map[string]string{".": "abc"}
	orig := snapshot(s)
	run.AfterStation(f, s, v("fix"))
	run.BeforeStation(f, s, []string{"."})
	run.Interrupt(s, "x")
	run.Goto(f, s, "plan")
	if !reflect.DeepEqual(snapshot(s), orig) || s.Status != orig.Status || s.Current != orig.Current {
		t.Fatalf("input state mutated:\n got %+v\nwant %+v", s, orig)
	}
}
