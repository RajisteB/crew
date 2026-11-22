package main

import (
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/mission"
)

func TestTrackerFor(t *testing.T) {
	dir := t.TempDir()
	env := func(k string) string {
		return map[string]string{"CREW_JIRA_EMAIL": testEmail, "CREW_JIRA_TOKEN": testToken}[k]
	}
	none := func(string) string { return "" }
	tr, sprints, err := trackerFor(dir, "acme/app", env)
	if tr != nil || sprints || err != nil {
		t.Fatalf("no entry: %v %v %v", tr, sprints, err)
	}
	cfg := (jira.Config{Version: 1}).
		With("acme/app", jira.Repo{Site: testSite, Project: "ACME"}).
		With("acme/boarded", jira.Repo{Site: testSite, Project: "ACME", Board: 3})
	if err := jira.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	tr, sprints, err = trackerFor(dir, "acme/app", env)
	if _, ok := tr.(*jira.Board); !ok || sprints || err != nil {
		t.Fatalf("entry: %T %v %v", tr, sprints, err)
	}
	tr, sprints, err = trackerFor(dir, "acme/boarded", env)
	if _, ok := tr.(*jira.Board); !ok || !sprints || err != nil {
		t.Fatalf("board: %T %v %v", tr, sprints, err)
	}
	tr, _, err = trackerFor(dir, "acme/app", none)
	u, ok := tr.(jira.Unavailable)
	if !ok || err != nil || !strings.Contains(u.Reason, "CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are not set where this engine was started") {
		t.Fatalf("no credentials: %T %v %v", tr, u, err)
	}
}

func TestStartChecks_JiraWithoutCredentials(t *testing.T) {
	r := newJiraRig(t)
	id := r.plan()
	out, err := r.cmd("start", id)
	if err != nil {
		t.Fatalf("no entry: %v\n%s", err, out)
	}
	r.saveEntry(0)
	setJiraEnv(t, testEmail, "")
	id = r.plan()
	_, err = r.cmd("start", id)
	want := "Jira is set up for acme/app, but CREW_JIRA_TOKEN is not set here. Run this command yourself in a terminal where CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are set."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if n := len(r.srv.Requests()); n != 0 {
		t.Fatalf("%d requests were sent", n)
	}
}

func TestMissionPlan_PrintsJiraLine(t *testing.T) {
	r := newJiraRig(t)
	r.stdin = string(planFile("Build.", designPiece("P1")))
	out, err := r.cmd("plan")
	if err != nil || !strings.Contains(out, "Jira: not set up for acme/app; deliverables stay in the mission folder (crew jira setup)\n") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	r.saveEntry(0)
	r.stdin = string(planFile("Build.", designPiece("P1")))
	out, err = r.cmd("plan")
	if err != nil || !strings.Contains(out, "Jira: project ACME on https://example.atlassian.net\n") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if i, j := strings.Index(out, "Auto-merge in"), strings.Index(out, "Jira:"); i < 0 || j < i {
		t.Fatalf("the Jira line is not after the auto-merge line:\n%s", out)
	}
}

func finishMission(r *jiraRig, id string) {
	r.edit(id, func(st *mission.State) {
		st.Status = mission.StatusDone
		for i := range st.Pieces {
			st.Pieces[i].Status = mission.PieceComplete
		}
	})
}

func TestJiraSync_SendsWhatIsOwed(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	finishMission(r, id)
	out, err := r.jira("sync", id)
	if err != nil || out != "Jira is up to date for mission "+id+"\n" {
		t.Fatalf("err = %v\n%q", err, out)
	}
	issues := r.srv.Issues()
	if len(issues) != 3 {
		t.Fatalf("issues = %+v", issues)
	}
	for _, is := range issues {
		if is.Status != "Done" {
			t.Fatalf("%s is %s", is.Key, is.Status)
		}
	}
}

func TestJiraSync_ReportsWhatIsStillOwed(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	finishMission(r, id)
	r.srv.Fail(500, 1000)
	out, err := r.jira("sync", id)
	if err != nil || !strings.Contains(out, "1 write still not sent; see the notes in crew mission status "+id) {
		t.Fatalf("err = %v\n%q", err, out)
	}
}

func TestJiraSync_RefusesWithALiveEngine(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	finishMission(r, id)
	unlock, err := r.store(id).Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	_, err = r.jira("sync", id)
	want := "mission " + id + " has a live engine, which sends Jira writes itself"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	if n := len(r.srv.Issues()); n != 0 {
		t.Fatalf("%d issues", n)
	}
}

func TestJiraSync_NotSetUp(t *testing.T) {
	r := newJiraRig(t)
	id := r.plan()
	_, err := r.jira("sync", id)
	if err == nil || !strings.Contains(err.Error(), "Jira is not set up for acme/app") {
		t.Fatalf("err = %v", err)
	}
}

func TestJiraUsage(t *testing.T) {
	r := newJiraRig(t)
	if _, err := r.jira(); err == nil || !strings.Contains(err.Error(), "crew jira setup --site URL --key KEY") {
		t.Fatalf("err = %v", err)
	}
	if _, err := r.jira("bogus"); err == nil {
		t.Fatal("want error")
	}
}

func TestJiraSync_RefusesWithoutCredentials(t *testing.T) {
	r := newJiraRig(t)
	r.saveEntry(0)
	id := r.plan()
	finishMission(r, id)
	setJiraEnv(t, "", testToken)
	unlock, err := r.store(id).Lock()
	if err != nil {
		t.Fatal(err)
	}
	unlock() // a lock taken now would show as a changed lock file only through use; sync must not need it
	before, _ := r.store(id).LoadState()
	_, err = r.jira("sync", id)
	want := "Jira is set up for acme/app, but CREW_JIRA_EMAIL is not set here. Run this command yourself in a terminal where CREW_JIRA_EMAIL and CREW_JIRA_TOKEN are set."
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v", err)
	}
	after, _ := r.store(id).LoadState()
	if before.Jira != after.Jira || len(r.srv.Requests()) != 0 {
		t.Fatal("sync changed something")
	}
}
