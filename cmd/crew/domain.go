package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/crewfile-dev/crew/internal/source"
)

const domainUsage = `usage:
  crew domain list   [--dir D]`

// domainCmd handles `crew domain …`.
func domainCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("domain", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirFlag := fs.String("dir", "", "crew directory")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, domainUsage)
	}
	if len(pos) == 0 {
		return errors.New(domainUsage)
	}
	if pos[0] != "list" {
		return fmt.Errorf("unknown domain command %q\n%s", pos[0], domainUsage)
	}
	dir, err := resolveDir(*dirFlag, false)
	if err != nil {
		return err
	}
	ds, errs := source.LoadDomains(dir)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	for _, d := range ds {
		fmt.Fprintf(out, "%-24s %d factories  %s\n", d.Name, len(d.Factories), d.Description)
	}
	return nil
}
