package mission

import (
	"fmt"
	"strings"
)

var mermaidReserved = map[string]bool{
	"end": true, "graph": true, "subgraph": true, "flowchart": true, "style": true,
	"class": true, "classdef": true, "click": true, "default": true, "linkstyle": true,
}

var labelEscaper = strings.NewReplacer(
	"#", "#35;", `"`, "#quot;", "<", "#lt;", ">", "#gt;",
	"\r\n", " ", "\n", " ", "\r", " ",
)

// Mermaid draws the plan as a flowchart: one node per piece, and an arrow
// from each piece to every piece that waits for it, and a dotted arrow from a
// delegated piece to each piece directly under it. It is safe on a plan that
// has not been checked.
func Mermaid(m Mission) string {
	keys, first := nodeKeys(m.Pieces)
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	for i, p := range m.Pieces {
		label := p.ID + " · " + p.Domain
		if p.Delegate {
			label += " · delegated"
		}
		fmt.Fprintf(&b, "    %s[\"%s\"]\n", keys[i], labelEscaper.Replace(label))
	}
	for i, p := range m.Pieces {
		for _, w := range p.WaitsFor {
			if j, ok := first[w]; ok {
				fmt.Fprintf(&b, "    %s --> %s\n", keys[j], keys[i])
			}
		}
	}
	for i, p := range m.Pieces {
		if j, ok := first[ParentID(p.ID)]; ok && ParentID(p.ID) != "" && m.Pieces[j].Delegate {
			fmt.Fprintf(&b, "    %s -.-> %s\n", keys[j], keys[i])
		}
	}
	return b.String()
}

// nodeKeys picks a diagram key per piece: its id when that is safe (a sub-piece's
// id has a ".", which Mermaid reads as syntax) and not taken, otherwise a generated one. first maps an id to its first piece.
func nodeKeys(pieces []Piece) (keys []string, first map[string]int) {
	used := map[string]bool{}
	first = map[string]int{}
	for _, p := range pieces {
		if idPattern.MatchString(p.ID) && !strings.Contains(p.ID, ".") {
			used[strings.ToLower(p.ID)] = true
		}
	}
	taken := map[string]bool{}
	for i, p := range pieces {
		if _, ok := first[p.ID]; !ok {
			first[p.ID] = i
		}
		k := p.ID
		if !idPattern.MatchString(k) || strings.Contains(k, ".") || mermaidReserved[strings.ToLower(k)] || taken[k] {
			k = fmt.Sprintf("node%d", i)
			for used[strings.ToLower(k)] || taken[k] {
				k += "x"
			}
		}
		taken[k] = true
		keys = append(keys, k)
	}
	return keys, first
}
