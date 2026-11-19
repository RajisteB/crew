package run

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

// StepRecord is one finished step as stored in the run folder.
type StepRecord struct {
	N       int     `json:"n"`
	Station string  `json:"station"`
	Dir     string  `json:"dir"` // absolute steps/<n>-<station>
	Verdict Verdict `json:"verdict"`
}

// PromptInput is everything a station's prompt is built from.
type PromptInput struct {
	Factory    source.Factory
	Station    string
	Input      string
	Steps      []StepRecord
	Inbox      []string
	RetryError string
}

// BuildPrompt assembles the prompt for one station. Earlier outputs are
// referenced by path, never inlined, so the prompt stays small however long
// the run gets.
func BuildPrompt(in PromptInput) string {
	var b strings.Builder
	st := in.Factory.Stations[in.Station]
	fmt.Fprintf(&b, "# Factory: %s\n\n%s\n", in.Factory.Name, strings.TrimSpace(in.Factory.Body))
	fmt.Fprintf(&b, "\n## Your station: %s\n\nYou are one station in this factory. Do this station's part of the work, then hand off.\n", in.Station)
	fmt.Fprintf(&b, "\n## Run input\n\n%s\n", strings.TrimSpace(in.Input))
	b.WriteString("\n## Earlier steps\n\n")
	if len(in.Steps) == 0 {
		b.WriteString("You are the first station of this run.\n")
	}
	for _, s := range in.Steps {
		fmt.Fprintf(&b, "%d. %s → %s — %s (output: %s)\n", s.N, s.Station, s.Verdict.Route, s.Verdict.Summary, filepath.Join(s.Dir, "output.md"))
	}
	if len(in.Inbox) > 0 {
		b.WriteString("\n## Messages from the user\n\n")
		for _, m := range in.Inbox {
			fmt.Fprintf(&b, "- %s\n", m)
		}
	}
	if in.RetryError != "" {
		fmt.Fprintf(&b, "\n## Your previous reply was rejected\n\n%s. Finish again, following \"How to finish\" exactly.\n", in.RetryError)
	}
	writeFinish(&b, st)
	return b.String()
}

func writeFinish(b *strings.Builder, st source.Station) {
	b.WriteString("\n## How to finish\n\nPick exactly one route:\n\n")
	for _, rn := range st.RouteNames() {
		fmt.Fprintf(b, "- `%s` → %s\n", rn, st.Routes[rn].To)
	}
	b.WriteString("- `fail` → ends the run; use it only when the work can't continue and no route sends it back\n")
	b.WriteString(`
Do not ask the user questions. You can't reach them. If something you need is missing or wrong, pick the route that sends the work back (or ` + "`fail`" + `). If you had to make a call the user might disagree with, make your best call and put it in notes.

Your reply is your handoff: everything before the verdict becomes this station's output, which the next stations read. End your reply with exactly one fenced block tagged verdict:

` + "```verdict" + `
{"route": "<one route above>", "summary": "<one line: what you did>", "notes": ["<anything the user should know>"]}
` + "```\n")
}
