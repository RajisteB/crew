package jiratest

import "strings"

// adfDoc is an Atlassian Document Format document: paragraphs of text nodes.
type adfDoc struct {
	Content []struct {
		Content []struct{ Text string }
	}
}

// text flattens the document to one line per paragraph.
func (d adfDoc) text() string {
	lines := make([]string, 0, len(d.Content))
	for _, p := range d.Content {
		var sb strings.Builder
		for _, n := range p.Content {
			sb.WriteString(n.Text)
		}
		lines = append(lines, sb.String())
	}
	return strings.Join(lines, "\n")
}
