package scaffold

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/crewfile-dev/crew/internal/config"
)

const (
	harnessesFile = "harnesses.yaml"
	backupFile    = "harnesses.yaml.before-missions"
	settingsToken = "{settings}"
)

// HarnessUpgrade reports what UpgradeHarnesses did.
type HarnessUpgrade struct {
	Added   []string // one line per harness changed
	Skipped []string // one line per harness left alone, with the reason
	Backup  string   // name of the copy made before writing; "" when nothing was written
}

// UpgradeHarnesses adds the missions parts of the template's harnesses to
// dir/harnesses.yaml, keeping comments and every existing value. A harness is
// upgraded as a whole or not at all. The original is copied to
// harnesses.yaml.before-missions first.
func UpgradeHarnesses(dir string) (HarnessUpgrade, error) {
	path := filepath.Join(dir, harnessesFile)
	orig, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return HarnessUpgrade{}, fmt.Errorf("no harnesses.yaml in %s; run crew init", dir)
	}
	if err != nil {
		return HarnessUpgrade{}, err
	}
	tmplBytes, err := tmpl.ReadFile("template/harnesses.yaml")
	if err != nil {
		return HarnessUpgrade{}, err
	}
	var doc, tdoc yaml.Node
	if err := yaml.Unmarshal(orig, &doc); err != nil {
		return HarnessUpgrade{}, fmt.Errorf("%s: %w", harnessesFile, err)
	}
	if err := yaml.Unmarshal(tmplBytes, &tdoc); err != nil {
		return HarnessUpgrade{}, err
	}
	have, want := harnessMap(&doc), harnessMap(&tdoc)
	if have == nil || want == nil {
		return HarnessUpgrade{}, fmt.Errorf("%s: no harnesses mapping", harnessesFile)
	}
	var res HarnessUpgrade
	for i := 0; i+1 < len(have.Content); i += 2 {
		name := have.Content[i].Value
		tn := mapGet(want, name)
		if tn == nil || have.Content[i+1].Kind != yaml.MappingNode {
			continue
		}
		added, skip := upgradeHarness(have.Content[i+1], tn)
		if skip != "" {
			res.Skipped = append(res.Skipped, name+": "+skip)
		} else if added != "" {
			res.Added = append(res.Added, name+": "+added)
		}
	}
	if len(res.Added) == 0 {
		return HarnessUpgrade{Skipped: res.Skipped}, nil
	}
	out, err := renderUpgrade(&doc, tmplBytes)
	if err != nil {
		return HarnessUpgrade{}, err
	}
	if err := writeWithBackup(dir, orig, out); err != nil {
		return HarnessUpgrade{}, err
	}
	res.Backup = backupFile
	return res, nil
}

// renderUpgrade encodes the upgraded tree after checking it passes the strict
// registry decode. Content equal to the template is written as the template.
func renderUpgrade(doc *yaml.Node, tmplBytes []byte) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	reg, err := config.DecodeRegistry(harnessesFile, buf.Bytes())
	if err != nil {
		return nil, fmt.Errorf("upgraded %s would not load, left it alone: %w", harnessesFile, err)
	}
	if treg, err := config.DecodeRegistry(harnessesFile, tmplBytes); err == nil && reflect.DeepEqual(reg, treg) {
		return tmplBytes, nil
	}
	return buf.Bytes(), nil
}

func writeWithBackup(dir string, orig, out []byte) error {
	path := filepath.Join(dir, harnessesFile)
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := fi.Mode().Perm()
	bak, err := os.OpenFile(filepath.Join(dir, backupFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("%s already exists; move it aside, then run crew mission setup again", backupFile)
		}
		return err
	}
	_, werr := bak.Write(orig)
	if cerr := bak.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return werr
	}
	tmp, err := os.CreateTemp(dir, harnessesFile+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(out); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func harnessMap(doc *yaml.Node) *yaml.Node {
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil
	}
	h := mapGet(doc.Content[0], "harnesses")
	if h == nil || h.Kind != yaml.MappingNode {
		return nil
	}
	return h
}

// mapGet returns the value node for key in a mapping node, or nil.
func mapGet(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func mapAdd(m *yaml.Node, key string, tmplKey, val *yaml.Node) {
	k := *tmplKey
	k.Value = key
	m.Content = append(m.Content, &k, val)
}

func scalars(n *yaml.Node) []string {
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]string, len(n.Content))
	for i, c := range n.Content {
		out[i] = c.Value
	}
	return out
}

// mapEntry returns the key and value nodes for key in m.
func mapEntry(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; m != nil && m.Kind == yaml.MappingNode && i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// upgradeHarness adds the missing missions keys of tn to h. It returns a
// description of what it added, or a reason for leaving the harness alone.
func upgradeHarness(h, tn *yaml.Node) (added, skip string) {
	needDenyVia := missingIn(h, tn, "headless", "deny_via")
	needDeny := mapGet(tn, "deny") != nil && mapGet(h, "deny") == nil
	needLaunch := missingIn(h, tn, "foreman", "orchestrator_launch")
	needAllow := missingIn(h, tn, "foreman", "orchestrator_allow")
	if !needDenyVia && !needDeny && !needLaunch && !needAllow {
		return "", ""
	}
	tcmd := mapGet(mapGet(tn, "headless"), "command")
	cmd := mapGet(mapGet(h, "headless"), "command")
	lacksSettings := cmd == nil || !slices.Contains(scalars(cmd), settingsToken)
	if needDenyVia && mapGet(h, "headless") != nil && lacksSettings && !slices.Equal(scalars(cmd), withoutSettings(scalars(tcmd))) {
		return "", "headless.command was changed by hand; add --settings {settings} to it, then run crew mission setup again"
	}
	var parts []string
	if needDeny {
		k, v := mapEntry(tn, "deny")
		mapAdd(h, "deny", k, v)
		parts = append(parts, "deny rules")
	}
	if needDenyVia {
		upgradeHeadless(h, tn, lacksSettings)
		parts = append(parts, "deny_via and {settings}")
	}
	if needLaunch || needAllow {
		parts = append(parts, upgradeForeman(h, tn, needLaunch, needAllow))
	}
	return strings.Join(parts, ", "), ""
}

func missingIn(h, tn *yaml.Node, section, key string) bool {
	return mapGet(mapGet(tn, section), key) != nil && mapGet(mapGet(h, section), key) == nil
}

// withoutSettings removes the {settings} element and the one before it.
func withoutSettings(cmd []string) []string {
	i := slices.Index(cmd, settingsToken)
	if i < 1 {
		return cmd
	}
	return slices.Concat(cmd[:i-1], cmd[i+1:])
}

func upgradeHeadless(h, tn *yaml.Node, replaceCommand bool) {
	th := mapGet(tn, "headless")
	hh := mapGet(h, "headless")
	if hh == nil {
		k, v := mapEntry(tn, "headless")
		mapAdd(h, "headless", k, v)
		return
	}
	if replaceCommand {
		for i := 0; i+1 < len(hh.Content); i += 2 {
			if hh.Content[i].Value == "command" {
				hh.Content[i+1] = mapGet(th, "command")
			}
		}
	}
	k, v := mapEntry(th, "deny_via")
	mapAdd(hh, "deny_via", k, v)
}

func upgradeForeman(h, tn *yaml.Node, launch, allow bool) string {
	tf := mapGet(tn, "foreman")
	hf := mapGet(h, "foreman")
	if hf == nil {
		hf = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		k, _ := mapEntry(tn, "foreman")
		mapAdd(h, "foreman", k, hf)
	}
	for key, need := range map[string]bool{"orchestrator_launch": launch, "orchestrator_allow": allow} {
		if need {
			k, v := mapEntry(tf, key)
			mapAdd(hf, key, k, v)
		}
	}
	if launch && allow {
		return "orchestrator launch and allow list"
	}
	if launch {
		return "orchestrator launch"
	}
	return "orchestrator allow list"
}
