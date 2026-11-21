package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/notify"
	"github.com/crewfile-dev/crew/internal/principal"
)

var missionIDRe = regexp.MustCompile(`m-\d{8}-\d{6}-[0-9a-f]{4}`)

type startCall struct{ id, dir, project, logPath string }

// missionRig is a temp crew dir, a temp git repo, a fake gh and a recorder
// in place of the detached engine.
type missionRig struct {
	t             *testing.T
	crewDir, repo string
	stdin         string
	starts        []startCall
	startErr      error
}

func newMissionRig(t *testing.T) *missionRig {
	t.Helper()
	r := &missionRig{t: t, crewDir: scaffoldDir(t), repo: t.TempDir()}
	if out, err := exec.Command("git", "-C", r.repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	domain := "---\nname: design\ndescription: UI work.\nfactories: [ship-change]\n---\nBody.\n"
	if err := os.MkdirAll(filepath.Join(r.crewDir, "domains"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.crewDir, "domains", "design.md"), []byte(domain), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeGH(t, "private")
	oldStart, oldNotifier, oldTick := startDetached, newNotifier, tickEvery
	startDetached = func(id, dir, project, logPath string) (int, error) {
		r.starts = append(r.starts, startCall{id, dir, project, logPath})
		return 4242, r.startErr
	}
	newNotifier = func() notify.Notifier { return notify.Nop{} }
	t.Cleanup(func() { startDetached, newNotifier, tickEvery = oldStart, oldNotifier, oldTick })
	store := principal.Store{Dir: filepath.Join(r.crewDir, "principal")}
	rec := principal.RepoRecord{Repo: "acme/app", Visibility: "private", ConfirmedAt: time.Now()}
	if err := store.ConfirmRepo(rec); err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *missionRig) cmd(args ...string) (string, error) {
	r.t.Helper()
	var out bytes.Buffer
	full := append(append([]string{}, args...), "--dir", r.crewDir, "--project", r.repo)
	err := missionCmd(full, strings.NewReader(r.stdin), &out)
	return out.String(), err
}

func (r *missionRig) root() string {
	r.t.Helper()
	root, err := gitx.Repo{Dir: r.repo}.Root()
	if err != nil {
		r.t.Fatal(err)
	}
	return root
}

func (r *missionRig) setApproval(v string) {
	r.t.Helper()
	p := filepath.Join(r.crewDir, "missions.yaml")
	b, err := os.ReadFile(p)
	if err != nil {
		r.t.Fatal(err)
	}
	b = bytes.Replace(b, []byte("mission_approval: on"), []byte("mission_approval: "+v), 1)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		r.t.Fatal(err)
	}
}

func planFile(request string, pieces ...mission.Piece) []byte {
	return mission.Mission{Pieces: pieces, Request: request}.Render()
}

func designPiece(id string, waits ...string) mission.Piece {
	return mission.Piece{ID: id, Domain: "design", Factory: "ship-change", Brief: "Do " + id + ".", WaitsFor: waits}
}

// plan creates a two-piece mission and returns its id.
func (r *missionRig) plan() string {
	r.t.Helper()
	r.stdin = string(planFile("Build the thing.", designPiece("P1"), designPiece("P2", "P1")))
	out, err := r.cmd("plan")
	if err != nil {
		r.t.Fatalf("plan: %v\n%s", err, out)
	}
	id := missionIDRe.FindString(out)
	if id == "" {
		r.t.Fatalf("no mission id in:\n%s", out)
	}
	return id
}

func (r *missionRig) store(id string) mission.Store {
	r.t.Helper()
	s, err := mission.Open(r.root(), id)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func (r *missionRig) edit(id string, f func(*mission.State)) {
	r.t.Helper()
	s := r.store(id)
	st, err := s.LoadState()
	if err != nil {
		r.t.Fatal(err)
	}
	f(&st)
	if err := s.SaveState(st); err != nil {
		r.t.Fatal(err)
	}
}

func (r *missionRig) status(id string) mission.Status {
	r.t.Helper()
	st, err := r.store(id).LoadState()
	if err != nil {
		r.t.Fatal(err)
	}
	return st.Status
}

func (r *missionRig) eventKinds(id string) []string {
	r.t.Helper()
	evs, err := r.store(id).EventsSince(0)
	if err != nil {
		r.t.Fatal(err)
	}
	var kinds []string
	for _, e := range evs {
		kinds = append(kinds, e.Kind)
	}
	return kinds
}
