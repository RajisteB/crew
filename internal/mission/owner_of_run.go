package mission

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// OwnerOfRun returns RunOwner(missionID, pieceID) when a mission of this repo names runID
// as a run of one of its pieces, and "" when none does.
func OwnerOfRun(repoRoot, runID string) (string, error) {
	if runID == "" {
		return "", nil
	}
	owners, err := RunOwners(repoRoot)
	if err != nil {
		return "", err
	}
	return owners[runID], nil
}

// RunOwners maps every run id the repo's mission states name to its owner,
// RunOwner(missionID, pieceID). A piece keeps one run for its whole life (a
// piece sent back from its pull request goes on in the same run), and
// sub-pieces of a delegated piece are pieces of the same state, so each
// piece's run_id is the only place a state records a run. Every folder under
// the missions folder that holds a state.json is read, whatever its name, so
// no mission is missed; a folder with no state.json is not a mission. A
// state that can't be read or parsed is an error naming the file: the caller
// can't tell which runs that mission owns. No missions folder is an empty map.
func RunOwners(repoRoot string) (map[string]string, error) {
	owners := map[string]string{}
	entries, err := os.ReadDir(missionsDir(repoRoot))
	if errors.Is(err, os.ErrNotExist) {
		return owners, nil
	}
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(missionsDir(repoRoot), e.Name(), "state.json")
		st, found, err := readStateFile(path)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		for _, p := range st.Pieces {
			if p.RunID != "" {
				owners[p.RunID] = RunOwner(e.Name(), p.ID)
			}
		}
	}
	return owners, nil
}

// readStateFile reads one mission's state.json. found is false when the file
// does not exist; any other failure is an error that names the file.
func readStateFile(path string) (State, bool, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return State{}, false, nil
	}
	st, err := Store{Dir: filepath.Dir(path)}.LoadState()
	if err != nil {
		return State{}, false, fmt.Errorf("mission state %s: %w", path, err)
	}
	return st, true, nil
}
