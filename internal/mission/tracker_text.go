package mission

import (
	"fmt"
	"strings"
)

const (
	maxSummaryRunes = 120
	maxCommentRunes = 1000
)

// cut shortens text to at most n runes, never splitting one.
func cut(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	return string(r[:n])
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
	return cut(firstLine(m.Request), maxSummaryRunes), "Mission " + missionID + "\n\n" + m.Request
}

// IssueText is the summary and body of a piece's issue.
func IssueText(missionID string, m Mission, p Piece) (summary, body string) {
	waits := "nothing"
	if len(p.WaitsFor) > 0 {
		waits = strings.Join(p.WaitsFor, ", ")
	}
	summary = cut(p.ID+": "+firstLine(p.Brief), maxSummaryRunes)
	body = fmt.Sprintf("Mission %s, piece %s\nDomain: %s\nFactory: %s\nWaits for: %s\n\n%s",
		missionID, p.ID, p.Domain, p.Factory, waits, p.Brief)
	return summary, body
}

// DeliverablesRel is the folder, relative to the repo, that holds a piece's deliverables.
func DeliverablesRel(missionID, piece string) string {
	return ".crew/missions/" + missionID + "/deliverables/" + piece
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
		text = "Escalated to you: " + ev.Msg
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
	case EvAmendment:
		piece, text = "", "Amendment by the principal: "+ev.Msg
	default:
		return "", "", false
	}
	return piece, cut(text, maxCommentRunes), true
}
