package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crewfile-dev/crew/internal/mission"
	"github.com/crewfile-dev/crew/internal/principal"
)

const unreadableSuffix = " (switch unreadable)"

// readSwitches reads the auto-merge switches of repo for display. The store is
// only read here; crew automerge is the one writer.
func readSwitches(crewDir, repo string) (map[string]principal.SwitchRecord, error) {
	return principal.Store{Dir: filepath.Join(crewDir, "principal")}.Switches(repo)
}

// modeText is a domain's mode as shown to the principal; a switch that can't
// be read is off.
func modeText(recs map[string]principal.SwitchRecord, err error, domain string) string {
	if err != nil {
		return string(principal.AutoOff) + unreadableSuffix
	}
	return string(principal.ModeOf(recs, domain))
}

// autoMergeLine is "Auto-merge in <slug>: <domain> <mode>, …" over the
// domains, which are listed once each in the order given.
func autoMergeLine(slug string, domains []string, recs map[string]principal.SwitchRecord, err error) string {
	var parts []string
	seen := map[string]bool{}
	for _, d := range domains {
		if seen[d] {
			continue
		}
		seen[d] = true
		parts = append(parts, d+" "+modeText(recs, err, d))
	}
	return fmt.Sprintf("Auto-merge in %s: %s", slug, strings.Join(parts, ", "))
}

// printAutoMergeLine prints the line for the domains the plan uses, under the
// piece table.
func printAutoMergeLine(env missionEnv, slug string, m mission.Mission) {
	if slug == "" {
		return
	}
	domains := make([]string, len(m.Pieces))
	for i, p := range m.Pieces {
		domains[i] = p.Domain
	}
	recs, err := readSwitches(env.crewDir, slug)
	fmt.Fprintf(env.out, "\n%s\n", safeText(autoMergeLine(slug, domains, recs, err)))
}

// printOpenPRs lists the pr-open pieces that hold no wait, each with its
// own domain's mode.
func printOpenPRs(env missionEnv, m mission.Mission, st mission.State) {
	var lines []string
	recs, err := readSwitches(env.crewDir, st.Repo)
	for _, p := range st.Pieces {
		if p.Status != mission.PiecePROpen || p.Wait != nil {
			continue
		}
		domain := ""
		if piece, ok := m.Piece(p.ID); ok {
			domain = piece.Domain
		}
		lines = append(lines, fmt.Sprintf("%s  PR #%d  %s  auto-merge %s", withKey(p), p.PR, p.PRURL, modeText(recs, err, domain)))
	}
	section(env.out, "Open pull requests", lines)
}
