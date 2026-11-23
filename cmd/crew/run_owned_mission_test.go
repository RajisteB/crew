package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/mission"
)

// stoppedPlainRun drives a plain run of the approval fixture to its stop and
// leaves its Owner empty.
func stoppedPlainRun(t *testing.T) (*cliEnv, string, string) {
	t.Helper()
	e := newCLIEnv(t, verdict("ready"), verdict("done"), verdict("pass"))
	out, code := e.crew("run", "ship-change", "--input", "x")
	if code != 0 || !strings.Contains(out, "waiting for approval") {
		t.Fatalf("exit %d:\n%s", code, out)
	}
	id := runID(t, out)
	return e, id, filepath.Join(e.project, ".crew", "runs", id, "state.json")
}

// nameRunInMission writes a mission state under the project's missions folder
// that names runID as the run of piece P1 of mission m-1.
func nameRunInMission(t *testing.T, project, runID string) {
	t.Helper()
	dir := filepath.Join(project, ".crew", "missions", "m-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	st := mission.State{MissionID: "m-1", Status: mission.StatusStopped, Pieces: []mission.PieceState{{ID: "P1", Status: mission.PieceRunning, RunID: runID}}}
	if err := (mission.Store{Dir: dir}).SaveState(st); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ownedVerbArgs(id string) map[string][]string {
	return map[string][]string{
		"say":     {"run", "say", id, "hi"},
		"goto":    {"run", "goto", id, "review"},
		"approve": {"run", "approve", id},
		"resume":  {"run", "resume", id},
	}
}

func TestCLI_RunNamedByAMissionIsRefusedWithoutAnOwnerStamp(t *testing.T) {
	e, id, statePath := stoppedPlainRun(t)
	nameRunInMission(t, e.project, id)
	before := readFile(t, statePath)
	for verb, args := range ownedVerbArgs(id) {
		out, code := e.crew(args...)
		if code != 1 || !strings.Contains(out, "belongs to mission m-1 piece P1") || !strings.Contains(out, "crew run "+verb+" is refused") {
			t.Errorf("%s: exit %d, want 1 and the refusal:\n%s", verb, code, out)
		}
		if !bytes.Equal(before, readFile(t, statePath)) {
			t.Errorf("%s changed state.json", verb)
		}
	}
	status, code := e.crew("run", "status", id)
	if code != 0 || !strings.Contains(status, "\nowner: mission m-1 piece P1\n") {
		t.Errorf("status: exit %d, want the owner line:\n%s", code, status)
	}
	list, code := e.crew("run", "status")
	if code != 0 || !hasLineEnding(list, id, "  mission m-1 piece P1") {
		t.Errorf("list: exit %d, want the run's line to end with the owner:\n%s", code, list)
	}
}

func hasLineEnding(text, prefix, suffix string) bool {
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, prefix) && strings.HasSuffix(l, suffix) {
			return true
		}
	}
	return false
}

func TestCLI_UnreadableMissionStateRefusesAndStatusStillWorks(t *testing.T) {
	e, id, statePath := stoppedPlainRun(t)
	dir := filepath.Join(e.project, ".crew", "missions", "m-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	bad := filepath.Join(dir, "state.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	before := readFile(t, statePath)
	for verb, args := range ownedVerbArgs(id) {
		out, code := e.crew(args...)
		if code != 1 || !strings.Contains(out, "crew run "+verb+" is refused") || !strings.Contains(out, bad) {
			t.Errorf("%s: exit %d, want 1, a refusal and the file's name:\n%s", verb, code, out)
		}
		if !bytes.Equal(before, readFile(t, statePath)) {
			t.Errorf("%s changed state.json", verb)
		}
	}
	status, code := e.crew("run", "status", id)
	if code != 0 || !strings.Contains(status, "\nowner: unknown (") || !strings.Contains(status, bad) || !strings.Contains(status, "cursor ") {
		t.Errorf("status: exit %d, want the run and an unknown owner line:\n%s", code, status)
	}
	if list, code := e.crew("run", "status"); code != 0 || !strings.Contains(list, id) {
		t.Errorf("list: exit %d:\n%s", code, list)
	}
}
