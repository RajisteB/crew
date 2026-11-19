package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/crewfile-dev/crew/internal/factory"
	"github.com/crewfile-dev/crew/internal/source"
)

const factoryUsage = `usage:
  crew factory list               [--dir D]
  crew factory diagram <name>     [--dir D]   print the factory as a Mermaid flowchart`

// factoryCmd handles `crew factory …`.
func factoryCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("factory", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dirFlag := fs.String("dir", "", "crew directory")
	pos, err := parseInterleaved(fs, args)
	if err != nil {
		return fmt.Errorf("%v\n%s", err, factoryUsage)
	}
	if len(pos) == 0 {
		return errors.New(factoryUsage)
	}
	dir, err := resolveDir(*dirFlag, false)
	if err != nil {
		return err
	}
	fs2, errs := source.LoadFactories(dir)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	switch pos[0] {
	case "list":
		for _, f := range fs2 {
			fmt.Fprintf(out, "%-24s %d stations  %s\n", f.Name, len(f.Stations), f.Description)
		}
		return nil
	case "diagram":
		if len(pos) != 2 {
			return errors.New(factoryUsage)
		}
		f, err := findFactory(fs2, pos[1])
		if err != nil {
			return err
		}
		fmt.Fprint(out, factory.Mermaid(f))
		return nil
	}
	return fmt.Errorf("unknown factory command %q\n%s", pos[0], factoryUsage)
}

func findFactory(fs []source.Factory, name string) (source.Factory, error) {
	var names []string
	for _, f := range fs {
		if f.Name == name {
			return f, nil
		}
		names = append(names, f.Name)
	}
	sort.Strings(names)
	return source.Factory{}, fmt.Errorf("no factory %q (have: %s)", name, strings.Join(names, ", "))
}

// parseInterleaved lets flags appear before, between or after positional
// arguments: `crew run say <id> --project P hello`.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}
