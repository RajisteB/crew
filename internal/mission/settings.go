// Package mission holds the settings, state and limits for missions.
package mission

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/crewfile-dev/crew/internal/source"
	"gopkg.in/yaml.v3"
)

// SettingsFile is the settings file name in the crew config directory.
const SettingsFile = "missions.yaml"

// Limits are the caps crew enforces on a mission.
type Limits struct {
	Concurrency       source.Limit `yaml:"runs_at_once"`
	Depth             source.Limit `yaml:"delegation_depth"`
	Pieces            source.Limit `yaml:"pieces"`
	Steps             source.Limit `yaml:"steps"`
	OrchestratorCalls source.Limit `yaml:"orchestrator_calls_per_piece"`
	Additions         source.Limit `yaml:"orchestrator_additions"`
}

// Settings is the parsed missions.yaml.
type Settings struct {
	Version     int               `yaml:"version"`
	Approval    string            `yaml:"mission_approval"`     // "on" | "off"
	Harness     string            `yaml:"orchestrator_harness"` // harness that runs headless orchestrator calls
	RemindAfter string            `yaml:"remind_after"`         // Go duration
	Limits      Limits            `yaml:"limits"`
	Repos       map[string]Limits `yaml:"repos"` // "owner/name" -> overrides; unset fields inherit
}

// LoadSettings reads and validates missions.yaml from crewDir.
func LoadSettings(crewDir string) (Settings, error) {
	path := filepath.Join(crewDir, SettingsFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return Settings{}, fmt.Errorf("%s not found in %s; run crew mission setup", SettingsFile, crewDir)
	}
	if err != nil {
		return Settings{}, err
	}
	var s Settings
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&s); err != nil {
		if errors.Is(err, io.EOF) {
			return Settings{}, fmt.Errorf("%s: file is empty", SettingsFile)
		}
		return Settings{}, fmt.Errorf("%s: %w", SettingsFile, err)
	}
	if err := s.validate(); err != nil {
		return Settings{}, fmt.Errorf("%s: %w", SettingsFile, err)
	}
	return s, nil
}

func (s Settings) validate() error {
	if s.Approval != "on" && s.Approval != "off" {
		return fmt.Errorf(`mission_approval must be "on" or "off", got %q`, s.Approval)
	}
	if s.RemindAfter != "" {
		if _, err := time.ParseDuration(s.RemindAfter); err != nil {
			return fmt.Errorf("remind_after: %w", err)
		}
	}
	named := []struct {
		key string
		l   source.Limit
	}{
		{"runs_at_once", s.Limits.Concurrency},
		{"delegation_depth", s.Limits.Depth},
		{"pieces", s.Limits.Pieces},
		{"steps", s.Limits.Steps},
		{"orchestrator_calls_per_piece", s.Limits.OrchestratorCalls},
		{"orchestrator_additions", s.Limits.Additions},
	}
	for _, n := range named {
		if !n.l.Set {
			return fmt.Errorf(`limits.%s: a number or "*" is required`, n.key)
		}
	}
	return nil
}

// For returns the top-level limits with repo's overrides applied.
func (s Settings) For(repo string) Limits {
	out := s.Limits
	o, ok := s.Repos[repo]
	if !ok {
		return out
	}
	pick := func(base *source.Limit, over source.Limit) {
		if over.Set {
			*base = over
		}
	}
	pick(&out.Concurrency, o.Concurrency)
	pick(&out.Depth, o.Depth)
	pick(&out.Pieces, o.Pieces)
	pick(&out.Steps, o.Steps)
	pick(&out.OrchestratorCalls, o.OrchestratorCalls)
	pick(&out.Additions, o.Additions)
	return out
}

// Remind is how long an unanswered mission waits before a reminder.
// It is zero when remind_after is unset.
func (s Settings) Remind() time.Duration {
	d, _ := time.ParseDuration(s.RemindAfter)
	return d
}

// Allows reports whether n is within l. An unlimited limit allows anything.
func Allows(l source.Limit, n int) bool {
	return l.Unlimited || n <= l.N
}
