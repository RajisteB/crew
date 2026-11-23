package mission

import (
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// PlanRequest is everything the scoped orchestrator is told to plan a delegated piece.
type PlanRequest struct {
	MissionID, Request string
	Changes            []string // what the principal said in each amendment
	Piece              Piece    // the delegated piece
	Above              []Piece  // the delegated pieces above it, nearest first
	Paths              []string // its effective paths; nil when it has none
	Briefing           string   // the domain's body
	Factories          []FactoryInfo
	Others             []string   // one line per other piece of the mission: "P1 (product): <first line of its brief>"
	PiecesLeft         string     // how many more pieces the mission may hold, or "*"
	MayDelegate        bool       // a sub-piece may itself be delegated
	Earlier            []Decision // the piece's earlier "plan" decisions
}

// FactoryInfo is a factory a sub-piece may use.
type FactoryInfo struct {
	Name, Description string
	Stations          []string
}

// BuildPlanRequest gathers what the scoped orchestrator needs for the piece
// ps names. id is the mission's id.
func BuildPlanRequest(id string, m Mission, ps PieceState, d source.Domain, factories []source.Factory, lim Limits) PlanRequest {
	p, _ := m.Piece(ps.ID)
	r := PlanRequest{
		MissionID: id, Request: m.Request, Changes: m.PrincipalChanges(), Piece: p,
		Paths: EffectivePaths(p, d), Briefing: d.Body,
		PiecesLeft:  left(lim.Pieces, len(m.Pieces)),
		MayDelegate: Allows(lim.Depth, Level(ps.ID)+1),
	}
	for _, a := range Ancestors(ps.ID) {
		if ap, ok := m.Piece(a); ok {
			r.Above = append(r.Above, ap)
		}
	}
	for _, name := range d.Factories {
		if f, ok := findFactory(factories, name); ok {
			r.Factories = append(r.Factories, FactoryInfo{Name: f.Name, Description: f.Description, Stations: f.StationNames()})
		}
	}
	for _, o := range m.Pieces {
		if o.ID != ps.ID {
			r.Others = append(r.Others, fmt.Sprintf("%s (%s): %s", o.ID, o.Domain, firstLine(o.Brief)))
		}
	}
	for _, dec := range ps.Decisions {
		if dec.Failure == failurePlan {
			r.Earlier = append(r.Earlier, dec)
		}
	}
	return r
}

const (
	planTaskIntro = `You are the orchestrator for the <domain> domain of a crew mission. A mission is a job split into pieces; each piece is one factory run in its own git worktree and ends as one pull request, or as files handed over as a deliverable. Piece <id> has been handed to you whole. Split it into the pieces that do its work. You may read the repository to decide; you change nothing.

Rules crew checks. A plan that breaks one is refused, and the reasons for earlier refusals are in the data:
- every piece uses one of the factories listed in the data;
- every piece keeps to the paths listed in the data: give each the narrowest paths that cover its work, or leave paths out to use them all;
- a piece waits only for other pieces of your plan: name them in waits_for by the ids you give them;
- the plan holds no more pieces than the number left in the data.
`
	planDelegateRule = `- a piece that is itself several pieces may be {"id": "...", "delegate": true, "brief": "...", "paths": ["..."]} with no factory; it is planned later, the same way.
`
	planTaskRest = `Pieces whose paths don't overlap run at the same time. A piece that must build on another's result waits for it.
If <id> can't be planned until something outside it is done or decided, don't guess: reply with a question for the principal instead of pieces.
`
	planReplyRule = "Reply with exactly one fenced block tagged `plan` holding one JSON object:\n" +
		`{"pieces": [{"id": "<id>.1", "factory": "...", "brief": "...", "paths": ["..."], "waits_for": []}], "reason": "one line on why this split"}` + "\n" +
		`or {"question": "...", "reason": "..."}.`
)

// Prompt is the whole headless prompt: the task, the mission's data between
// two marker lines that nothing in the data can imitate, and the reply rule
// again.
func (r PlanRequest) Prompt() string {
	data := r.dataText()
	begin, end := markersFor(data)
	fill := strings.NewReplacer("<domain>", r.Piece.Domain, "<id>", r.Piece.ID).Replace
	task := planTaskIntro
	if r.MayDelegate {
		task += planDelegateRule
	}
	task += planTaskRest + "\n" + planReplyRule + "\n"
	return fill(task) + "\n" + dataNote + "\n\n" + begin + "\n" + data + end + "\n\n" + fill(planReplyRule) + "\n"
}

func (r PlanRequest) dataText() string {
	var b strings.Builder
	section(&b, "Mission", r.MissionID)
	section(&b, "Request (word for word)", r.Request)
	if len(r.Changes) > 0 {
		section(&b, principalChangesTitle, bulletList(r.Changes))
	}
	section(&b, "Piece "+r.Piece.ID, fmt.Sprintf("brief: %s\npaths: %s\nwaits for: %s",
		r.Piece.Brief, strings.Join(r.Piece.Paths, ", "), strings.Join(r.Piece.WaitsFor, ", ")))
	for _, a := range r.Above {
		section(&b, "Above "+a.ID, a.Brief)
	}
	paths := "(any)"
	if len(r.Paths) > 0 {
		paths = strings.Join(r.Paths, "\n")
	}
	section(&b, "Paths its pieces must keep to", paths)
	section(&b, "Domain briefing", strings.TrimSpace(r.Briefing))
	section(&b, "Factories", r.factoriesText())
	section(&b, "The mission's other pieces", strings.Join(r.Others, "\n"))
	section(&b, "Pieces left", r.PiecesLeft)
	section(&b, "Earlier tries at this plan", r.earlierText())
	return b.String()
}

func (r PlanRequest) factoriesText() string {
	lines := make([]string, len(r.Factories))
	for i, f := range r.Factories {
		lines[i] = fmt.Sprintf("%s: %s (stations: %s)", f.Name, f.Description, strings.Join(f.Stations, ", "))
	}
	return strings.Join(lines, "\n")
}

func (r PlanRequest) earlierText() string {
	lines := make([]string, len(r.Earlier))
	for i, d := range r.Earlier {
		lines[i] = fmt.Sprintf("refused: %s", d.Reason)
	}
	return strings.Join(lines, "\n")
}
