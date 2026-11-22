package mission

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListDelivered_SortedRelativePaths(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.md"), "b")
	writeFile(t, filepath.Join(dir, "a", "c.md"), "c")
	f, err := listDelivered(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a/c.md", "b.md"}; !slices.Equal(f.Files, want) {
		t.Fatalf("files = %v, want %v", f.Files, want)
	}
	if f.Print == "" || f.Extra != 0 {
		t.Fatalf("found = %+v", f)
	}
}

func TestListDelivered_SkipsSymlinks(t *testing.T) {
	dir, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "s")
	writeFile(t, filepath.Join(outside, "d", "x.md"), "x")
	writeFile(t, filepath.Join(dir, "real.md"), "r")
	if err := os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "d"), filepath.Join(dir, "linkdir")); err != nil {
		t.Fatal(err)
	}
	f, err := listDelivered(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"real.md"}; !slices.Equal(f.Files, want) {
		t.Fatalf("files = %v, want %v", f.Files, want)
	}
}

func TestListDelivered_MissingFolder(t *testing.T) {
	f, err := listDelivered(filepath.Join(t.TempDir(), "nope"))
	if err != nil || len(f.Files) != 0 || f.Extra != 0 {
		t.Fatalf("found %+v, err %v", f, err)
	}
}

func TestListDelivered_CapsAtTwenty(t *testing.T) {
	dir := t.TempDir()
	for i := range 23 {
		writeFile(t, filepath.Join(dir, "f"+string(rune('a'+i))+".md"), "x")
	}
	f, err := listDelivered(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Files) != 20 || f.Extra != 3 || f.Files[0] != "fa.md" {
		t.Fatalf("files %d extra %d first %q", len(f.Files), f.Extra, f.Files[0])
	}
}

func TestListDelivered_PrintChangesWithContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.md")
	writeFile(t, path, "one")
	first, _ := listDelivered(dir)
	again, _ := listDelivered(dir)
	if first.Print != again.Print {
		t.Fatal("print changed without a change")
	}
	writeFile(t, path, "longer content")
	changed, _ := listDelivered(dir)
	if changed.Print == first.Print {
		t.Fatal("print did not change when the file did")
	}
}

func TestCopyFiles_RefusesEscapes(t *testing.T) {
	from, to := t.TempDir(), filepath.Join(t.TempDir(), "out")
	writeFile(t, filepath.Join(from, "ok.md"), "ok")
	for _, name := range []string{"../x", "/etc/passwd"} {
		if err := copyFiles(from, to, []string{"ok.md", name}); err == nil {
			t.Fatalf("%q was accepted", name)
		}
		if _, err := os.Stat(filepath.Join(to, "ok.md")); err == nil {
			t.Fatalf("something was written before refusing %q", name)
		}
	}
}

func TestCopyFiles_Modes(t *testing.T) {
	from, to := t.TempDir(), filepath.Join(t.TempDir(), "out")
	writeFile(t, filepath.Join(from, "a", "b.md"), "hello")
	if err := copyFiles(from, to, []string{"a/b.md"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(to, "a", "b.md"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("info %v err %v", info, err)
	}
	if d, _ := os.Stat(filepath.Join(to, "a")); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", d.Mode().Perm())
	}
}

func TestCopyFiles_NeverFollowsSymlinks(t *testing.T) {
	from, to, outside := t.TempDir(), filepath.Join(t.TempDir(), "out"), t.TempDir()
	writeFile(t, filepath.Join(outside, "s.txt"), "secret")
	writeFile(t, filepath.Join(outside, "d", "x.md"), "x")
	if err := os.Symlink(filepath.Join(outside, "s.txt"), filepath.Join(from, "link.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(outside, "d"), filepath.Join(from, "linkdir")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link.txt", "linkdir/x.md"} {
		if err := copyFiles(from, to, []string{name}); err == nil {
			t.Fatalf("copied through a link: %s", name)
		}
	}
	// an existing destination is an error, not overwritten
	writeFile(t, filepath.Join(from, "a.md"), "new")
	writeFile(t, filepath.Join(to, "a.md"), "old")
	if err := copyFiles(from, to, []string{"a.md"}); err == nil {
		t.Fatal("overwrote an existing file")
	}
}

func TestPieceInput_Deliverables(t *testing.T) {
	m := Mission{Request: "x"}
	p := Piece{ID: "P6", Domain: "product", Factory: "feature", Brief: "b"}
	earlier := []Earlier{{Piece: "P5", Files: []string{"brief.md", "plan.md"}}}
	got := PieceInput("m-1", m, p, sourceDomain("d"), earlier)
	in := "## Deliverables from earlier pieces\n\n- P5: .crew/inputs/P5/brief.md, .crew/inputs/P5/plan.md\n\n"
	i, j, k := strings.Index(got, in), strings.Index(got, "\n## Deliverables\n\nIf your piece produces work"), strings.Index(got, "## Commit")
	if i < 0 || j < 0 || k < 0 || !(i < j && j < k) || strings.Index(got, "## Domain briefing") > i {
		t.Fatalf("sections wrong:\n%s", got)
	}
	if !strings.Contains(got, "Git ignores that folder: don't commit those files and don't move them. crew collects them when you finish.") {
		t.Fatalf("text:\n%s", got)
	}
	plain := PieceInput("m-1", m, p, sourceDomain("d"), nil)
	if strings.Contains(plain, "from earlier pieces") || !strings.Contains(plain, "## Deliverables\n") {
		t.Fatalf("plain:\n%s", plain)
	}
}

func deliveredObs(files ...string) Obs {
	o := obs(t0)
	o.Runs["P1"] = doneRun()
	o.Git["P1"] = GitObs{Ahead: 2}
	o.Delivered = map[string]Found{"P1": {Files: files, Print: "p1"}}
	return o
}

func TestDecide_CollectsBeforeOpeningAPR(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	_, as := Decide(m, s, deliveredObs("a.md"), limits(1, 3), 0)
	if len(as) != 1 || len(of[CollectDeliverable](as)) != 1 {
		t.Fatalf("want exactly [CollectDeliverable], got %+v", as)
	}
	s.Pieces[0].Deliverable = &Deliverable{Files: []string{"a.md"}, Print: "p1"}
	_, as = Decide(m, s, deliveredObs("a.md"), limits(1, 3), 0)
	if len(as) != 1 || len(of[OpenPR](as)) != 1 {
		t.Fatalf("want exactly [OpenPR], got %+v", as)
	}
}

func TestDecide_ChangedDeliverableIsCollectedAgain(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Deliverable = &Deliverable{Files: []string{"a.md"}, Print: "old"}
	_, as := Decide(m, s, deliveredObs("a.md"), limits(1, 3), 0)
	if len(as) != 1 || len(of[CollectDeliverable](as)) != 1 {
		t.Fatalf("got %+v", as)
	}
}

func TestDecide_DeliverableTakenBackIsDropped(t *testing.T) {
	m := mis(nil, "P1")
	s := st(m, PieceRunning)
	s.Pieces[0].Deliverable = &Deliverable{Files: []string{"a.md"}, Print: "p1"}
	ns, as := Decide(m, s, deliveredObs(), limits(1, 3), 0)
	if piece(t, ns, "P1").Deliverable != nil {
		t.Fatal("the deliverable should be dropped")
	}
	if len(of[OpenPR](as)) != 1 {
		t.Fatalf("decision should go on to OpenPR: %+v", as)
	}
	if s.Pieces[0].Deliverable == nil {
		t.Fatal("Decide modified its input")
	}
}

func TestListDelivered_LinkedRootIsEmpty(t *testing.T) {
	outside, base := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.txt"), "s")
	link := filepath.Join(base, "deliverables")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	f, err := listDelivered(link)
	if err != nil || len(f.Files) != 0 {
		t.Fatalf("found %+v, err %v", f, err)
	}
}

func TestPlainDir_LinkedCrewIsRefused(t *testing.T) {
	outside, wt := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(outside, "deliverables", "secret.txt"), "s")
	if err := os.Symlink(outside, filepath.Join(wt, ".crew")); err != nil {
		t.Fatal(err)
	}
	if ok, err := plainDir(wt, ".crew", "deliverables"); ok || err == nil {
		t.Fatalf("ok %v err %v", ok, err)
	}
}

func TestListDelivered_SkipsUnsafeNames(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ok.md"), "x")
	writeFile(t, filepath.Join(dir, "a\n## Commit\nx.md"), "x")
	writeFile(t, filepath.Join(dir, "d\tir", "in.md"), "x")
	writeFile(t, filepath.Join(dir, strings.Repeat("l", 200), strings.Repeat("m", 100)+".md"), "x")
	f, err := listDelivered(dir)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"ok.md"}; !slices.Equal(f.Files, want) {
		t.Fatalf("files = %v, want %v", f.Files, want)
	}
}

func TestCopyFiles_RefusesALinkedDestination(t *testing.T) {
	from, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(from, "a.md"), "a")
	link := filepath.Join(t.TempDir(), "to")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	if err := copyFiles(from, link, []string{"a.md"}); err == nil {
		t.Fatal("copied into a linked folder")
	}
	if _, err := os.Stat(filepath.Join(outside, "a.md")); err == nil {
		t.Fatal("file written through the link")
	}
}

func TestPlainDir(t *testing.T) {
	root, outside := t.TempDir(), t.TempDir()
	writeFile(t, filepath.Join(root, ".crew", "deliverables", "x"), "x")
	if ok, err := plainDir(root, ".crew", "deliverables"); !ok || err != nil {
		t.Fatalf("plain: %v %v", ok, err)
	}
	if ok, err := plainDir(root, ".crew", "nope"); ok || err != nil {
		t.Fatalf("missing: %v %v", ok, err)
	}
	if err := os.Symlink(outside, filepath.Join(root, ".crew", "inputs")); err != nil {
		t.Fatal(err)
	}
	if ok, err := plainDir(root, ".crew", "inputs"); ok || err == nil {
		t.Fatalf("link: %v %v", ok, err)
	}
	writeFile(t, filepath.Join(root, "file"), "x")
	if ok, err := plainDir(root, "file"); ok || err == nil {
		t.Fatalf("file: %v %v", ok, err)
	}
}

func TestSafeName(t *testing.T) {
	if !safeName("brief v2.md") {
		t.Error("a plain name was refused")
	}
	for _, name := range []string{"a\nb", "a b", "a b", "a‮b", "a\xffb"} {
		if safeName(name) {
			t.Errorf("%q was accepted", name)
		}
	}
}
