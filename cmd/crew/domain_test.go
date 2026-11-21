package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDomainList_Prints(t *testing.T) {
	dir := scaffoldDir(t)
	d := filepath.Join(dir, "domains")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: design\ndescription: UI work.\nfactories: [ship-change]\n---\nBody.\n"
	if err := os.WriteFile(filepath.Join(d, "design.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := domainCmd([]string{"list", "--dir", dir}, &out); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "design") || !strings.Contains(got, "1 factories") || !strings.Contains(got, "UI work.") {
		t.Fatalf("list output:\n%s", got)
	}
}
