package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/gitx"
	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/scaffold"
	"github.com/crewfile-dev/crew/internal/verify"
)

const missionUsage = `usage:
  crew mission setup                                   write missions.yaml and domains/ if missing, and add the missions parts to harnesses.yaml
  crew mission plan [-|@FILE]                          check a plan and create the mission
  crew mission start <id>                              run the start checks; wait for go, or go
  crew mission go <id>                                 the principal's go
  crew mission status [<id>] [--since N]               what is running and what waits on you
  crew mission answer <id> <wait> --do retry|goto|cancel|approve|accept [--station S] [--note TEXT|@FILE] --words TEXT|@FILE
  crew mission amend <id> [-|@FILE] --words TEXT|@FILE
  crew mission stop <id>
  crew mission resume <id>
  crew mission run <id> [--engine]                     internal: the supervisor, or the engine under it
common flags: --dir D (crew directory), --project P (default: current directory)`

const maxPlanBytes = 1 << 20

// missionEnv is what every mission subcommand works with.
type missionEnv struct {
	crewDir, project, repoRoot string
	flags                      missionFlags
	stdin                      io.Reader
	out                        io.Writer
}

type missionFlags struct {
	dir, project, do, station, note, words string
	since                                  int
	engine                                 bool
}

func missionFlagSet(f *missionFlags) *flag.FlagSet {
	fs := flag.NewFlagSet("mission", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&f.dir, "dir", "", "crew directory")
	fs.StringVar(&f.project, "project", "", "project root")
	fs.StringVar(&f.do, "do", "", "answer: retry, goto, cancel, approve or accept")
	fs.StringVar(&f.station, "station", "", "answer: the station to go to")
	fs.StringVar(&f.note, "note", "", "answer: a message for the station (text, or @file)")
	fs.StringVar(&f.words, "words", "", "answer, amend: what the principal said (text, or @file)")
	fs.IntVar(&f.since, "since", -1, "status: only events after this cursor")
	fs.BoolVar(&f.engine, "engine", false, "run: the engine itself, not its supervisor")
	return fs
}

// missionCmd handles `crew mission …`.
func missionCmd(args []string, stdin io.Reader, out io.Writer) error {
	var f missionFlags
	pos, err := parseInterleaved(missionFlagSet(&f), args)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, missionUsage)
	}
	if len(pos) == 0 {
		return errors.New(missionUsage)
	}
	crewDir, err := resolveDir(f.dir, false)
	if err != nil {
		return err
	}
	project, err := projectDir(f.project)
	if err != nil {
		return err
	}
	env := missionEnv{crewDir: crewDir, project: project, flags: f, stdin: stdin, out: out}
	if pos[0] == "setup" {
		return missionSetup(env)
	}
	if env.repoRoot, err = (gitx.Repo{Dir: project}).Root(context.Background()); err != nil {
		return fmt.Errorf("crew mission needs a git repository: %w", err)
	}
	return dispatchMission(env, pos)
}

func dispatchMission(env missionEnv, pos []string) error {
	sub, rest := pos[0], pos[1:]
	if sub == "plan" {
		return missionPlan(env, rest)
	}
	if sub == "status" && len(rest) == 0 {
		return missionList(env)
	}
	if len(rest) == 0 {
		return errors.New(missionUsage)
	}
	id, rest := rest[0], rest[1:]
	switch sub {
	case "status":
		return missionStatus(env, id)
	case "start":
		return missionStart(env, id)
	case "go":
		return missionGo(env, id)
	case "answer":
		return missionAnswer(env, id, rest)
	case "amend":
		return missionAmend(env, id, rest)
	case "stop":
		return missionStop(env, id)
	case "resume":
		return missionResume(env, id)
	case "run":
		return missionRun(env, id)
	}
	return fmt.Errorf("unknown mission command %q\n%s", sub, missionUsage)
}

func missionSetup(env missionEnv) error {
	wrote, err := scaffold.MissionDefaults(env.crewDir)
	if err != nil {
		return err
	}
	for _, w := range wrote {
		fmt.Fprintf(env.out, "wrote %s\n", filepath.Join(env.crewDir, w))
	}
	up, err := scaffold.UpgradeHarnesses(env.crewDir)
	if err != nil {
		return err
	}
	printUpgrade(env.out, env.crewDir, up)
	switch {
	case len(wrote) == 0 && up.Backup == "" && len(up.Skipped) == 0:
		fmt.Fprintln(env.out, "already set up")
	case len(wrote) > 0 || up.Backup != "":
		fmt.Fprintln(env.out, "next: crew compile")
	}
	return nil
}

// readPlanSource reads a plan from stdin ("-" or nothing) or "@FILE".
func readPlanSource(env missionEnv, args []string) ([]byte, error) {
	if len(args) > 1 {
		return nil, errors.New(missionUsage)
	}
	var r io.Reader = env.stdin
	if len(args) == 1 && args[0] != "-" {
		path, ok := strings.CutPrefix(args[0], "@")
		if !ok {
			return nil, fmt.Errorf("plan: %q is not - or @FILE", args[0])
		}
		f, err := os.Open(expand(path))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		r = f
	}
	b, err := io.ReadAll(io.LimitReader(r, maxPlanBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxPlanBytes {
		return nil, fmt.Errorf("plan is larger than %d bytes", maxPlanBytes)
	}
	return b, nil
}

// repoInfo asks gh for the repo's slug and default branch.
func repoInfo(env missionEnv) (gh.RepoInfo, error) {
	info, err := ghClient(env.repoRoot).Repo(context.Background())
	if err != nil {
		return gh.RepoInfo{}, fmt.Errorf("can't reach GitHub: %w", err)
	}
	if info.DefaultBranch == "" {
		return gh.RepoInfo{}, errors.New("this repo has no default branch yet; push one first")
	}
	return info, nil
}

func printCheckErrors(out io.Writer, errs []error) error {
	for _, e := range errs {
		fmt.Fprintln(out, e)
	}
	if slices.ContainsFunc(errs, func(e error) bool { return errors.Is(e, mission.ErrNoDeny) }) {
		fmt.Fprintln(out, "run crew mission setup to add the missions parts to harnesses.yaml")
	}
	return fmt.Errorf("the plan has %d problem(s); nothing was created", len(errs))
}

func missionPlan(env missionEnv, args []string) error {
	src, err := readPlanSource(env, args)
	if err != nil {
		return err
	}
	m, err := mission.Parse(src)
	if err != nil {
		return printCheckErrors(env.out, []error{err})
	}
	info, err := repoInfo(env)
	if err != nil {
		return err
	}
	chk, err := loadChecks(env.crewDir, info.Slug)
	if err != nil {
		return err
	}
	if errs := mission.Check(m, chk.in); len(errs) > 0 {
		return printCheckErrors(env.out, errs)
	}
	store, st, err := mission.New(env.repoRoot, m, info, time.Now())
	if err != nil {
		return err
	}
	fmt.Fprintf(env.out, "mission %s planned (%s)\n\n", st.MissionID, store.Dir)
	printPieceTable(env.out, m)
	printAutoMergeLine(env, info.Slug, m)
	printJiraLine(env, info.Slug)
	fmt.Fprintf(env.out, "\n```mermaid\n%s```\n\nnext: crew mission start %s\n", mission.Mermaid(m), st.MissionID)
	return nil
}

func printPieceTable(out io.Writer, m mission.Mission) {
	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "PIECE\tDOMAIN\tFACTORY\tWAITS FOR")
	for _, p := range m.Pieces {
		waits := strings.Join(p.WaitsFor, ", ")
		if waits == "" {
			waits = "-"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", p.ID, p.Domain, p.Factory, waits)
	}
	tw.Flush()
}

// openMission opens a mission and reads its state.
func openMission(env missionEnv, id string) (mission.Store, mission.State, error) {
	store, err := mission.Open(env.repoRoot, id)
	if err != nil {
		return mission.Store{}, mission.State{}, err
	}
	st, err := store.LoadState()
	return store, st, err
}

// startChecks is everything start and resume check before an engine runs.
func startChecks(env missionEnv, store mission.Store) error {
	p, err := verifiedProject(env.crewDir, env.out)
	if err != nil {
		return err
	}
	m, err := store.Mission()
	if err != nil {
		return err
	}
	info, err := repoInfo(env)
	if err != nil {
		return err
	}
	settings, err := mission.LoadSettings(env.crewDir)
	if err != nil {
		return err
	}
	chk := newChecks(p, settings, info.Slug)
	if errs := mission.Check(m, chk.in); len(errs) > 0 {
		return printCheckErrors(env.out, errs)
	}
	if err := jiraStartCheck(env, info.Slug); err != nil {
		return err
	}
	return confirmRepo(context.Background(), env.crewDir, env.repoRoot, info)
}

func missionStart(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	if st.Status != mission.StatusPlanned {
		return fmt.Errorf("mission %s is %s; only a planned mission can be started", id, st.Status)
	}
	if err := startChecks(env, store); err != nil {
		return err
	}
	settings, err := mission.LoadSettings(env.crewDir)
	if err != nil {
		return err
	}
	if settings.Approval != "on" {
		return launchMission(env, store, id, mission.StatusPlanned)
	}
	return awaitGo(env, store, id)
}

// awaitGo parks a started mission until the principal says go.
func awaitGo(env missionEnv, store mission.Store, id string) error {
	st, err := writeState(store, id, func(cur mission.State) (mission.State, mission.Event, error) {
		if cur.Status != mission.StatusPlanned {
			return cur, mission.Event{}, fmt.Errorf("mission %s is %s; only a planned mission can be started", id, cur.Status)
		}
		next := cur.Clone()
		next.Status = mission.StatusAwaitingGo
		return next, mission.Event{}, nil
	})
	if err != nil {
		return err
	}
	m, err := store.Mission()
	if err != nil {
		return err
	}
	printPieceTable(env.out, m)
	printAutoMergeLine(env, st.Repo, m)
	printJiraLine(env, st.Repo)
	fmt.Fprintf(env.out, "\n%s\n\nwaiting for go: crew mission go %s\n", m.Request, id)
	return nil
}

func missionGo(env missionEnv, id string) error {
	store, st, err := openMission(env, id)
	if err != nil {
		return err
	}
	if st.Status != mission.StatusAwaitingGo {
		return fmt.Errorf("mission %s is %s; go needs a mission waiting for go (crew mission start %s)", id, st.Status, id)
	}
	if err := startChecks(env, store); err != nil {
		return err
	}
	return launchMission(env, store, id, mission.StatusAwaitingGo)
}

// launchMission approves the plan and marks the mission running, under the
// mission lock and only if it is still in the status from, then starts its
// supervisor in the background.
func launchMission(env missionEnv, store mission.Store, id string, from mission.Status) error {
	_, err := writeState(store, id, func(cur mission.State) (mission.State, mission.Event, error) {
		if cur.Status != from {
			return cur, mission.Event{}, fmt.Errorf("mission %s is %s, not %s; nothing was started", id, cur.Status, from)
		}
		if err := store.SaveApproved(); err != nil {
			return cur, mission.Event{}, err
		}
		next := cur.Clone()
		next.Status = mission.StatusRunning
		return next, mission.Event{Kind: mission.EvMissionStart, By: "crew"}, nil
	})
	if err != nil {
		return err
	}
	return detachMission(env, store, id)
}

func detachMission(env missionEnv, store mission.Store, id string) error {
	pid, err := startDetached(id, env.crewDir, env.project, filepath.Join(store.Dir, "mission.log"))
	if err != nil {
		return fmt.Errorf("mission %s is marked running but its engine didn't start (%w); try crew mission resume %s", id, err, id)
	}
	fmt.Fprintf(env.out, "mission %s running (pid %d)\nfollow with: crew mission status %s\n", id, pid, id)
	return nil
}

// checks is the project as the plan checks and the engine need it.
type checks struct {
	in       mission.CheckInput
	settings mission.Settings
	project  *verify.Project
}

// loadChecks builds the one CheckInput every command uses: domains,
// factories and registry from verify, limits from the settings for repo.
func loadChecks(crewDir, repo string) (checks, error) {
	settings, err := mission.LoadSettings(crewDir)
	if err != nil {
		return checks{}, err
	}
	p, _ := verify.Run(crewDir)
	return newChecks(p, settings, repo), nil
}

func newChecks(p *verify.Project, settings mission.Settings, repo string) checks {
	in := mission.CheckInput{Domains: p.Domains, Factories: p.Factories, Registry: p.Registry, Limits: settings.For(repo)}
	return checks{in: in, settings: settings, project: p}
}
