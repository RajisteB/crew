package mission

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/crewfile-dev/crew/internal/run"
	"github.com/crewfile-dev/crew/internal/source"
)

const (
	failureLimitHit    = "limit-hit"
	failureFailed      = "failed"
	failureUncommitted = "uncommitted"

	replyRule = "Reply with exactly one fenced block tagged `decision` holding one JSON object."
)

// allowedActions are what the orchestrator may choose.
var allowedActions = []string{"retry", "goto", "cancel", "rebrief", "add", "escalate"}

// Summary is everything the orchestrator is told about a stuck piece.
type Summary struct {
	MissionID, Request       string
	Changes                  []string // what the principal said in each amendment, in order
	Piece                    Piece
	Failure, Station         string
	Reason                   string // why the run stopped, as the run reported it
	LimitRoute               string
	Count                    int
	Max                      string
	Routes                   map[string]int // "station.route" → times taken
	Stations                 []string       // the piece's factory stations, for goto
	LastSummary              string
	LastNotes                []string
	Earlier                  []Decision
	CallsLeft, AdditionsLeft string
	StepsLeft                string // station steps left for the mission
	Allowed                  []string
	StepsDir                 string   // where the full outputs are
	Outside                  []string // for the orchestrator: the files outside the paths
	OutsideMore              int
	Scope                    *Scope // nil for the main orchestrator
}

// BuildSummary gathers (id is the mission id) what the orchestrator needs to settle a stuck piece.
func BuildSummary(id string, m Mission, ps PieceState, rs run.State, f source.Factory, last run.StepRecord, stepsDir string, lim Limits, additions, stepsUsed int) Summary {
	piece, _ := m.Piece(ps.ID)
	sum := Summary{
		MissionID: id, Request: m.Request, Changes: m.PrincipalChanges(), Piece: piece,
		Failure: failureOf(rs), Station: rs.Current, Reason: rs.Reason,
		LimitRoute: rs.LimitRoute, Routes: maps.Clone(rs.RouteCounts),
		Stations: f.StationNames(), LastSummary: last.Verdict.Summary,
		LastNotes: slices.Clone(last.Verdict.Notes), Earlier: slices.Clone(ps.Decisions),
		CallsLeft: left(lim.OrchestratorCalls, ps.Calls), AdditionsLeft: left(lim.Additions, additions),
		StepsLeft: left(lim.Steps, stepsUsed),
		Allowed:   allowedFor(failureOf(rs)), StepsDir: stepsDir,
	}
	if sum.Station == "" {
		sum.Station = last.Station
	}
	if rs.LimitRoute != "" {
		sum.Count = rs.RouteCounts[rs.LimitRoute]
		sum.Max = routeMax(f, rs.LimitRoute)
	}
	return sum
}

// failureOf names the kind of stop the run is in.
func failureOf(rs run.State) string {
	switch {
	case rs.LimitRoute != "":
		return failureLimitHit
	case rs.Status == run.StatusDone:
		return failureUncommitted
	}
	return failureFailed
}

// routeMax is the max written on the route "station.route", as text.
func routeMax(f source.Factory, key string) string {
	station, route, ok := strings.Cut(key, ".")
	if !ok {
		return ""
	}
	return f.Stations[station].Routes[route].Max.String()
}

// left is how much of a limit remains after used, or "*" when unlimited.
func left(l source.Limit, used int) string {
	if l.Unlimited {
		return "*"
	}
	return strconv.Itoa(max(l.N-used, 0))
}

func section(b *strings.Builder, title, body string) {
	if body == "" {
		body = "(none)"
	}
	b.WriteString("## " + title + "\n" + body + "\n\n")
}

func pieceText(p Piece) string {
	return fmt.Sprintf("id: %s\ndomain: %s\nfactory: %s\nbrief: %s\npaths: %s\nwaits for: %s",
		p.ID, p.Domain, p.Factory, p.Brief, strings.Join(p.Paths, ", "), strings.Join(p.WaitsFor, ", "))
}

func failureText(s Summary) string {
	t := fmt.Sprintf("failure: %s\nstation: %s\nreason: %s", s.Failure, s.Station, s.Reason)
	if s.LimitRoute != "" {
		t += fmt.Sprintf("\nroute that hit its limit: %s\ntaken: %d times\nmax: %s", s.LimitRoute, s.Count, s.Max)
	}
	return t
}

func routesText(routes map[string]int) string {
	keys := make([]string, 0, len(routes))
	for k := range routes {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	lines := make([]string, 0, len(keys))
	for _, k := range keys {
		lines = append(lines, fmt.Sprintf("%s: %d", k, routes[k]))
	}
	return strings.Join(lines, "\n")
}

func lastText(s Summary) string {
	if s.LastSummary == "" && len(s.LastNotes) == 0 {
		return ""
	}
	t := "summary: " + s.LastSummary
	for _, n := range s.LastNotes {
		t += "\n- " + n
	}
	return t
}

func decisionsText(ds []Decision) string {
	var lines []string
	for _, d := range ds {
		what := strings.Join(strings.Fields(d.Action+" "+d.Station+" "+d.Note), " ")
		lines = append(lines, fmt.Sprintf("- by %s for %s: %s (%s)", d.By, d.Failure, what, d.Reason))
	}
	return strings.Join(lines, "\n")
}
