package mission

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	changesMarker  = "<!-- crew:changes -->"
	changesHeading = "## Changes"
	fieldSep       = " · "
	fence          = "---\n"
)

// Piece is one unit of a mission: a domain, a factory and what to wait for.
type Piece struct {
	ID       string   `yaml:"id" json:"id"`
	Domain   string   `yaml:"domain" json:"domain"`
	Factory  string   `yaml:"factory" json:"factory"`
	Delegate bool     `yaml:"delegate" json:"delegate"`
	Brief    string   `yaml:"brief" json:"brief"`
	Paths    []string `yaml:"paths" json:"paths"`
	WaitsFor []string `yaml:"waits_for" json:"waits_for"`
	// Apart is written only by crew: pieces whose paths overlap this one's and
	// that must be done before it starts.
	Apart []string `yaml:"apart_from,omitempty" json:"apart_from,omitempty"`
}

// Change is one line of the change log.
type Change struct {
	Time          time.Time
	By, What, Why string
}

// Mission is a parsed mission.md.
type Mission struct {
	Pieces  []Piece
	Request string // the body, byte for byte
	Changes []Change
}

type frontmatter struct {
	Pieces []Piece `yaml:"pieces"`
}

// Parse reads a mission file. The request is everything between the
// frontmatter and the last changes-marker line, minus the newline Render
// adds before that marker.
func Parse(src []byte) (Mission, error) {
	text := string(src)
	if !strings.HasPrefix(text, fence) {
		return Mission{}, errors.New("mission file must start with a '---' frontmatter block")
	}
	rest := text[len(fence):]
	var yamlText string
	switch {
	case strings.HasPrefix(rest, fence):
		rest = rest[len(fence):]
	default:
		i := strings.Index(rest, "\n"+fence)
		if i < 0 {
			return Mission{}, errors.New("frontmatter block is not closed with '---'")
		}
		yamlText, rest = rest[:i+1], rest[i+1+len(fence):]
	}
	pieces, err := decodePieces(yamlText)
	if err != nil {
		return Mission{}, err
	}
	request, log, err := splitChanges(rest)
	if err != nil {
		return Mission{}, err
	}
	changes, err := parseChanges(log)
	if err != nil {
		return Mission{}, err
	}
	return Mission{Pieces: pieces, Request: request, Changes: changes}, nil
}

func decodePieces(y string) ([]Piece, error) {
	var fm frontmatter
	dec := yaml.NewDecoder(strings.NewReader(y))
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("frontmatter: %w", err)
	}
	return fm.Pieces, nil
}

// splitChanges splits on the last marker line. The marker must start a line
// and end one, and Render always puts a newline before it.
func splitChanges(body string) (request, log string, err error) {
	marker := "\n" + changesMarker
	for end := len(body); ; {
		i := strings.LastIndex(body[:end], marker)
		if i < 0 {
			return "", "", fmt.Errorf("missing the %s line", changesMarker)
		}
		after := body[i+len(marker):]
		if after == "" || strings.HasPrefix(after, "\n") {
			return body[:i], strings.TrimPrefix(after, "\n"), nil
		}
		end = i
	}
}

func parseChanges(log string) ([]Change, error) {
	var out []Change
	started := false
	for _, line := range strings.Split(log, "\n") {
		switch {
		case !started && line == changesHeading:
			started = true
		case line == "":
		case !started:
			return nil, fmt.Errorf("changes: expected %q, got %q", changesHeading, line)
		default:
			c, err := parseChange(line)
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	return out, nil
}

func parseChange(line string) (Change, error) {
	body, ok := strings.CutPrefix(line, "- ")
	parts := strings.SplitN(body, fieldSep, 4)
	if !ok || len(parts) != 4 {
		return Change{}, fmt.Errorf("changes: %q is not '- <time> · <by> · <what> · <why>'", line)
	}
	t, err := time.Parse(time.RFC3339, parts[0])
	if err != nil {
		return Change{}, fmt.Errorf("changes: %q: bad time: %w", line, err)
	}
	return Change{Time: t, By: parts[1], What: parts[2], Why: parts[3]}, nil
}

// Render writes the mission file. Parse(m.Render()) returns the same
// request and changes.
func (m Mission) Render() []byte {
	var b bytes.Buffer
	pieces := m.Pieces
	if pieces == nil {
		pieces = []Piece{}
	}
	y, err := yaml.Marshal(frontmatter{Pieces: pieces})
	if err != nil { // plain strings and slices always marshal
		panic(fmt.Sprintf("mission: render frontmatter: %v", err))
	}
	b.WriteString(fence)
	b.Write(y)
	b.WriteString(fence)
	b.WriteString(m.Request)
	b.WriteString("\n" + changesMarker + "\n" + changesHeading + "\n")
	for _, c := range m.Changes {
		fmt.Fprintf(&b, "- %s%s%s%s%s%s%s\n", c.Time.Format(time.RFC3339), fieldSep,
			field(c.By), fieldSep, field(c.What), fieldSep, oneLine(c.Why))
	}
	return b.Bytes()
}

var lineBreaks = strings.NewReplacer("\r\n", " ", "\n", " ", "\r", " ")

func oneLine(s string) string { return lineBreaks.Replace(s) }

// field is a change field that is not the last one: it may not hold the
// separator, so its middle dot becomes a hyphen.
func field(s string) string { return strings.ReplaceAll(oneLine(s), "·", "-") }

// Piece finds a piece by id.
func (m Mission) Piece(id string) (Piece, bool) {
	for _, p := range m.Pieces {
		if p.ID == id {
			return p, true
		}
	}
	return Piece{}, false
}

// WithChange returns a copy of m with c appended to the change log.
func (m Mission) WithChange(c Change) Mission {
	out := m
	out.Pieces = append([]Piece(nil), m.Pieces...)
	out.Changes = append(append([]Change(nil), m.Changes...), c)
	return out
}

// principalChangesTitle heads the list of the principal's amendments in a
// piece's input and in the orchestrator's prompt.
const principalChangesTitle = "Changes the principal made after the start (where they differ from the request, the latest change wins)"

// PrincipalChanges returns what the principal said in each amendment, in
// order. The orchestrator's own additions and re-briefs are not included. An
// amendment that made several changes logs its words once per change, so
// consecutive entries made at the same time with the same words count once.
func (m Mission) PrincipalChanges() []string {
	var out []string
	var lastTime time.Time
	for _, c := range m.Changes {
		if c.By != principalBy {
			continue
		}
		if n := len(out); n > 0 && c.Time.Equal(lastTime) && out[n-1] == c.Why {
			continue
		}
		out = append(out, c.Why)
		lastTime = c.Time
	}
	return out
}
