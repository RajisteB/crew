package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/crewfile-dev/crew/internal/scaffold"
)

// printUpgrade tells what crew mission setup did to harnesses.yaml.
func printUpgrade(out io.Writer, dir string, up scaffold.HarnessUpgrade) {
	if up.Backup != "" {
		fmt.Fprintf(out, "updated %s (copy of the old file: %s)\n", filepath.Join(dir, "harnesses.yaml"), filepath.Join(dir, up.Backup))
		for _, a := range up.Added {
			fmt.Fprintf(out, "  %s\n", a)
		}
	}
	for _, s := range up.Skipped {
		fmt.Fprintf(out, "skipped %s\n", s)
	}
}
