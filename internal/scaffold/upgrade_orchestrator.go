package scaffold

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"gopkg.in/yaml.v3"
)

// cutoverAllow are the rules for plain runs that the orchestrator's session
// allows. They are the last four rules of the template's allow list, so
// appending them turns an older list into the template's.
var cutoverAllow = []string{"Bash(crew run status:*)", "Bash(crew run say:*)", "Bash(crew run resume:*)", "Bash(crew factory diagram:*)"}

const (
	foremanKey      = "foreman"
	orchestratorKey = "orchestrator"
)

// renameForeman turns h's foreman: block into orchestrator:, taking the key's
// comments from tn when tn has the block. It reports whether h changed. A
// harness that has both keys is left alone.
func renameForeman(h, tn *yaml.Node) bool {
	fk, fv := mapEntry(h, foremanKey)
	if fv == nil || mapGet(h, orchestratorKey) != nil {
		return false
	}
	var kept []*yaml.Node
	if fv.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(fv.Content); i += 2 {
			k := fv.Content[i]
			switch k.Value {
			case "launch": // started foremen
				continue
			case "orchestrator_launch":
				k.Value = "launch"
			case "orchestrator_allow":
				k.Value = "allow"
			}
			kept = append(kept, k, fv.Content[i+1])
		}
	}
	tk, tv := mapEntry(tn, orchestratorKey)
	if len(kept) == 0 && tv == nil {
		removeKey(h, foremanKey)
		return true
	}
	fk.Value = orchestratorKey
	if tk != nil {
		fk.HeadComment, fk.LineComment, fk.FootComment = tk.HeadComment, tk.LineComment, tk.FootComment
	}
	if len(kept) == 0 {
		fv.Style = 0
	}
	fv.Kind, fv.Tag, fv.Content = yaml.MappingNode, "!!map", kept
	return true
}

func removeKey(m *yaml.Node, key string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = slices.Delete(m.Content, i, i+2)
			return
		}
	}
}

// addCutoverAllow appends to h's orchestrator.allow each rule of cutoverAllow
// that tn's list has and h's lacks. It reports whether h changed.
func addCutoverAllow(h, tn *yaml.Node) bool {
	have := mapGet(mapGet(h, orchestratorKey), "allow")
	want := mapGet(mapGet(tn, orchestratorKey), "allow")
	if have == nil || have.Kind != yaml.SequenceNode || want == nil {
		return false
	}
	changed := false
	for _, rule := range cutoverAllow {
		if slices.Contains(scalars(have), rule) {
			continue
		}
		for _, el := range want.Content {
			if el.Value == rule {
				c := *el
				c.HeadComment, c.LineComment, c.FootComment = "", "", ""
				have.Content = append(have.Content, &c)
				changed = true
			}
		}
	}
	return changed
}

// addOrchestrator adds the template's launch and allow to h's orchestrator
// block, creating the block when h has none, for each that h lacks. It returns
// what it added, or "".
func addOrchestrator(h, tn *yaml.Node) string {
	launch := missingIn(h, tn, orchestratorKey, "launch")
	allow := missingIn(h, tn, orchestratorKey, "allow")
	if !launch && !allow {
		return ""
	}
	ho := mapGet(h, orchestratorKey)
	if ho == nil {
		ho = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		k, _ := mapEntry(tn, orchestratorKey)
		mapAdd(h, orchestratorKey, k, ho)
	}
	for _, add := range []struct {
		key  string
		need bool
	}{{"launch", launch}, {"allow", allow}} {
		if add.need {
			k, v := mapEntry(mapGet(tn, orchestratorKey), add.key)
			mapAdd(ho, add.key, k, v)
		}
	}
	switch {
	case launch && allow:
		return "orchestrator launch and allow list"
	case launch:
		return "orchestrator launch"
	}
	return "orchestrator allow list"
}

// backupName is the first name not taken in dir: harnesses.yaml.before-missions,
// then .before-missions.2 to .before-missions.9.
func backupName(dir string) (string, error) {
	name := backupFile
	for n := 2; n <= 10; n++ {
		_, err := os.Lstat(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return name, nil
		}
		if err != nil {
			return "", err
		}
		if n == 10 {
			break
		}
		name = fmt.Sprintf("%s.%d", backupFile, n)
	}
	return "", fmt.Errorf("%s already exists; move it aside, then run crew mission setup again", name)
}
