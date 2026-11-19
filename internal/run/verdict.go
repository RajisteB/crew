package run

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/crewfile-dev/crew/internal/source"
)

var verdictOpen = regexp.MustCompile("(?m)^```verdict[ \t]*$")

// ParseReply splits a station's reply into its output (everything except the
// last verdict block) and the verdict. allowed lists the station's routes;
// fail is always accepted.
func ParseReply(reply string, allowed []string) (string, Verdict, error) {
	opens := verdictOpen.FindAllStringIndex(reply, -1)
	if len(opens) == 0 {
		return "", Verdict{}, errors.New("no verdict block (end the reply with a ```verdict fenced block)")
	}
	start := opens[len(opens)-1]
	rest := reply[start[1]:]
	end := strings.Index(rest, "\n```")
	if end < 0 {
		return "", Verdict{}, errors.New("no verdict block: the ```verdict block is never closed")
	}
	var v Verdict
	if err := json.Unmarshal([]byte(rest[:end]), &v); err != nil {
		return "", Verdict{}, fmt.Errorf("verdict is not valid JSON: %v", err)
	}
	if v.Route == "" {
		return "", Verdict{}, errors.New("verdict route is empty")
	}
	if !contains(allowed, v.Route) && v.Route != source.RouteFail {
		return "", Verdict{}, fmt.Errorf("verdict route %q is not one of: %s", v.Route, strings.Join(append(append([]string(nil), allowed...), source.RouteFail), ", "))
	}
	after := rest[end+len("\n```"):]
	out := strings.TrimSpace(reply[:start[0]] + after)
	return out + "\n", v, nil
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
