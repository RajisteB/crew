package source

import "path/filepath"

// DomainsDir holds domains/<name>.md.
const DomainsDir = "domains"

// Domain is domains/<name>.md: a named area of work that lists the factories
// it may use. The body is the briefing for that area.
type Domain struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Factories   []string `yaml:"factories"`
	Paths       []string `yaml:"paths"`
	Releases    []string `yaml:"releases"`
	Body, Path  string   // yaml:"-"
}

// LoadDomains parses every domains/*.md, reporting failures per file. A
// missing folder is zero domains.
func LoadDomains(root string) ([]Domain, []error) {
	var out []Domain
	var errs []error
	files, err := listMarkdown(filepath.Join(root, DomainsDir))
	if err != nil {
		return nil, []error{err}
	}
	for _, f := range files {
		var d Domain
		body, err := parse(f, &d)
		if err != nil {
			errs = append(errs, &FileError{rel(root, f), err})
			continue
		}
		if err := checkPaths(d.Paths); err != nil {
			errs = append(errs, &FileError{rel(root, f), err})
			continue
		}
		d.Body, d.Path = body, rel(root, f)
		out = append(out, d)
	}
	return out, errs
}
