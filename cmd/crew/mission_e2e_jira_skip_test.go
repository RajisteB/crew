package main

import (
	"net/http"
	"slices"
	"testing"

	"github.com/crewfile-dev/crew/internal/jira"
	"github.com/crewfile-dev/crew/internal/mission"
)

// TestMission_EndToEnd_RefusedAttachmentsHoldUpNothing: the site refuses
// every upload with 403 for the whole mission; every other write still gets
// through, so each issue and the epic reach Done.
func TestMission_EndToEnd_RefusedAttachmentsHoldUpNothing(t *testing.T) {
	r := newJiraE2E(t, true)
	for _, key := range []string{"ACME-2", "ACME-3"} { // P1's and P2's issues
		r.srv.FailOn(http.MethodPost, "/rest/api/3/issue/"+key+"/attachments", http.StatusForbidden, 1<<30)
	}
	r.launchDeliverables()
	for _, id := range []string{"P1", "P2"} {
		st := r.tickUntil(id+"'s deliverable given up and in review", func(s mission.State) bool {
			p := piece(t, s, id)
			return p.Deliverable != nil && slices.Equal(p.Deliverable.Attached, p.Deliverable.Files) && p.Jira.Lane == mission.LaneInReview
		})
		r.srv.SetStatus(piece(t, st, id).Jira.Issue, "Done")
	}
	st := r.tickUntil("Jira to catch up", func(s mission.State) bool {
		return s.Status == mission.StatusDone && s.Jira.EpicLane == mission.LaneDone
	})
	for _, is := range r.srv.Issues() {
		if is.Status != "Done" || len(is.Attachments) != 0 {
			t.Errorf("%s: status %s, attachments %v", is.Key, is.Status, is.Attachments)
		}
	}
	if len(r.srv.Issues()) != 3 || st.Jira.Epic != "ACME-1" {
		t.Fatalf("issues %+v epic %q", r.srv.Issues(), st.Jira.Epic)
	}
}

func TestSetTracker_SetsTrackerDown(t *testing.T) {
	dir := t.TempDir()
	setJiraEnv(t, testEmail, testToken)
	cfg := (jira.Config{Version: 1}).With("acme/app", jira.Repo{Site: testSite, Project: "ACME"})
	if err := jira.SaveConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	e := &mission.Engine{}
	if err := setTracker(e, dir, "acme/app"); err != nil {
		t.Fatal(err)
	}
	if e.TrackerDown == nil || !e.TrackerDown(&jira.Error{Status: 503}) || e.TrackerDown(&jira.Error{Status: 403}) {
		t.Fatal("TrackerDown is not jira.IsDown")
	}
}
