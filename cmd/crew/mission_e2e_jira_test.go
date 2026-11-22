package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/jira/jiratest"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/principal"
)

// jiraE2E is the end-to-end rig with the repo's Jira project set up and the
// local Jira behind it.
type jiraE2E struct {
	*e2eRig
	srv *jiratest.Server
}

// newJiraE2E sets Jira up for acme/app. With deliverables the fake station
// leaves a deliverable and commits nothing.
func newJiraE2E(t *testing.T, deliverables bool) *jiraE2E {
	t.Helper()
	r := &jiraE2E{e2eRig: newE2ERig(t), srv: jiratest.New(t)}
	if deliverables {
		editFile(t, filepath.Join(r.crewDir, "harnesses.yaml"), "fakeharness-commit.sh", "fakeharness-deliverable.sh")
		t.Setenv("FAKE_MARK_DIR", t.TempDir())
	}
	old := newJiraClient
	newJiraClient = func(_ jira.Repo, email, token string) *jira.Client { return jira.New(r.srv.URL(), email, token) }
	t.Cleanup(func() { newJiraClient = old })
	setJiraEnv(t, testEmail, testToken)
	cfg := (jira.Config{Version: 1}).With("acme/app", jira.Repo{Site: testSite, Project: "ACME"})
	if err := jira.SaveConfig(r.crewDir, cfg); err != nil {
		t.Fatal(err)
	}
	r.confirmRepo()
	return r
}

func (r *jiraE2E) mergePR() {
	r.t.Helper()
	if err := os.WriteFile(filepath.Join(r.ghDir, "pr-state"), []byte("MERGED\n"), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func (r *jiraE2E) issueOf(st mission.State, id string) jiratest.Issue {
	r.t.Helper()
	is, ok := r.srv.Issue(piece(r.t, st, id).Jira.Issue)
	if !ok {
		r.t.Fatalf("piece %s has no issue on the board", id)
	}
	return is
}

func (r *jiraE2E) status() string {
	r.t.Helper()
	return r.cmd("", "status", r.id)
}

// finishWithPRs runs a two-code-piece mission to its end, merging each PR.
func (r *jiraE2E) finishWithPRs() mission.State {
	r.t.Helper()
	for _, id := range []string{"P1", "P2"} {
		r.tickUntil(id+"'s merge wait", func(s mission.State) bool { return piece(r.t, s, id).Wait != nil })
		r.mergePR()
	}
	return r.tickUntil("the mission to be done", func(s mission.State) bool { return s.Status == mission.StatusDone })
}

func TestMission_EndToEnd_JiraBoard(t *testing.T) {
	r := newJiraE2E(t, false)
	r.launch()
	st := r.finishWithPRs()
	st = r.tickUntil("Jira to catch up", func(s mission.State) bool {
		return s.Jira.EpicLane == mission.LaneDone && piece(t, s, "P2").Jira.Lane == mission.LaneDone
	})

	issues := r.srv.Issues()
	if len(issues) != 3 {
		t.Fatalf("want an epic and two issues, got %+v", issues)
	}
	epic, ok := r.srv.Issue(st.Jira.Epic)
	if !ok || epic.Type != "Epic" || epic.Status != "Done" {
		t.Fatalf("epic = %+v", epic)
	}
	for _, id := range []string{"P1", "P2"} {
		is := r.issueOf(st, id)
		if is.Assignee == "" || !slices.Contains(is.Labels, mission.TrackerLabel(r.id, id)) {
			t.Errorf("%s: assignee %q, labels %v", id, is.Assignee, is.Labels)
		}
		if !slices.ContainsFunc(is.Comments, func(c string) bool { return strings.HasPrefix(c, "Pull request opened: ") }) {
			t.Errorf("%s: no pull request comment in %q", id, is.Comments)
		}
		if is.Flags < 1 || is.Flagged {
			t.Errorf("%s: flagged %d times, flagged now %v", id, is.Flags, is.Flagged)
		}
		if is.Status != "Done" {
			t.Errorf("%s ends in %s", id, is.Status)
		}
	}
	if got := strings.Join(r.issueOf(st, "P1").History, ", "); got != "To Do, In Progress, In Review, Done" {
		t.Fatalf("P1 history = %s", got)
	}
}

// deliverableMission runs P1 (deliverable only) and P2 (waits for P1) up to
// the point where P1 has left its deliverable.
func (r *jiraE2E) launchDeliverables() {
	r.launchPieces(e2ePiece("P1"), e2ePiece("P2", "P1"))
}

func TestMission_EndToEnd_Deliverable(t *testing.T) {
	r := newJiraE2E(t, true)
	r.launchDeliverables()
	st := r.tickUntil("P1's deliverable on the board", func(s mission.State) bool {
		p := piece(t, s, "P1")
		return p.Wait != nil && p.Deliverable != nil && slices.Equal(p.Deliverable.Attached, p.Deliverable.Files) && p.Jira.Lane == mission.LaneInReview
	})
	if w := piece(t, st, "P1").Wait; w.Kind != mission.WaitDeliverable {
		t.Fatalf("wait = %+v", w)
	}
	p1 := r.issueOf(st, "P1")
	if !slices.Contains(p1.Attachments, "brief.md") || p1.Status != "In Review" {
		t.Fatalf("P1 issue: %+v", p1)
	}
	out := r.status()
	if !strings.Contains(out, "  P1  deliverable  ") || !strings.Contains(out, "brief.md  waiting on you") {
		t.Fatalf("status:\n%s", out)
	}
	r.srv.SetStatus(piece(t, st, "P1").Jira.Issue, "Done")
	r.tickUntil("P1 complete and P2 started", func(s mission.State) bool {
		return piece(t, s, "P1").Status == mission.PieceComplete && piece(t, s, "P2").Status != mission.PieceQueued
	})
	r.tickUntil("P2's deliverable", func(s mission.State) bool { return piece(t, s, "P2").Deliverable != nil })
	saw, err := os.ReadFile(filepath.Join(os.Getenv("FAKE_MARK_DIR"), "saw-input"))
	if err != nil || string(saw) != "the brief\n" {
		t.Fatalf("P2 did not read P1's brief: %q, %v", saw, err)
	}
}

func TestMission_EndToEnd_DeliverableAcceptedBySwitch(t *testing.T) {
	r := newJiraE2E(t, true)
	store := principal.Store{Dir: filepath.Join(r.crewDir, "principal")}
	rec := principal.SwitchRecord{Repo: "acme/app", Domain: "design", Mode: principal.AutoOn, SetAt: time.Now()}
	if err := store.SetSwitches([]principal.SwitchRecord{rec}); err != nil {
		t.Fatal(err)
	}
	r.launchDeliverables()
	r.tickUntil("P1 complete without a wait", func(s mission.State) bool {
		if piece(t, s, "P1").Wait != nil {
			t.Fatalf("a wait was opened for P1: %+v", piece(t, s, "P1").Wait)
		}
		return piece(t, s, "P1").Status == mission.PieceComplete
	})
	if p := piece(t, r.tick(), "P1"); p.Deliverable == nil || !p.Deliverable.Accepted || p.Deliverable.By != "engine" {
		t.Fatalf("deliverable = %+v", p.Deliverable)
	}
}

func TestMission_EndToEnd_JiraDown(t *testing.T) {
	r := newJiraE2E(t, false)
	r.srv.Fail(500, 1<<30)
	r.launch()
	st := r.finishWithPRs()
	if st.Jira.Epic != "" || len(r.srv.Issues()) != 0 {
		t.Fatalf("something reached the board: %+v, %+v", st.Jira, r.srv.Issues())
	}
	if out := r.status(); !strings.Contains(out, "writes not sent yet)") {
		t.Fatalf("status has no not-sent count:\n%s", out)
	}
	r.srv.Fail(0, 0)
	var out bytes.Buffer
	args := []string{"sync", r.id, "--dir", r.crewDir, "--project", r.repo}
	if err := jiraCmd(args, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	issues := r.srv.Issues()
	if len(issues) != 3 {
		t.Fatalf("issues = %+v", issues)
	}
	for _, is := range issues {
		if is.Status != "Done" {
			t.Errorf("%s is %s", is.Key, is.Status)
		}
	}
	if out := r.status(); strings.Contains(out, "not sent yet") {
		t.Fatalf("still owed:\n%s", out)
	}
}
