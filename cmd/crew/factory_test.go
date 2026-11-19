package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/crewfile-dev/crew/internal/scaffold"
	"github.com/crewfile-dev/crew/internal/verify"
)

func scaffoldDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "crew")
	if _, err := scaffold.Init(dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestFactoryCmd_Diagram(t *testing.T) {
	var out bytes.Buffer
	if err := factoryCmd([]string{"diagram", "ship-change", "--dir", scaffoldDir(t)}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.String(), "flowchart LR\n") || !strings.Contains(out.String(), `s_review -.->|"fix ≤*"| s_build`) {
		t.Fatalf("diagram output:\n%s", out.String())
	}
}

func TestFactoryCmd_DiagramUnknown(t *testing.T) {
	err := factoryCmd([]string{"diagram", "nope", "--dir", scaffoldDir(t)}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `no factory "nope"`) || !strings.Contains(err.Error(), "ship-change") {
		t.Fatalf("got %v", err)
	}
}

func TestFactoryCmd_List(t *testing.T) {
	var out bytes.Buffer
	if err := factoryCmd([]string{"list", "--dir", scaffoldDir(t)}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ship-change") || !strings.Contains(out.String(), "3 stations") {
		t.Fatalf("list output:\n%s", out.String())
	}
}

func TestPrintMatrix_ListsForemen(t *testing.T) {
	p, _ := verify.Run(scaffoldDir(t))
	var out bytes.Buffer
	printMatrix(&out, p)
	if !strings.Contains(out.String(), "foremen: foreman-ship-change (launch: claude --agent foreman-ship-change)") {
		t.Fatalf("matrix:\n%s", out.String())
	}
}
