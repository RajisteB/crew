package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/mission"
)

func TestMissionPlan_CreatesMissionAndPrintsDiagram(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	out, _ := r.cmd("status")
	if !strings.Contains(out, id) {
		t.Fatalf("mission not listed:\n%s", out)
	}
	if r.status(id) != mission.StatusPlanned {
		t.Fatalf("status = %s", r.status(id))
	}
	r.stdin = string(planFile("Build the thing.", designPiece("P1")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"planned", "P1", "design", "ship-change", "```mermaid", "flowchart TD"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestMissionPlan_CheckErrorsCreateNothing(t *testing.T) {
	r := newMissionRig(t)
	bad := mission.Piece{ID: "P1", Domain: "nope", Factory: "ship-change", Brief: "x"}
	r.stdin = string(planFile("Build.", bad, designPiece("P1")))
	out, err := r.cmd("plan")
	if err == nil {
		t.Fatalf("want an error:\n%s", out)
	}
	for _, want := range []string{`unknown domain "nope"`, "used more than once"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	list, _ := mission.List(r.root())
	if len(list) != 0 {
		t.Fatalf("created %d missions", len(list))
	}
}

func TestMissionStart_UnconfirmedRepoRefused(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	if err := os.RemoveAll(filepath.Join(r.crewDir, "principal")); err != nil {
		t.Fatal(err)
	}
	_, err := r.cmd("start", id)
	if err == nil || !strings.Contains(err.Error(), "crew repo confirm") {
		t.Fatalf("err = %v", err)
	}
	if r.status(id) != mission.StatusPlanned {
		t.Fatalf("status = %s", r.status(id))
	}
}

func TestMissionStart_VerifyOrCheckFailureStaysPlanned(t *testing.T) {
	t.Run("check", func(t *testing.T) {
		r := newMissionRig(t)
		id := r.plan()
		if err := os.Remove(filepath.Join(r.crewDir, "domains", "design.md")); err != nil {
			t.Fatal(err)
		}
		out, err := r.cmd("start", id)
		if err == nil || !strings.Contains(out+err.Error(), `unknown domain "design"`) {
			t.Fatalf("err = %v\n%s", err, out)
		}
		if r.status(id) != mission.StatusPlanned || len(r.starts) != 0 {
			t.Fatalf("status = %s, starts = %d", r.status(id), len(r.starts))
		}
	})
	t.Run("verify", func(t *testing.T) {
		r := newMissionRig(t)
		id := r.plan()
		if err := os.WriteFile(filepath.Join(r.crewDir, "harnesses.yaml"), []byte("::: not yaml"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := r.cmd("start", id); err == nil || !strings.Contains(err.Error(), "verify") {
			t.Fatalf("err = %v", err)
		}
		if r.status(id) != mission.StatusPlanned {
			t.Fatalf("status = %s", r.status(id))
		}
	})
}

func TestMissionStart_ApprovalOnWaitsForGo(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	out, err := r.cmd("start", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "waiting for go: crew mission go "+id) || !strings.Contains(out, "P2") {
		t.Fatalf("output:\n%s", out)
	}
	if r.status(id) != mission.StatusAwaitingGo || len(r.starts) != 0 {
		t.Fatalf("status = %s, starts = %d", r.status(id), len(r.starts))
	}
}

func TestMissionStart_ApprovalOffGoes(t *testing.T) {
	r := newMissionRig(t)
	r.setApproval("off")
	id := r.plan()
	out, err := r.cmd("start", id)
	if err != nil {
		t.Fatal(err)
	}
	if r.status(id) != mission.StatusRunning || len(r.starts) != 1 || !strings.Contains(out, "crew mission status "+id) {
		t.Fatalf("status = %s, starts = %v\n%s", r.status(id), r.starts, out)
	}
}

func TestMissionGo_SavesApprovedAndStartsRunning(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	if _, err := r.cmd("go", id); err == nil {
		t.Fatal("go on a planned mission should be refused")
	}
	if _, err := r.cmd("start", id); err != nil {
		t.Fatal(err)
	}
	out, err := r.cmd("go", id)
	if err != nil {
		t.Fatal(err)
	}
	store := r.store(id)
	if _, err := os.Stat(filepath.Join(store.Dir, "approved.md")); err != nil {
		t.Fatalf("approved.md: %v", err)
	}
	if r.status(id) != mission.StatusRunning || len(r.starts) != 1 {
		t.Fatalf("status = %s, starts = %d", r.status(id), len(r.starts))
	}
	call := r.starts[0]
	if call.id != id || call.dir != r.crewDir || call.logPath != filepath.Join(store.Dir, "mission.log") {
		t.Fatalf("start call = %+v", call)
	}
	for _, want := range []string{"4242", "follow with: crew mission status " + id} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if got := r.eventKinds(id); len(got) == 0 || got[0] != mission.EvMissionStart {
		t.Fatalf("events = %v", got)
	}
}

func TestMissionStatus_WaitingOnYouComesFirst(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	since := time.Now().Add(-2*time.Hour - 10*time.Minute - 20*time.Second)
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceWaiting
		s.Pieces[0].Wait = &mission.Wait{ID: "w1", Kind: mission.WaitQuestion, Question: "which database?", Since: since}
	})
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"w1", "P1", "question", "which database?", "2h10m"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	waiting, queued, cursor := strings.Index(out, "Waiting on you"), strings.Index(out, "Queued"), strings.Index(out, "cursor ")
	if waiting < 0 || queued < 0 || cursor < 0 || !(waiting < queued && queued < cursor) {
		t.Fatalf("section order wrong (%d %d %d):\n%s", waiting, queued, cursor, out)
	}
	list, _ := r.cmd("status")
	if !strings.Contains(list, "2 pieces, 1 waiting on you") {
		t.Fatalf("list:\n%s", list)
	}
}

func TestMissionStatus_NotRunningIsSaid(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	out, err := r.cmd("status", id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not running") {
		t.Fatalf("output:\n%s", out)
	}
	unlock, err := r.store(id).Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	out, _ = r.cmd("status", id)
	if !strings.Contains(out, "live, pid ") {
		t.Fatalf("output:\n%s", out)
	}
}

func TestMissionAnswer_BadWaitPrintsErrorAndQueuesNothing(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	_, err := r.cmd("answer", id, "nope", "--do", "retry", "--words", "try again")
	if err == nil || !strings.Contains(err.Error(), `no piece is waiting on "nope"`) {
		t.Fatalf("err = %v", err)
	}
	if items, _ := r.store(id).Inbox(); len(items) != 0 {
		t.Fatalf("inbox = %v", items)
	}
	if _, err := r.cmd("answer", id, "w1", "--do", "retry"); err == nil {
		t.Fatal("--words is required")
	}
}

func TestMissionAnswer_QueuesWhenValid(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) {
		s.Pieces[0].Status = mission.PieceWaiting
		s.Pieces[0].Wait = &mission.Wait{ID: "w1", Kind: mission.WaitQuestion, Since: time.Now(), Back: mission.PieceRunning}
	})
	out, err := r.cmd("answer", id, "w1", "--do", "retry", "--words", "go on")
	if err != nil || !strings.Contains(out, "queued; the mission isn't running, so it is applied on resume") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if items, _ := r.store(id).Inbox(); len(items) != 1 || items[0].Kind != "answer" {
		t.Fatalf("inbox = %v", items)
	}
}

func TestMissionAmend_QueuesWithWords(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning })
	unlock, err := r.store(id).Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	r.stdin = string(planFile("Build the thing.", designPiece("P1"), designPiece("P2", "P1"), designPiece("P3", "P2")))
	if _, err := r.cmd("amend", id); err == nil {
		t.Fatal("--words is required")
	}
	out, err := r.cmd("amend", id, "-", "--words", "also add P3")
	if err != nil || !strings.Contains(out, "queued; applied on the next tick") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	items, _ := r.store(id).Inbox()
	if len(items) != 1 || items[0].Kind != "amend" || !strings.Contains(string(items[0].Body), "also add P3") {
		t.Fatalf("inbox = %+v", items)
	}
}

func TestMissionSetup_WritesDefaultsOnce(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "crew")
	var out bytes.Buffer
	args := []string{"setup", "--dir", dir, "--project", t.TempDir()}
	if err := missionCmd(args, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(dir, "missions.yaml")
	if _, err := os.Stat(settings); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "domains")); err != nil || !fi.IsDir() {
		t.Fatalf("domains: %v", err)
	}
	if err := os.WriteFile(settings, []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if err := missionCmd(args, strings.NewReader(""), &out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(settings)
	if string(b) != "edited\n" || !strings.Contains(out.String(), "already set up") {
		t.Fatalf("file = %q, output = %q", b, out.String())
	}
}

func TestMissionStop_NoEngineMarksStopped(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning })
	out, err := r.cmd("stop", id)
	if err != nil || !strings.Contains(out, "wasn't running") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	if r.status(id) != mission.StatusStopped {
		t.Fatalf("status = %s", r.status(id))
	}
	if got := r.eventKinds(id); len(got) == 0 || got[len(got)-1] != mission.EvStopped {
		t.Fatalf("events = %v", got)
	}
}

func TestMissionResume_RefusesWhileEngineLive(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusStopped })
	if err := r.store(id).SaveApproved(); err != nil {
		t.Fatal(err)
	}
	unlock, err := r.store(id).Lock()
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.cmd("resume", id)
	if err == nil || !strings.Contains(err.Error(), "is being driven (pid ") {
		t.Fatalf("err = %v", err)
	}
	if len(r.starts) != 0 || r.status(id) != mission.StatusStopped {
		t.Fatalf("starts = %d, status = %s", len(r.starts), r.status(id))
	}
	unlock()
	if _, err := r.cmd("resume", id); err != nil {
		t.Fatal(err)
	}
	if len(r.starts) != 1 || r.status(id) != mission.StatusRunning {
		t.Fatalf("starts = %d, status = %s", len(r.starts), r.status(id))
	}
}

func TestMissionRunEngine_TicksAndExitsWhenDone(t *testing.T) {
	r := newMissionRig(t)
	r.stdin = string(planFile("Build the thing.", designPiece("P1")))
	out, err := r.cmd("plan")
	if err != nil {
		t.Fatal(err)
	}
	id := missionIDRe.FindString(out)
	r.edit(id, func(s *mission.State) {
		s.Status = mission.StatusRunning
		s.Pieces[0].Status = mission.PieceComplete
	})
	tickEvery = 5 * time.Millisecond
	if out, err := r.cmd("run", id, "--engine"); err != nil {
		t.Fatalf("run --engine: %v\n%s", err, out)
	}
	if r.status(id) != mission.StatusDone {
		t.Fatalf("status = %s", r.status(id))
	}
}

func TestMissionRun_SupervisorGivesUpAndMarksStopped(t *testing.T) {
	r := newMissionRig(t)
	id := r.plan()
	r.edit(id, func(s *mission.State) { s.Status = mission.StatusRunning })
	old := engineCmd
	engineCmd = func(string, []string) *exec.Cmd { return exec.Command("sh", "-c", "exit 3") }
	t.Cleanup(func() { engineCmd = old })
	if _, err := r.cmd("run", id); err == nil {
		t.Fatal("want an error when the engine keeps crashing")
	}
	if r.status(id) != mission.StatusStopped {
		t.Fatalf("status = %s", r.status(id))
	}
	kinds := strings.Join(r.eventKinds(id), ",")
	if strings.Count(kinds, mission.EvNote) != 3 || !strings.HasSuffix(kinds, mission.EvStopped) {
		t.Fatalf("events = %s", kinds)
	}
}
