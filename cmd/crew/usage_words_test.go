package main

import (
	"strings"
	"testing"
)

func TestUsage_RunInputTakesStdin(t *testing.T) {
	if !strings.Contains(usage, "crew run <factory> --input TEXT|@FILE|- [--detach]") {
		t.Error("the top-level usage does not show --input - for stdin")
	}
}
