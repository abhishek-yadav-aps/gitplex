package gitplex

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type State struct {
	ManifestPath string               `json:"manifest_path"`
	Workspace    string               `json:"workspace"`
	Branch       string               `json:"branch,omitempty"`
	Repos        map[string]RepoState `json:"repos"`
}

type RepoState struct {
	URL  string `json:"url"`
	Path string `json:"path"`
	Head string `json:"head"`
}

func statePath(root string) string {
	return filepath.Join(root, ".gitplex", "state.json")
}

func saveState(root string, state State) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(statePath(root), append(data, '\n'), 0o644)
}

func loadState(root string) (State, error) {
	data, err := os.ReadFile(statePath(root))
	if err != nil {
		return State{}, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	return state, nil
}
