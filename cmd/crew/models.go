package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/crewfile-dev/crew/internal/catalog"
	"github.com/crewfile-dev/crew/internal/config"
)

// runModels handles `crew models [sync]`.
func runModels(dir string, args []string, f flags) {
	switch {
	case len(args) == 0:
		listModels(dir, f)
	case len(args) == 1 && args[0] == "sync":
		syncModels(dir)
	default:
		fail(2, "usage: crew models [--harness H | --all]  or  crew models sync")
	}
}

func syncModels(dir string) {
	reg, err := config.LoadRegistry(dir)
	if err != nil {
		fail(1, "%v", err)
	}
	if !hasCatalog(reg) {
		fmt.Println("nothing to sync: no harness in harnesses.yaml has a catalog")
		return
	}
	if !strings.HasPrefix(reg.CatalogSource, "https://") {
		fail(1, "%s: catalog_source must be an https:// URL (got %q)", config.RegistryFile, reg.CatalogSource)
	}
	api, err := catalog.Fetch(reg.CatalogSource)
	if err != nil {
		fail(1, "%v", err)
	}
	m, err := catalog.Build(api, reg, reg.CatalogSource, time.Now().Format("2006-01-02"))
	if err != nil {
		fail(1, "%v", err)
	}
	old, _ := config.LoadModels(dir) // a missing or broken file just means "everything is new"
	path := filepath.Join(dir, config.ModelsFile)
	if err := catalog.Write(path, m); err != nil {
		fail(1, "write %s: %v", path, err)
	}
	for _, hn := range unionHarnesses(old, m) {
		added, removed := diffIDs(old, m, hn)
		line := fmt.Sprintf("%-12s %3d models", hn, len(m.Harnesses[hn]))
		if len(added) > 0 {
			line += "  + " + strings.Join(added, ", ")
		}
		if len(removed) > 0 {
			line += "  - " + strings.Join(removed, ", ")
		}
		fmt.Println(line)
	}
	fmt.Printf("wrote %s (from %s)\nrun `crew verify` to check agents against the new list\n", path, reg.CatalogSource)
}

func hasCatalog(reg *config.Registry) bool {
	for _, h := range reg.Harnesses {
		if h.Catalog != nil {
			return true
		}
	}
	return false
}

func unionHarnesses(old, cur *config.Models) []string {
	all := map[string][]config.ModelEntry{}
	if old != nil {
		for k := range old.Harnesses {
			all[k] = nil
		}
	}
	for k := range cur.Harnesses {
		all[k] = nil
	}
	return sortedHarnesses(all)
}

// diffIDs lists models added to and removed from harness hn. When there was
// no previous file, nothing is reported as added (it would be every model).
func diffIDs(old, cur *config.Models, hn string) (added, removed []string) {
	if old == nil {
		return nil, nil
	}
	was, now := ids(old.Harnesses[hn]), ids(cur.Harnesses[hn])
	for id := range now {
		if !was[id] {
			added = append(added, id)
		}
	}
	for id := range was {
		if !now[id] {
			removed = append(removed, id)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed
}

func ids(es []config.ModelEntry) map[string]bool {
	m := make(map[string]bool, len(es))
	for _, e := range es {
		m[e.ID] = true
	}
	return m
}

func listModels(dir string, f flags) {
	models, err := config.LoadModels(dir)
	if err != nil {
		fail(1, "%v (run `crew models sync` to create it)", err)
	}
	show, err := harnessesToShow(dir, models, f)
	if err != nil {
		fail(2, "%v", err)
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for _, hn := range show {
		es := models.Harnesses[hn]
		fmt.Fprintf(tw, "\n%s — %d models\n", hn, len(es))
		fmt.Fprintln(tw, "  ID\tNAME\tEFFORTS\tCONTEXT\t$/MTok in · out")
		for _, e := range es {
			eff := strings.Join(e.Efforts, " ")
			if eff == "" {
				eff = "-"
			}
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\t%s\n", e.ID, e.Name, eff, tokens(e.Context), price(e))
		}
	}
	tw.Flush()
	fmt.Printf("\nlist fetched %s from %s; refresh with `crew models sync`\n", models.Fetched, models.Source)
}

// harnessesToShow: --harness H, --all, or (default) the enabled targets.
func harnessesToShow(dir string, models *config.Models, f flags) ([]string, error) {
	if f.harness != "" {
		if _, ok := models.Harnesses[f.harness]; !ok {
			return nil, fmt.Errorf("no models listed for %q (listed: %s)", f.harness, strings.Join(sortedHarnesses(models.Harnesses), ", "))
		}
		return []string{f.harness}, nil
	}
	if f.all {
		return sortedHarnesses(models.Harnesses), nil
	}
	lo, err := config.LoadCrew(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for hn, t := range lo.Targets {
		if _, ok := models.Harnesses[hn]; ok && t.IsEnabled() {
			out = append(out, hn)
		}
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("no enabled harness has a model list; try --all")
	}
	return out, nil
}

func tokens(n int) string {
	switch {
	case n <= 0:
		return "-"
	case n >= 1_000_000 && n%1_000_000 == 0:
		return fmt.Sprintf("%dM", n/1_000_000)
	case n >= 1000:
		return fmt.Sprintf("%dK", n/1000)
	}
	return fmt.Sprint(n)
}

func price(e config.ModelEntry) string {
	if e.InputPerMTok == 0 && e.OutputPerMTok == 0 {
		return "free / not listed"
	}
	return fmt.Sprintf("%.2f · %.2f", e.InputPerMTok, e.OutputPerMTok)
}

func sortedHarnesses(m map[string][]config.ModelEntry) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
