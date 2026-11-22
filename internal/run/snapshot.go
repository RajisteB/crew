package run

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/crewfile-dev/crew/internal/config"
)

// Snapshot fingerprints each watched root: its HEAD commit, its uncommitted
// diff and its untracked files, all excluding .crew/. watch entries are
// relative to project unless absolute; the map is keyed by the entry as
// written.
func Snapshot(project string, watch []string) (map[string]string, error) {
	out := make(map[string]string, len(watch))
	for _, w := range watch {
		root := w
		if !filepath.IsAbs(root) {
			root = filepath.Join(project, w)
		}
		fp, err := fingerprint(root)
		if err != nil {
			return nil, err
		}
		out[w] = fp
	}
	return out, nil
}

// Drifted lists the roots whose fingerprint changed, sorted. With no
// previous snapshot nothing counts as drift.
func Drifted(prev, cur map[string]string) []string {
	if len(prev) == 0 {
		return nil
	}
	var out []string
	for k, v := range cur {
		if p, ok := prev[k]; ok && p != v {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

var excludeCrew = ":(exclude)" + CrewDir

func fingerprint(root string) (string, error) {
	if _, err := git(root, "rev-parse", "--is-inside-work-tree"); err != nil {
		return "", fmt.Errorf("%s is not a git repository (factory watch:)", root)
	}
	h := sha256.New()
	head, err := git(root, "rev-parse", "HEAD")
	if err != nil {
		head = []byte("no-commits")
	}
	h.Write(head)
	for _, args := range [][]string{
		{"diff", "--binary", "--cached", "--", ".", excludeCrew},
		{"diff", "--binary", "--", ".", excludeCrew},
	} {
		b, err := git(root, args...)
		if err != nil {
			return "", err
		}
		h.Write(b)
	}
	untracked, err := git(root, "ls-files", "--others", "--exclude-standard", "-z", "--", ".", excludeCrew)
	if err != nil {
		return "", err
	}
	names := strings.Split(strings.TrimRight(string(untracked), "\x00"), "\x00")
	sort.Strings(names)
	for _, n := range names {
		if n == "" {
			continue
		}
		h.Write([]byte(n))
		if b, err := os.ReadFile(filepath.Join(root, n)); err == nil {
			sum := sha256.Sum256(b)
			h.Write(sum[:])
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func git(dir string, args ...string) ([]byte, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = config.CleanEnv()
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s in %s: %v: %s", strings.Join(args, " "), dir, err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}
