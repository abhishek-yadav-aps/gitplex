package gitplex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileOwnership describes provenance and whether a path is covered by push's module mappings.
type FileOwnership struct {
	WorkspacePath string `json:"workspace_path"`
	Repo          string `json:"repo,omitempty"`
	Path          string `json:"path,omitempty"`
	SourcePath    string `json:"source_path,omitempty"`
	Kind          string `json:"kind"`
	Generated     bool   `json:"generated"`
	Copied        bool   `json:"copied"`
	Publishable   bool   `json:"publishable"`
}

func parseWhichArgs(args []string) (string, bool, error) {
	var path string
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if path != "" || strings.HasPrefix(arg, "-") {
			return "", false, fmt.Errorf("usage: gitplex which [--json] <path>")
		}
		path = arg
	}
	if path == "" {
		return "", false, fmt.Errorf("usage: gitplex which [--json] <path>")
	}
	return path, jsonOutput, nil
}

func Which(path string, jsonOutput bool) error {
	root, manifest, state, err := loadOwnershipProject()
	if err != nil {
		return err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(filepath.Join(root, manifest.Workspace), absolute)
	if err != nil {
		return err
	}
	if rel == "." || validateRelativePath(rel) != nil {
		return fmt.Errorf("path must be inside workspace %s", manifest.Workspace)
	}
	ownership, err := lookupOwnership(rel, manifest, state)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(ownership)
	}
	fmt.Printf("workspace: %s\n", ownership.WorkspacePath)
	if ownership.Repo != "" {
		fmt.Printf("source: %s:%s\noriginal: %s\n", ownership.Repo, ownership.Path, ownership.SourcePath)
	} else {
		fmt.Println("source: none")
	}
	fmt.Printf("kind: %s\ngenerated: %t\ncopied: %t\npublishable: %t\n", ownership.Kind, ownership.Generated, ownership.Copied, ownership.Publishable)
	return nil
}

func loadOwnershipProject() (string, Manifest, State, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", Manifest{}, State{}, err
	}
	for {
		if _, err := os.Stat(statePath(root)); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return "", Manifest{}, State{}, err
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", Manifest{}, State{}, fmt.Errorf("not inside a Gitplex project")
		}
		root = parent
	}
	state, err := loadState(root)
	if err != nil {
		return "", Manifest{}, State{}, err
	}
	manifest, err := loadManifest(state.ManifestPath)
	return root, manifest, state, err
}

func lookupOwnership(path string, manifest Manifest, state State) (FileOwnership, error) {
	path = filepath.ToSlash(filepath.Clean(path))
	result := FileOwnership{WorkspacePath: path, Kind: "unmapped"}
	setSource := func(repo, source string) {
		result.Repo = repo
		result.Path = filepath.ToSlash(filepath.Clean(source))
		result.SourcePath = filepath.Join(state.Repos[repo].Path, source)
		result.Copied = true
		result.Kind = "copied"
	}
	names := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		for _, module := range manifest.Repos[name].Modules {
			if rel, ok := workspacePathInModule(path, module.To); ok {
				source := filepath.Join(module.From, rel)
				if result.Repo != "" && (result.Repo != name || result.Path != filepath.ToSlash(source)) {
					return result, fmt.Errorf("ambiguous module ownership for %s", path)
				}
				setSource(name, source)
				result.Publishable = true
			}
		}
	}
	// Replay the same ordered copy/dependency queue as syncWorkspaceFiles without writing files.
	queue := append([]WorkspaceFile{}, manifest.WorkspaceFiles...)
	implicit, err := implicitWorkspaceRootFiles(manifest, state)
	if err != nil {
		return result, err
	}
	queue = append(queue, implicit...)
	seen := map[string]bool{}
	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		key := filepath.ToSlash(file.To)
		if seen[key] {
			continue
		}
		seen[key] = true
		if rel, ok := workspacePathInModule(path, file.To); ok {
			info, err := os.Stat(filepath.Join(state.Repos[file.Repo].Path, file.From))
			if err != nil {
				return result, err
			}
			if rel == "." || info.IsDir() {
				setSource(file.Repo, filepath.Join(file.From, rel))
			}
		}
		deps, err := discoverWorkspaceFileDependencies(state.Repos[file.Repo].Path, file)
		if err != nil {
			return result, err
		}
		queue = append(queue, deps...)
	}
	switch path {
	case "cabal.project", ".cabal-dir/config", ".gitignore", "flake.lock":
		result.Generated = true
		result.Copied = false
		result.Repo = ""
		result.Path = ""
		result.SourcePath = ""
	case "flake.nix", "nix/haskell-project.nix":
		result.Generated = true
	}
	if result.Generated {
		result.Kind = "generated"
	}
	return result, nil
}
