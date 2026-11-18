// Package scaffold writes a starter crewfile directory.
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
