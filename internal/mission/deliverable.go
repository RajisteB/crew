package mission

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"
)

const (
	scratchOut          = "deliverables" // <worktree>/.crew/deliverables: what the station leaves
	scratchIn           = "inputs"       // <worktree>/.crew/inputs/<piece>: earlier pieces' deliverables
	maxDeliverableFiles = 20
	maxPathBytes        = 255
)

// Found is what a finished run left in its deliverables folder.
type Found struct {
	Files []string
	Print string
	Extra int // files beyond maxDeliverableFiles, not taken
}

// Earlier is a deliverable an earlier piece produced, as this piece's input.
type Earlier struct {
	Piece string
	Files []string
}

// DeliverablesDir is where crew keeps the piece's collected deliverable.
func (s Store) DeliverablesDir(piece string) string {
	return filepath.Join(s.Dir, "deliverables", piece)
}

// listDelivered lists the regular files under dir, sorted, as slash-separated
// paths relative to dir. Symlinks and anything else that isn't a regular file
// or a directory are skipped, and a symlinked directory is not entered. A
// missing dir is an empty Found. Print is the hex SHA-256 of each taken
// file's path, size and modification time.
func listDelivered(dir string) (Found, error) {
	var all []string
	if info, err := os.Lstat(dir); err != nil || !info.IsDir() {
		if err == nil || errors.Is(err, fs.ErrNotExist) {
			return Found{}, nil // missing, a link or a file: nothing is read
		}
		return Found{}, err
	}
	if err := walkRegular(dir, "", &all); err != nil {
		if errors.Is(err, fs.ErrNotExist) && len(all) == 0 {
			return Found{}, nil
		}
		return Found{}, err
	}
	slices.Sort(all)
	taken := all[:min(len(all), maxDeliverableFiles)]
	h := sha256.New()
	for _, name := range taken {
		info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
		if err != nil {
			return Found{}, err
		}
		fmt.Fprintf(h, "%s\x00%d\x00%d\x00", name, info.Size(), info.ModTime().UnixNano())
	}
	return Found{Files: slices.Clone(taken), Print: hex.EncodeToString(h.Sum(nil)), Extra: len(all) - len(taken)}, nil
}

// unsafeRune is a control character, a line or paragraph separator, or a
// format character such as a bidi override.
func unsafeRune(r rune) bool {
	return unicode.IsControl(r) || unicode.In(r, unicode.Zl, unicode.Zp, unicode.Cf)
}

// safeName is false for a name that holds a control character or isn't UTF-8:
// names go into another agent's instructions and into messages.
func safeName(name string) bool {
	return utf8.ValidString(name) && !strings.ContainsFunc(name, unsafeRune)
}

// notPlainError says a folder crew needs is a link or a file.
type notPlainError struct{ Path string }

func (e *notPlainError) Error() string { return fmt.Sprintf("%s is not a plain folder", e.Path) }

// plainDir reports whether root/rel[0]/rel[1]/... exists and every part is a
// real directory. A missing part is false with no error; a part that is a
// symlink or a file is a *notPlainError.
func plainDir(root string, rel ...string) (bool, error) {
	cur := root
	for _, part := range rel {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return false, nil
		case err != nil:
			return false, err
		case !info.IsDir():
			return false, &notPlainError{Path: cur}
		}
	}
	return true, nil
}

// walkRegular appends the regular files below dir/rel, never following a link.
func walkRegular(dir, rel string, out *[]string) error {
	entries, err := os.ReadDir(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := path.Join(rel, e.Name())
		if !safeName(e.Name()) || len(name) > maxPathBytes {
			continue
		}
		switch {
		case e.Type().IsRegular():
			*out = append(*out, name)
		case e.IsDir():
			if err := walkRegular(dir, name, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFiles copies the named regular files from one folder to another,
// creating folders with mode 0700 and files with mode 0600. It refuses a name
// that is absolute or holds "..", a source that is or sits below a symlink,
// and a destination that already exists. Nothing is written when a name is
// refused.
func copyFiles(from, to string, files []string) error {
	for _, name := range files {
		if !filepath.IsLocal(filepath.FromSlash(name)) {
			return fmt.Errorf("%q is not a path inside the deliverables folder", name)
		}
	}
	if info, err := os.Lstat(to); err == nil && !info.IsDir() {
		return &notPlainError{Path: to}
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	for _, name := range files {
		if err := copyOne(from, to, filepath.FromSlash(name)); err != nil {
			return err
		}
	}
	return nil
}

func copyOne(from, to, name string) error {
	if err := checkNoLinks(from, name); err != nil {
		return err
	}
	src, err := os.OpenFile(filepath.Join(from, name), os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return err
	}
	defer src.Close()
	dst := filepath.Join(to, name)
	if err := os.MkdirAll(filepath.Dir(dst), dirMode); err != nil {
		return err
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, fileMode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, src); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// checkNoLinks fails when the file, or any folder on the way to it below from,
// is a symlink, or when the file is not a regular file.
func checkNoLinks(from, name string) error {
	parts := strings.Split(name, string(filepath.Separator))
	cur := from
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		last := i == len(parts)-1
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("%q is a symlink", name)
		case last && !info.Mode().IsRegular():
			return fmt.Errorf("%q is not a regular file", name)
		case !last && !info.IsDir():
			return fmt.Errorf("%q is not inside a folder", name)
		}
	}
	return nil
}

// collects reports whether the piece's run left a deliverable crew has not
// collected, or one that changed since; it then asks for the collection.
func (d *decider) collects(id string) bool {
	f := d.o.Delivered[id]
	cur := d.ps(id).Deliverable
	if len(f.Files) == 0 || (cur != nil && cur.Print == f.Print) {
		return false
	}
	d.add(CollectDeliverable{Piece: id})
	return true
}

// dropsTakenBack drops an unaccepted deliverable whose files the station
// has since removed.
func (d *decider) dropsTakenBack(id string) {
	p := d.ps(id)
	if p.Deliverable != nil && !p.Deliverable.Accepted && len(d.o.Delivered[id].Files) == 0 {
		p.Deliverable = nil
	}
}
