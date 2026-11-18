package build

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// SeedStateForTest records files as written by crewfile, the way an older
// copy-based compile would have.
func SeedStateForTest(t *testing.T, statePath string, files map[string][]byte) {
	t.Helper()
	st, err := loadState(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for p, b := range files {
		st.Files[p] = stateEntry{SHA256: hashOf(b), Kind: "seed"}
	}
	b, _ := json.MarshalIndent(st, "", "  ")
	os.MkdirAll(filepath.Dir(statePath), 0o755)
	if err := os.WriteFile(statePath, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
