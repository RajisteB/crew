package build

import (
	"io/fs"
	"path/filepath"
	"sort"
)

// junk is a file the OS drops into folders; it never blocks replacing a
// copied folder with a link.
func junk(name string) bool { return name == ".DS_Store" }

// Diff compares planned files with disk and the ownership record.
func Diff(statePath string, files []File, force bool) ([]Action, error) {
	st, err := loadState(statePath)
	if err != nil {
		return nil, err
	}
	planned := map[string]bool{}
	for _, f := range files {
		planned[f.Path] = true
	}
	var acts []Action
	for i := range files {
		a, err := classify(&files[i], st, planned, force)
		if err != nil {
			return nil, err
		}
		acts = append(acts, a)
	}
	orphans, err := orphanActions(st, planned, force)
	if err != nil {
		return nil, err
	}
	return append(acts, orphans...), nil
}

// classify decides what to do with one planned output.
func classify(f *File, st *stateFile, planned map[string]bool, force bool) (Action, error) {
	conflict := func(why string) (Action, error) { return Action{Conflict, f.Path, why, f}, nil }
	if l := linkedAncestor(f.Root, f.Path); l != "" {
		return conflict(l + " is a link; crew never writes through one, even with --force (move it)")
	}
	d, err := inspect(f.Path)
	if err != nil {
		return Action{}, err
	}
	rec, owned := st.Files[f.Path]
	switch {
	case !d.exists:
		return Action{Create, f.Path, "", f}, nil
	case d.fingerprint == f.want():
		return Action{Unchanged, f.Path, "", f}, nil
	case d.isDir && f.Link != "" && onlyOwnedCopies(f.Path, st, planned):
		return Action{Update, f.Path, "replacing the copied folder with a link", f}, nil
	case d.isDir:
		// Apply could never empty it, so --force doesn't help here.
		return conflict("a folder crew didn't write (or that holds other files) is in the way; move it yourself")
	case !owned && !force:
		return conflict("file exists and was not written by crew (use --force to take it over)")
	case owned && d.fingerprint != rec.SHA256 && !force:
		return conflict("file was edited by hand since the last compile (edit the source instead, or --force)")
	}
	return Action{Update, f.Path, "", f}, nil
}

// orphanActions handles records of outputs that are no longer produced.
func orphanActions(st *stateFile, planned map[string]bool, force bool) ([]Action, error) {
	var paths []string
	for p := range st.Files {
		if !planned[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	var acts []Action
	for _, p := range paths {
		rec := st.Files[p]
		if l := linkedAncestor(rec.Root, p); l != "" {
			acts = append(acts, Action{Forget, p, l + " is now a link; dropping the record without deleting anything", nil})
			continue
		}
		d, err := inspect(p)
		switch {
		case err != nil:
			return nil, err
		case !d.exists:
			acts = append(acts, Action{Forget, p, "", nil}) // already gone
		case d.isDir:
			acts = append(acts, Action{Conflict, p, "no longer produced, and is now a folder; not deleting (remove it yourself)", nil})
		case d.fingerprint != rec.SHA256 && !force:
			acts = append(acts, Action{Conflict, p, "no longer produced, but edited by hand; not deleting (remove it yourself or --force)", nil})
		default:
			acts = append(acts, Action{Delete, p, "no longer produced by any source", nil})
		}
	}
	return acts, nil
}

// onlyOwnedCopies reports whether every file under dir is one crew wrote,
// is unchanged, and is about to be deleted as no longer produced, i.e. the
// folder is an old copy that a link may replace. OS junk files are ignored;
// anything else (including a link inside) makes it false.
func onlyOwnedCopies(dir string, st *stateFile, planned map[string]bool) bool {
	ok, found := true, false
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !ok {
			return err
		}
		if d.IsDir() || junk(d.Name()) {
			return nil
		}
		found = true
		rec, owned := st.Files[p]
		disk, _ := inspect(p)
		ok = d.Type().IsRegular() && owned && !planned[p] && disk.fingerprint == rec.SHA256
		return nil
	})
	return err == nil && ok && found
}
