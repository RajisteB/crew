package scaffold

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpgradeHarnesses_KeysAddedInFixedOrder(t *testing.T) {
	var first []byte
	for i := range 30 {
		dir := writeHarnesses(t, []byte("# mine\n"+strings.Replace(string(readFixture(t)), "verified: 2026-11-18", "verified: 2026-12-01", 1)))
		if _, err := UpgradeHarnesses(dir); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(dir, "harnesses.yaml"))
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		l, a := strings.Index(s, "orchestrator_launch"), strings.Index(s, "orchestrator_allow")
		if l < 0 || a < 0 || l > a {
			t.Fatalf("run %d: orchestrator_launch at %d, orchestrator_allow at %d", i, l, a)
		}
		if first == nil {
			first = b
		} else if !bytes.Equal(first, b) {
			t.Fatalf("run %d wrote different bytes", i)
		}
	}
}
