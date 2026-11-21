// Package scaffold writes a starter crew directory.
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:template
var tmpl embed.FS

// Init copies the template into dir. It never overwrites existing files.
func Init(dir string) ([]string, error) {
	var created []string
	root, _ := fs.Sub(tmpl, "template")
	// Check everything first so a refusal never leaves a half-written directory.
	if err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if _, serr := os.Stat(filepath.Join(dir, p)); serr == nil {
				return fmt.Errorf("%s already exists; refusing to overwrite", filepath.Join(dir, p))
			}
		}
		return err
	}); err != nil {
		return nil, err
	}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, p)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if _, err := os.Stat(dst); err == nil {
			return fmt.Errorf("%s already exists; refusing to overwrite", dst)
		}
		b, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			return err
		}
		created = append(created, p)
		return nil
	})
	return created, err
}

// MissionDefaults writes the mission settings file and the domains folder
// from the template into dir, each only when it is missing. It returns what
// it wrote, relative to dir. It never overwrites.
func MissionDefaults(dir string) ([]string, error) {
	var wrote []string
	settings := filepath.Join(dir, "missions.yaml")
	if _, err := os.Stat(settings); os.IsNotExist(err) {
		b, err := tmpl.ReadFile("template/missions.yaml")
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(settings, b, 0o644); err != nil {
			return nil, err
		}
		wrote = append(wrote, "missions.yaml")
	} else if err != nil {
		return nil, err
	}
	domains := filepath.Join(dir, "domains")
	if _, err := os.Stat(domains); os.IsNotExist(err) {
		if err := os.MkdirAll(domains, 0o755); err != nil {
			return nil, err
		}
		wrote = append(wrote, "domains/")
	} else if err != nil {
		return nil, err
	}
	return wrote, nil
}
