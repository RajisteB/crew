package mission

import (
	"fmt"
	"strings"
	"time"

	"github.com/crewfile-dev/crew/internal/gh"
	"github.com/crewfile-dev/crew/internal/principal"
)

const (
	checksGrace    = 2 * time.Minute  // how long a new head may show no checks
	checksPatience = 30 * time.Minute // how long checks may run before the principal is told
	maxPRSendBacks = 2
	maxFailedShown = 10
)

// prOpen applies the pull request rules, in order, to a pr-open piece whose
// pull request was observed. It records no event for a conflict, a send back
// or a merge: the engine records them when the effect succeeds.
func (d *decider) prOpen(id string) {
	pr, ok := d.o.PRs[id]
	if !ok {
		return
	}
	p := d.ps(id)
	if d.ended(id, pr) || pr.State != "OPEN" {
		return
	}
	if p.Wait != nil && (p.Wait.Kind == WaitQuestion || p.Wait.Kind == WaitLimit) {
		return
	}
	if pr.Mergeable == "CONFLICTING" {
		d.conflict(id)
		return
	}
	auto := d.o.Auto[id] // absent: the zero mode, which is not on or without-checks
	mode := auto.Mode
	ours := pr.HeadSHA == p.Head && p.Head != ""
	pass, fail, pending := tally(pr.Checks)
	if mode != principal.AutoWithoutChecks && ours && fail > 0 {
		d.failedChecks(id, pr.Checks)
		return
	}
	since := d.o.Now.Sub(p.PRSince)
	switch {
	case mode != principal.AutoOn && mode != principal.AutoWithoutChecks:
		d.mergeWait(id, blockerQuestion(pr.Number, id, auto.Blocker))
	case !ours:
		if since >= checksGrace {
			d.mergeWait(id, fmt.Sprintf("PR #%d (%s) has commits crew didn't push, so crew won't merge it; it is waiting on you", pr.Number, id))
		}
	case pr.Mergeable != "MERGEABLE":
	case mode == principal.AutoWithoutChecks:
		d.add(Merge{Piece: id, Head: p.Head})
	default:
		d.byChecks(id, pr, since, pass, fail, pending)
	}
}

// byChecks is rules 10 to 12: mode on, our head, a mergeable pull request.
func (d *decider) byChecks(id string, pr gh.PR, since time.Duration, pass, fail, pending int) {
	switch {
	case pass+fail+pending == 0:
		if since >= checksGrace {
			d.mergeWait(id, fmt.Sprintf("PR #%d (%s) has no checks, and auto-merge needs at least one to pass; it is waiting on you", pr.Number, id))
		}
	case pending > 0:
		if since >= checksPatience {
			d.mergeWait(id, fmt.Sprintf("PR #%d (%s): checks are still running after 30m; crew merges it when they pass, or merge it yourself", pr.Number, id))
		}
	case pass > 0 && fail == 0:
		d.add(Merge{Piece: id, Head: d.ps(id).Head, Checked: true})
	}
}

// ended handles a merged or closed pull request (rules 1 and 2) and reports
// whether the pull request is no longer open.
func (d *decider) ended(id string, pr gh.PR) bool {
	p := d.ps(id)
	switch pr.State {
	case "MERGED":
		d.complete(id)
		d.record(EvPRMerged, id, fmt.Sprintf("PR #%d", pr.Number))
		d.add(Cleanup{Piece: id, Force: true})
	case "CLOSED":
		if p.Wait == nil || p.Wait.Kind != WaitPRClosed {
			d.wait(id, WaitPRClosed, fmt.Sprintf("PR #%d (%s) was closed without merging", pr.Number, id), "")
		}
	default:
		return false
	}
	return true
}

func (d *decider) conflict(id string) {
	if d.ps(id).SentBack[reasonConflict] < maxPRSendBacks {
		d.add(ResolveConflict{Piece: id})
		return
	}
	d.ask(id, reasonConflict)
}

func (d *decider) failedChecks(id string, checks []gh.Check) {
	if d.ps(id).SentBack[reasonChecksFailed] < maxPRSendBacks {
		d.add(SendBack{Piece: id, Reason: reasonChecksFailed, Note: failedChecksNote(checks)})
		return
	}
	d.ask(id, reasonChecksFailed)
}

// mergeWait tells the principal the pull request waits on them, unless the
// piece already holds a wait.
func (d *decider) mergeWait(id, question string) {
	if d.ps(id).Wait == nil {
		d.wait(id, WaitMerge, question, "")
	}
}

func blockerQuestion(n int, id, blocker string) string {
	if blocker == "" {
		return fmt.Sprintf("PR #%d (%s) is waiting on you", n, id)
	}
	return fmt.Sprintf("PR #%d (%s) also changes files of the %s domain, whose auto-merge is off; it is waiting on you", n, id, blocker)
}

// tally counts the checks that passed, failed and are pending; skipped checks count for nothing.
func tally(checks []gh.Check) (pass, fail, pending int) {
	for _, c := range checks {
		switch c.State {
		case gh.CheckPass:
			pass++
		case gh.CheckFail:
			fail++
		case gh.CheckPending:
			pending++
		}
	}
	return pass, fail, pending
}

func failedChecksNote(checks []gh.Check) string {
	var failed []string
	for _, c := range checks {
		if c.State == gh.CheckFail {
			failed = append(failed, fmt.Sprintf("%s (%s)", c.Name, c.URL))
		}
	}
	shown := failed
	if len(shown) > maxFailedShown {
		shown = shown[:maxFailedShown]
	}
	list := strings.Join(shown, ", ")
	if extra := len(failed) - len(shown); extra > 0 {
		list += fmt.Sprintf(" (and %d more)", extra)
	}
	return "These checks failed on the pull request: " + list + ". Fix what they report and commit."
}
