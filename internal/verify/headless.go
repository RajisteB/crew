package verify

import (
	"regexp"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
)

var (
	headlessToken = regexp.MustCompile(`\{[^{}]*\}`)
	knownTokens   = map[string]bool{"{agent}": true, "{prompt}": true, "{allowed}": true, "{settings}": true}
	// Words and flags that mark a permission bypass or blanket auto-approval.
	// crew never runs a station that way.
	bypassWords = []string{"danger", "bypass", "skip-permissions", "yolo"}
	bypassArgs  = map[string]bool{"--auto": true, "--full-auto": true, "--auto-approve": true}
)

func checkHeadless(at string, hs *config.Harness, rep *Report) {
	h := hs.Headless
	if h == nil {
		return
	}
	at += ".headless"
	if len(h.Command) == 0 {
		rep.errf(at, "command is required")
		return
	}
	seen := map[string]bool{}
	for i, arg := range h.Command {
		low := strings.ToLower(arg)
		if bypassArgs[low] || (low == "auto" && i > 0 && strings.HasSuffix(h.Command[i-1], "permission-mode")) {
			rep.errf(at, "headless commands may not bypass permissions (%q auto-approves anything not denied); pre-allow the agent's tools instead", arg)
		}
		for _, w := range bypassWords {
			if strings.Contains(low, w) {
				rep.errf(at, "headless commands may not bypass permissions (%q); pre-allow the agent's tools instead", arg)
			}
		}
		for _, tok := range headlessToken.FindAllString(arg, -1) {
			if !knownTokens[tok] {
				rep.errf(at, "unknown token %q (known: {agent}, {prompt}, {allowed}, {settings})", tok)
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
	switch h.Output {
	case "", "text", "claude-json":
	default:
		rep.errf(at, "output must be \"text\" or \"claude-json\" (got %q)", h.Output)
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
	checkDenyVia(at, hs, seen["{settings}"], rep)
}

func checkDenyVia(at string, h *config.Harness, hasToken bool, rep *Report) {
	switch h.Headless.DenyVia {
	case "":
		if hasToken {
			rep.errf(at, "a {settings} token in command needs deny_via: settings-file")
		}
	case "settings-file":
		if !hasToken {
			rep.errf(at, "deny_via: settings-file needs a {settings} token in command")
		}
		if len(h.Deny.All) == 0 {
			rep.errf(at, "deny_via: settings-file needs a non-empty deny.all in the harness")
		}
	default:
		rep.errf(at, "deny_via must be \"settings-file\" or empty (got %q)", h.Headless.DenyVia)
	}
}
