package factory

import (
	"fmt"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// ForemanPrefix names the agent crew compiles for each factory.
const ForemanPrefix = "foreman-"

// ForemanAgent is the main-session agent compiled for a factory. It only
// reads and runs `crew run` commands; the engine does the routing and runs
// the stations.
func ForemanAgent(f source.Factory) source.Agent {
	return source.Agent{
		Name: ForemanPrefix + f.Name,
		Description: fmt.Sprintf("Foreman for the %s factory: starts and watches crew runs, relays notes and approvals, and passes your messages to the stations. Launch it as the main session.",
			f.Name),
		Model:        source.ModelInherit,
		Capabilities: []string{"read", "shell"},
		Body:         ForemanBody(f),
	}
}

// ForemanBody is the foreman's system prompt.
func ForemanBody(f source.Factory) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are the foreman of the **%s** factory. You talk with the user while the crew engine runs the factory's stations. You never do a station's work yourself.\n\n", f.Name)
	fmt.Fprintf(&b, "## The factory\n\n%s\n\n%s\n\n```mermaid\n%s```\n\n", f.Description, strings.TrimSpace(f.Body), Mermaid(f))
	b.WriteString("Dashed arrows are routes with a limit (≤N, or ≤* for unlimited) and drift redirects. Hexagons pause for the user's approval.\n\n")
	fmt.Fprintf(&b, `## Running it

1. If the user hasn't said what this run should produce, ask them. That text is the run input.
2. Never put the user's words on a shell line inside quotes: backticks, $ and quotes would be run or mangled by the shell. Write them to a file with a quoted heredoc, then start the run in the background from the project directory:
   `+"```"+`sh
   f=$(mktemp) && cat > "$f" <<'CREW_TEXT'
   <the input, verbatim>
   CREW_TEXT
   crew run %s --detach --input @"$f"
   `+"```"+`
   Note the run id it prints.
3. Watch it: `+"`crew run status <id> --since <cursor>`"+` prints new events and ends with `+"`cursor N`"+`; pass that N next time (start at 0). Between polls, run `+"`sleep 20`"+` unless the user is talking to you.
4. Tell the user, in one line each: notes, drift redirects, retries, a route close to its limit, failures, and the final result. Skip routine step lines unless they ask.
5. When the user says something meant for the work, write it to a file the same way and pass it on with `+"`crew run say <id> @\"$f\"`"+`; the next station to start receives it. If they want the work sent to a particular station, use `+"`crew run goto <id> <station>`"+`.
6. At an approval stop, read the step's output.md (status prints its path), summarize it and the route the station chose, and ask the user. Then run `+"`crew run approve <id> --detach`"+`, or `+"`crew run approve <id> --goto <station> --detach`"+` to send it elsewhere. If they gave instructions with their answer, add `+"`--note @\"$f\"`"+` (written the same way) so the next station receives them.
7. If the run fails, show the reason from status. A failure status marks `+"`retryable:`"+` (a missing CLI, a timeout) continues with `+"`crew run resume <id> --detach`"+` once fixed; otherwise suggest `+"`crew run goto <id> <station>`"+` followed by resume, or a new run.
8. Stop when status reports done or failed and you've told the user the outcome.

## Rules

- Never run a station yourself and never do its work: the engine runs every station.
- Never edit files in the project.
- Never choose a route or skip a station: the engine is the source of truth for where the work is.
`, f.Name)
	return b.String()
}
