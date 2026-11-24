package mission

import (
	"fmt"
	"strings"
)

const (
	maxSummaryRunes = 120
	maxCommentRunes = 1000
	maxBodyRunes    = 30_000 // Jira refuses a description over 32,767 characters
	bodyCutMark     = "\n… (cut: the full text is in the mission folder)"
)

// cut shortens text to at most n runes, never splitting one.
func cut(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	return string(r[:n])
}

// cutBody keeps a description within maxBodyRunes, and says so when it cut.
func cutBody(body string) string {
	if c := cut(body, maxBodyRunes); c != body {
		return c + bodyCutMark
	}
	return body
}

// TrackerLabel is the label that ties an issue to its mission and piece.
func TrackerLabel(missionID, piece string) string {
	if piece == "" {
		piece = "epic"
	}
	return "crew-" + missionID + "-" + piece
}

// EpicText is the summary and body of the mission's epic.
func EpicText(missionID string, m Mission) (summary, body string) {
	return cutWords(firstLine(m.Request), maxSummaryRunes), cutBody("Mission " + missionID + "\n\n" + m.Request)
}

// IssueText is the summary and body of a piece's issue.
func IssueText(missionID string, m Mission, p Piece) (summary, body string) {
	waits := "nothing"
	if len(p.WaitsFor) > 0 {
		waits = strings.Join(p.WaitsFor, ", ")
	}
	summary = cutWords(p.ID+": "+firstLine(p.Brief), maxSummaryRunes)
	factory := p.Factory
	if p.Delegate {
		factory = "none (delegated: its pieces are planned when it is ready)"
	}
	partOf := ""
	if parent := ParentID(p.ID); parent != "" {
		partOf = "Part of: " + parent + "\n"
	}
	body = fmt.Sprintf("Mission %s, piece %s\n%sDomain: %s\nFactory: %s\nWaits for: %s\n\n%s",
		missionID, p.ID, partOf, p.Domain, factory, waits, p.Brief)
	return summary, cutBody(body)
}

// DeliverablesRel is the folder, relative to the repo, that holds a piece's deliverables.
func DeliverablesRel(missionID, piece string) string {
	return ".crew/missions/" + missionID + "/deliverables/" + piece
}

// escalatedText is the comment for an escalation: a scoped orchestrator's
// question about a piece under it is passed up to the orchestrator above,
// any other goes to the principal.
func escalatedText(ev Event) string {
	if scope, ok := strings.CutPrefix(ev.By, scopedBy("")); ok && Under(ev.Piece, scope) {
		return "Passed up by " + ev.By + ": " + ev.Msg
	}
	return "Escalated to you: " + ev.Msg
}

// TrackerComment is the comment an event puts on an issue. piece "" means the epic.
func TrackerComment(ev Event, s State) (piece, text string, ok bool) {
	piece = ev.Piece
	switch ev.Kind {
	case EvPROpened:
		p, _ := s.Piece(ev.Piece)
		text = "Pull request opened: " + p.PRURL
	case EvDecision:
		text = "Decision (" + ev.By + "): " + ev.Msg
	case EvEscalated:
		text = escalatedText(ev)
	case EvWaitingOnPrincipal:
		text = "Waiting on you: " + ev.Msg
	case EvRebrief:
		text = "Brief changed by " + ev.By + "."
	case EvPieceCancelled:
		text = "Cancelled: " + ev.Msg
	case EvDeliverablePosted:
		text = "Deliverable posted: " + ev.Msg + ". A copy is in " + DeliverablesRel(s.MissionID, ev.Piece) + "."
	case EvDeliverableAccepted:
		text = "Deliverable accepted by " + ev.By + ": " + ev.Msg
	case EvBudgetHit:
		text = fmt.Sprintf("Budget reached: %s (%d of %s)", ev.Msg, ev.Count, ev.Max)
	case EvAmendment:
		piece, text = "", "Amendment by the principal: "+ev.Msg
	default:
		return "", "", false
	}
	return piece, cut(text, maxCommentRunes), true
}

// cutWords shortens text to at most n runes. When it cuts, it backs up to the
// last space (if that is in the second half), drops trailing space and
// punctuation, and ends with an ellipsis.
func cutWords(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	head := r[:n-1]
	if i := lastSpace(head); i >= n/2 {
		head = head[:i]
	}
	return strings.TrimRight(string(head), " ,;:.") + "…"
}

// lastSpace is the rune index of the last space in r, or -1.
func lastSpace(r []rune) int {
	for i := len(r) - 1; i >= 0; i-- {
		if r[i] == ' ' {
			return i
		}
	}
	return -1
}
