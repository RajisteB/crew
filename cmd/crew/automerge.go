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
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
	"github.com/crewfile-dev/crew/internal/source"
)

const automergeUsage = `usage:
  crew automerge on  (--domain D | --all) [--without-checks] [--project P] [--dir D]
  crew automerge off (--domain D | --all) [--project P] [--dir D]
  crew automerge status [--project P] [--dir D]
on and off need a terminal: you type a confirmation that names the repo and the domain.`

// automergeCmd handles `crew automerge …`. It is the only code that writes the
// auto-merge switch, and it never reads standard input: the confirmation is
// typed on the controlling terminal.
func automergeCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("automerge", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirFlag := fs.String("dir", "", "crew directory")
	projFlag := fs.String("project", "", "project root (default: current directory)")
	domainFlag := fs.String("domain", "", "domain to set")
	allFlag := fs.Bool("all", false, "every defined domain")
	noChecks := fs.Bool("without-checks", false, "merge without waiting for checks")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, automergeUsage)
	}
	if len(pos) != 1 || !slices.Contains([]string{"on", "off", "status"}, pos[0]) {
		return errors.New(automergeUsage)
	}
	dir, err := resolveDir(*dirFlag, false)
	if err != nil {
		return err
	}
	project, err := projectDir(*projFlag)
	if err != nil {
		return err
	}
	req := amRequest{
		mode: pos[0], crewDir: dir, project: project,
		domain: *domainFlag, all: *allFlag, noChecks: *noChecks,
		store: principal.Store{Dir: filepath.Join(dir, "principal")},
	}
	if req.mode == "status" {
		return automergeStatus(req, out)
	}
	return automergeSet(req, out)
}

type amRequest struct {
	mode, crewDir, project, domain string
	all, noChecks                  bool
	store                          principal.Store
}

// definedDomains returns the sorted names of every domain in the crew dir.
func definedDomains(crewDir string) ([]string, error) {
	ds, errs := source.LoadDomains(crewDir)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	if len(ds) == 0 {
		return nil, fmt.Errorf("no domains defined in %s; run crew mission setup", filepath.Join(crewDir, source.DomainsDir))
	}
	names := make([]string, len(ds))
	for i, d := range ds {
		names[i] = d.Name
	}
	slices.Sort(names)
	return names, nil
}

// switchesFor reads the repo's records, turning a tampered store into the
// message that tells the principal what to do.
func switchesFor(store principal.Store, slug string) (map[string]principal.SwitchRecord, error) {
	recs, err := store.Switches(slug)
	return recs, explainTampered(store, err)
}

func explainTampered(store principal.Store, err error) error {
	if !errors.Is(err, principal.ErrTampered) {
		return err
	}
	path := filepath.Join(store.Dir, "automerge.json")
	return fmt.Errorf("%s was edited outside crew, so every switch reads as off; delete it and its .sum, then set the switches again with crew automerge", path)
}

func automergeStatus(req amRequest, out io.Writer) error {
	names, err := definedDomains(req.crewDir)
	if err != nil {
		return err
	}
	info, err := ghClient(req.project).Repo(context.Background())
	if err != nil {
		return err
	}
	recs, err := switchesFor(req.store, info.Slug)
	if err != nil {
		return err
	}
	for _, n := range names {
		rec, ok := recs[n]
		if !ok {
			fmt.Fprintf(out, "%s  off\n", n)
			continue
		}
		fmt.Fprintf(out, "%s  %s  set %s\n", n, rec.Mode, rec.SetAt.Format(time.DateOnly))
	}
	return nil
}

// automergeSet runs `on` and `off`: terminal, repo, domains, checks, block, typed phrase, write.
func automergeSet(req amRequest, out io.Writer) error {
	tty, err := openTTY()
	if err != nil {
		return fmt.Errorf("crew automerge needs a terminal: %w", err)
	}
	defer tty.Close()
	info, err := ghClient(req.project).Repo(context.Background())
	if err != nil {
		return err
	}
	if err := principal.CheckRepo(req.store, info); err != nil {
		if errors.Is(err, principal.ErrNotConfirmed) {
			return fmt.Errorf("%s isn't confirmed; run crew repo confirm first", info.Slug)
		}
		return err
	}
	domains, err := resolveAMDomains(req)
	if err != nil {
		return err
	}
	mode, err := requestedMode(req, info, domains)
	if err != nil {
		return err
	}
	noPaths, err := domainsWithoutPaths(req.crewDir, domains)
	if err != nil {
		return err
	}
	phrase := info.Slug + " " + domains[0]
	if req.all {
		phrase = info.Slug + " all"
	}
	fmt.Fprint(tty, confirmationBlock(info, domains, noPaths, mode))
	if err := principal.Confirm(tty, fmt.Sprintf(`Type "%s" to confirm: `, phrase), phrase); err != nil {
		return err
	}
	now := time.Now().UTC()
	recs := make([]principal.SwitchRecord, len(domains))
	for i, d := range domains {
		recs[i] = principal.SwitchRecord{Repo: info.Slug, Domain: d, Mode: mode, SetAt: now}
	}
	if err := req.store.SetSwitches(recs); err != nil {
		return explainTampered(req.store, err)
	}
	fmt.Fprintf(out, "auto-merge %s for %s in %s\n", mode, strings.Join(domains, ", "), info.Slug)
	return nil
}

// requestedMode is the mode the command sets; `on` without --without-checks
// first needs a repo that has checks.
func requestedMode(req amRequest, info gh.RepoInfo, domains []string) (principal.AutoMode, error) {
	switch {
	case req.mode == "on" && req.noChecks:
		return principal.AutoWithoutChecks, nil
	case req.mode == "on":
		return principal.AutoOn, requireChecks(req, info, domains)
	}
	return principal.AutoOff, nil
}

// resolveAMDomains applies the --domain / --all / --without-checks rules.
func resolveAMDomains(req amRequest) ([]string, error) {
	if req.mode == "off" && req.noChecks {
		return nil, errors.New("--without-checks only goes with crew automerge on")
	}
	if (req.domain == "") == !req.all {
		return nil, errors.New("give exactly one of --domain D or --all")
	}
	names, err := definedDomains(req.crewDir)
	if err != nil {
		return nil, err
	}
	if req.all {
		return names, nil
	}
	if !slices.Contains(names, req.domain) {
		return nil, fmt.Errorf("no domain %q (have: %s)", req.domain, strings.Join(names, ", "))
	}
	return []string{req.domain}, nil
}

// requireChecks fails unless a workflow file exists or the default branch has checks.
func requireChecks(req amRequest, info gh.RepoInfo, domains []string) error {
	if hasWorkflowFile(req.project) {
		return nil
	}
	has, err := ghClient(req.project).HasChecks(context.Background(), info.DefaultBranch)
	if err != nil || has {
		return err
	}
	flagUsed := "--domain " + domains[0]
	if req.all {
		flagUsed = "--all"
	}
	return fmt.Errorf("%s has no checks: nothing ran on the latest commit of %s, and there is no workflow in .github/workflows.\n"+
		"With auto-merge on, crew merges a pull request only after a check passes, so nothing would ever merge here.\n"+
		"Add CI, or run crew automerge on %s --without-checks to merge as soon as the factory finishes.",
		info.Slug, info.DefaultBranch, flagUsed)
}

func hasWorkflowFile(project string) bool {
	entries, err := os.ReadDir(filepath.Join(project, ".github", "workflows"))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(entries, func(e os.DirEntry) bool {
		ext := filepath.Ext(e.Name())
		return !e.IsDir() && (ext == ".yml" || ext == ".yaml")
	})
}

// confirmationBlock is what the principal reads on the terminal before typing
// the phrase. noPaths names the other defined domains that list no paths.
func confirmationBlock(info gh.RepoInfo, domains, noPaths []string, mode principal.AutoMode) string {
	base := info.DefaultBranch
	var modeLine, warning string
	switch mode {
	case principal.AutoOn:
		modeLine = fmt.Sprintf("on. crew merges a pull request of this domain into %s once it has at least one check and every check has passed.", base)
		warning = fmt.Sprintf("If merging to %s deploys this repo, this turns on automatic deploys.", base)
	case principal.AutoWithoutChecks:
		modeLine = "on, without checks. crew merges as soon as the factory finishes."
		warning = fmt.Sprintf("Nothing but the factory's own review station stands between an agent's work and %s. If merging to %s deploys this repo, this turns on automatic deploys of unchecked work.", base, base)
	default:
		modeLine = "off. crew leaves this domain's pull requests for you to merge."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "\nRepo:     %s (%s)\nDomains:  %s\nMode:     %s\n\n", info.Slug, info.Visibility, strings.Join(domains, ", "), modeLine)
	if warning != "" {
		b.WriteString(warning + "\n\n")
		b.WriteString(noPathsParagraph(noPaths))
	}
	return b.String()
}

// noPathsParagraph says which domains the cross-domain rule can't protect. It
// is empty when there are none.
func noPathsParagraph(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0] + " lists no paths, so crew can't tell when a pull request changes its files: " +
			"its switch won't hold back another domain's pull request. Add paths: to its domain file to change that.\n\n"
	}
	return strings.Join(names, ", ") + " list no paths, so crew can't tell when a pull request changes their files: " +
		"their switch won't hold back another domain's pull request. Add paths: to their domain files to change that.\n\n"
}

// domainsWithoutPaths is the sorted names of the defined domains that this
// command does not switch and that list no paths.
func domainsWithoutPaths(crewDir string, switching []string) ([]string, error) {
	ds, errs := source.LoadDomains(crewDir)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	var out []string
	for _, d := range ds {
		if len(d.Paths) == 0 && !slices.Contains(switching, d.Name) {
			out = append(out, d.Name)
		}
	}
	slices.Sort(out)
	return out, nil
}
