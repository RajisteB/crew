package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/crewfile-dev/crewfile/internal/config"
	"github.com/crewfile-dev/crewfile/internal/source"
)

const notAvailable = "— not available"

// runCapabilities prints which native tools each capability becomes.
func runCapabilities(dir string, f flags) {
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		fail(1, "%v", err)
	}
	show, err := capabilityHarnesses(dir, reg, f)
	if err != nil {
		fail(2, "%v", err)
	}
	head, rows := capabilityTable(reg, show)
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(head, "\t"))
	for _, r := range rows {
		fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	tw.Flush()
	fmt.Println("\n" + notAvailable + ": an agent asking for it still compiles; the capability is dropped for that harness.")
	fmt.Println("(sandbox_mode) / (readonly): no per-agent tool list; edit, write or shell make the agent writable.")
}

// capabilityTable builds one row per capability (plus "all"), one column
// per harness.
func capabilityTable(reg *config.Registry, harnesses []string) ([]string, [][]string) {
	head := []string{"CAPABILITY"}
	for _, hn := range harnesses {
		head = append(head, reg.Harnesses[hn].Display)
	}
	var rows [][]string
	for _, c := range append(append([]string(nil), reg.Capabilities...), source.CapAll) {
		row := []string{c}
		for _, hn := range harnesses {
			row = append(row, capabilityCell(reg.Harnesses[hn], c))
		}
		rows = append(rows, row)
	}
	return head, rows
}

func capabilityCell(h config.Harness, c string) string {
	switch {
	case !h.Agents.Supported:
		return "(no custom agents)"
	case c == source.CapAll:
		return "every tool"
	case h.Agents.Fields.Tools == nil:
		keys := make([]string, 0, len(h.Agents.Derived))
		for _, d := range h.Agents.Derived {
			keys = append(keys, d.Key)
		}
		if len(keys) == 0 {
			return "(no tool control)"
		}
		return "(" + strings.Join(keys, ", ") + ")"
	case len(h.Capabilities[c]) == 0:
		return notAvailable
	}
	return strings.Join(h.Capabilities[c], ", ")
}

// capabilityHarnesses: --harness H, --all, or (default) the enabled targets.
func capabilityHarnesses(dir string, reg *config.Registry, f flags) ([]string, error) {
	if f.harness != "" {
		if _, ok := reg.Harnesses[f.harness]; !ok {
			return nil, fmt.Errorf("unknown harness %q", f.harness)
		}
		return []string{f.harness}, nil
	}
	var out []string
	if f.all {
		for hn := range reg.Harnesses {
			out = append(out, hn)
		}
	} else {
		lo, err := config.LoadCrewfile(dir)
		if err != nil {
			return nil, err
		}
		for hn, t := range lo.Targets {
			if _, ok := reg.Harnesses[hn]; ok && t.IsEnabled() {
				out = append(out, hn)
			}
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no harness is enabled; try --all")
	}
	return out, nil
}
