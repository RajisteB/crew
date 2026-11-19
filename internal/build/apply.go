package build

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Apply performs the actions. It refuses to do anything if there is a
// conflict. The ownership record is updated as each step completes and saved
// on every exit, so a run that fails halfway leaves an accurate record and
// the next compile picks up where it stopped.
func Apply(statePath string, acts []Action) (err error) {
	var conflicts []string
	for _, a := range acts {
		if a.Kind == Conflict {
			conflicts = append(conflicts, a.Path+": "+a.Reason)
		}
	}
	if len(conflicts) > 0 {
		return fmt.Errorf("refusing to write; %d conflict(s):\n  %s", len(conflicts), strings.Join(conflicts, "\n  "))
	}
	st, err := loadState(statePath)
	if err != nil {
		return err
	}
	touched := map[string]bool{}
	defer func() {
		if err == nil { // complete run: drop records nothing referred to
			for p := range st.Files {
				if !touched[p] {
					delete(st.Files, p)
				}
			}
		}
		if serr := saveState(statePath, st); err == nil {
			err = serr
		}
	}()

	// Deletes first, so an old copied folder is empty by the time its link is made.
	for _, a := range acts {
		switch a.Kind {
		case Delete:
			if err := deleteOwned(a.Path, st.Files[a.Path].Root); err != nil {
				return err
			}
			delete(st.Files, a.Path)
		case Forget:
			delete(st.Files, a.Path)
		}
	}
	for _, a := range acts {
		switch a.Kind {
		case Create, Update:
			if err := write(*a.File); err != nil {
				return err
			}
			fallthrough
		case Unchanged:
			f := a.File
			st.Files[a.Path] = stateEntry{f.want(), f.Kind, f.Harnesses, f.Root}
			touched[a.Path] = true
		}
	}
	return nil
}

// deleteOwned removes one recorded file or link. It re-checks that nothing
// above it became a link since Diff, and never removes a folder.
func deleteOwned(path, root string) error {
	if l := linkedAncestor(root, path); l != "" {
		return fmt.Errorf("%s: not deleting, %s is a link", path, l)
	}
	if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
		return fmt.Errorf("%s: not deleting, it is a folder", path)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	removeEmptyParents(filepath.Dir(path), root)
	return nil
}

func write(f File) error {
	if l := linkedAncestor(f.Root, f.Path); l != "" {
		return fmt.Errorf("%s: not writing, %s is a link", f.Path, l)
	}
	if f.Link == "" {
		return writeAtomic(f.Path, f.Content)
	}
	return linkAtomic(f.Path, f.Link)
}

// linkAtomic points path at target. An existing file or link is replaced
// atomically; an existing folder must already be empty apart from OS junk
// (Diff checked that it only held crew's own copies, deleted first).
func linkAtomic(path, target string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && fi.IsDir() {
		if err := removeEmptyTree(path); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
	}
	tmp, err := tempLink(filepath.Dir(path), target)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// tempLink creates a link with a random name next to its final location.
func tempLink(dir, target string) (string, error) {
	for i := 0; i < 5; i++ {
		var b [8]byte
		if _, err := rand.Read(b[:]); err != nil {
			return "", err
		}
		tmp := filepath.Join(dir, ".crew-link-"+hex.EncodeToString(b[:]))
		err := os.Symlink(target, tmp)
		if err == nil {
			return tmp, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return "", err
		}
	}
	return "", fmt.Errorf("%s: could not create a temporary link", dir)
}

// removeEmptyTree removes dir, its empty subfolders and OS junk files; it
// fails, leaving the rest, if any other file or a link is inside.
func removeEmptyTree(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(dir, e.Name())
		switch {
		case e.Type().IsDir():
			if err := removeEmptyTree(p); err != nil {
				return err
			}
		case e.Type().IsRegular() && junk(e.Name()):
			if err := os.Remove(p); err != nil {
				return err
			}
		default:
			return fmt.Errorf("folder still holds %s", e.Name())
		}
	}
	return os.Remove(dir)
}

// removeEmptyParents tidies real folders left empty by a delete (a copied
// skill's scripts/ and the skill folder itself). It stops at root, at the
// first non-empty folder, and never removes a link.
func removeEmptyParents(dir, root string) {
	for i := 0; i < maxDepth; i++ {
		if dir == root || dir == filepath.Dir(dir) || (root == "" && isHarnessRoot(dir)) {
			return
		}
		fi, err := os.Lstat(dir)
		if err != nil || !fi.IsDir() {
			return
		}
		if os.Remove(dir) != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

func writeAtomic(path string, content []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".crew-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
