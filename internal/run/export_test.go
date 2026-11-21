package run

import "github.com/crewfile-dev/crew/internal/config"

// ExpandCommandForTest exposes expandCommand to the external test package.
func ExpandCommandForTest(h config.HeadlessSpec, req StationRequest, settings string) []string {
	return expandCommand(h, req, settings)
}
