package main

import "slices"

// commonRunFlags are taken by every `crew run` verb.
var commonRunFlags = []string{"dir", "project"}

// verbRunFlags are the other flags each verb takes. A verb not named here
// starts a run of the factory of that name.
var verbRunFlags = map[string][]string{
	"status":  {"since", "wait"},
	"say":     nil,
	"goto":    nil,
	"approve": {"goto", "note", "detach"},
	"resume":  {"detach"},
}

// startRunFlags are the flags `crew run <factory>` takes.
var startRunFlags = []string{"input", "detach"}

// checkVerbFlags refuses, with a usage error, the first flag given that the
// verb pos[0] does not take.
func checkVerbFlags(pos, set []string) error {
	verb, takes := pos[0], startRunFlags
	if fl, ok := verbRunFlags[verb]; ok {
		takes = fl
	} else {
		verb = "<factory>"
	}
	for _, name := range set {
		if !slices.Contains(commonRunFlags, name) && !slices.Contains(takes, name) {
			return usageError{"crew run " + verb + " doesn't take --" + name}
		}
	}
	return nil
}
