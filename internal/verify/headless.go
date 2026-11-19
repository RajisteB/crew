package verify

import (
	"regexp"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
)

var (
	headlessToken = regexp.MustCompile(`\{[^{}]*\}`)
	knownTokens   = map[string]bool{"{agent}": true, "{prompt}": true, "{allowed}": true}
	// Words that mark a permission bypass. crew never runs a station that way.
	bypassWords = []string{"dangerously", "bypass", "skip-permissions", "yolo"}
)

func checkHeadless(at string, h *config.HeadlessSpec, rep *Report) {
	if h == nil {
		return
	}
	at += ".headless"
	if len(h.Command) == 0 {
		rep.errf(at, "command is required")
		return
	}
	seen := map[string]bool{}
	for _, arg := range h.Command {
		low := strings.ToLower(arg)
		for _, w := range bypassWords {
			if strings.Contains(low, w) {
				rep.errf(at, "headless commands may not bypass permissions (%q); pre-allow the agent's tools instead", arg)
			}
		}
		for _, tok := range headlessToken.FindAllString(arg, -1) {
			if !knownTokens[tok] {
				rep.errf(at, "unknown token %q (known: {agent}, {prompt}, {allowed})", tok)
			}
			seen[tok] = true
		}
	}
	if !seen["{agent}"] {
		rep.errf(at, "command must contain {agent}")
	}
	switch h.PromptVia {
	case "stdin":
	case "arg":
		if !seen["{prompt}"] {
			rep.errf(at, "prompt_via: arg needs a {prompt} token in command")
		}
	default:
		rep.errf(at, "prompt_via must be \"stdin\" or \"arg\" (got %q)", h.PromptVia)
	}
	switch h.AllowVia {
	case "flag":
		if !seen["{allowed}"] {
			rep.errf(at, "allow_via: flag needs an {allowed} token in command")
		}
	case "agent-file":
	default:
		rep.errf(at, "allow_via must be \"flag\" or \"agent-file\" (got %q)", h.AllowVia)
	}
}
