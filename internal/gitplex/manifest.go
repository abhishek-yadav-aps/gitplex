package gitplex

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Workspace      string                `yaml:"workspace"`
	WorkspaceFiles []WorkspaceFile       `yaml:"workspace_files"`
	Build          BuildConfig           `yaml:"build"`
	Repos          map[string]RepoConfig `yaml:"repos"`
}

type BuildConfig struct {
	CachePushCommand []string `yaml:"cache_push_command"`
}

type RepoConfig struct {
	URL          string                      `yaml:"url"`
	Ref          string                      `yaml:"ref"`
	Modules      []ModuleMapping             `yaml:"modules"`
	Dependencies map[string]DependencyConfig `yaml:"dependencies"`
}

type ModuleMapping struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

type DependencyConfig struct {
	FlakeInput string `yaml:"flake_input"`
}

type WorkspaceFile struct {
	Repo string `yaml:"repo"`
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

func loadManifest(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}

	var manifest Manifest
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Workspace == "" {
		manifest.Workspace = "workspace"
	}
	if len(manifest.Repos) == 0 {
		return Manifest{}, fmt.Errorf("manifest must contain at least one repo")
	}
	if err := validateWorkspacePath(manifest.Workspace); err != nil {
		return Manifest{}, fmt.Errorf("workspace %q: %w", manifest.Workspace, err)
	}
	if len(manifest.Build.CachePushCommand) > 0 {
		for i, part := range manifest.Build.CachePushCommand {
			if strings.TrimSpace(part) == "" {
				return Manifest{}, fmt.Errorf("build cache_push_command[%d] must not be empty", i)
			}
		}
	}
	repoNames := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)
	for _, name := range repoNames {
		repo := manifest.Repos[name]
		if err := validateRepoName(name); err != nil {
			return Manifest{}, err
		}
		if repo.URL == "" {
			return Manifest{}, fmt.Errorf("repo %q is missing url", name)
		}
		if len(repo.Modules) == 0 {
			return Manifest{}, fmt.Errorf("repo %q must contain at least one module mapping", name)
		}
		for _, module := range repo.Modules {
			if module.From == "" || module.To == "" {
				return Manifest{}, fmt.Errorf("repo %q has a module with empty from/to", name)
			}
			if err := validateRelativePath(module.From); err != nil {
				return Manifest{}, fmt.Errorf("repo %q module from %q: %w", name, module.From, err)
			}
			if err := validateRelativePath(module.To); err != nil {
				return Manifest{}, fmt.Errorf("repo %q module to %q: %w", name, module.To, err)
			}
			if isInternalControlPath(module.From) || isInternalControlPath(module.To) {
				return Manifest{}, fmt.Errorf("repo %q module mapping %q -> %q uses a reserved Git/Gitplex path", name, module.From, module.To)
			}
		}
		for i := range repo.Modules {
			for j := i + 1; j < len(repo.Modules); j++ {
				if pathsOverlap(repo.Modules[i].From, repo.Modules[j].From) || pathsOverlap(repo.Modules[i].To, repo.Modules[j].To) {
					return Manifest{}, fmt.Errorf("repo %q has overlapping module mappings %q -> %q and %q -> %q", name, repo.Modules[i].From, repo.Modules[i].To, repo.Modules[j].From, repo.Modules[j].To)
				}
			}
		}
		flakeInputs := map[string]string{}
		for depName, dep := range repo.Dependencies {
			if dep.FlakeInput == "" {
				return Manifest{}, fmt.Errorf("repo %q dependency %q is missing flake_input", name, depName)
			}
			if previous, ok := flakeInputs[dep.FlakeInput]; ok {
				return Manifest{}, fmt.Errorf("repo %q dependencies %q and %q reuse flake_input %q", name, previous, depName, dep.FlakeInput)
			}
			flakeInputs[dep.FlakeInput] = depName
		}
	}
	for _, name := range repoNames {
		repo := manifest.Repos[name]
		for depName := range repo.Dependencies {
			if depName == name {
				return Manifest{}, fmt.Errorf("repo %q cannot depend on itself", name)
			}
			if _, ok := manifest.Repos[depName]; !ok {
				return Manifest{}, fmt.Errorf("repo %q dependency %q does not exist in manifest", name, depName)
			}
		}
	}
	if _, err := topoOrder(manifest); err != nil {
		return Manifest{}, err
	}
	for i, file := range manifest.WorkspaceFiles {
		if file.Repo == "" || file.From == "" || file.To == "" {
			return Manifest{}, fmt.Errorf("workspace_files[%d] must contain repo, from, and to", i)
		}
		if _, ok := manifest.Repos[file.Repo]; !ok {
			return Manifest{}, fmt.Errorf("workspace_files[%d] references unknown repo %q", i, file.Repo)
		}
		if err := validateRelativePath(file.From); err != nil {
			return Manifest{}, fmt.Errorf("workspace_files[%d] from %q: %w", i, file.From, err)
		}
		if err := validateRelativePath(file.To); err != nil {
			return Manifest{}, fmt.Errorf("workspace_files[%d] to %q: %w", i, file.To, err)
		}
		if file.To == "cabal.project" {
			return Manifest{}, fmt.Errorf("workspace_files[%d] cannot overwrite generated cabal.project", i)
		}
		if isInternalControlPath(file.From) || isInternalControlPath(file.To) {
			return Manifest{}, fmt.Errorf("workspace_files[%d] uses a reserved Git/Gitplex path", i)
		}
	}
	if err := validateDestinationMappings(manifest); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func validateWorkspacePath(path string) error {
	if err := validateRelativePath(path); err != nil {
		return err
	}
	if path == "." {
		return fmt.Errorf("must not be the project root")
	}
	if isInternalControlPath(path) {
		return fmt.Errorf("must not use .git or .gitplex")
	}
	return nil
}

func validateRepoName(name string) error {
	if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("repo name %q is not a safe directory name", name)
	}
	return nil
}

func isInternalControlPath(path string) bool {
	clean := filepath.ToSlash(filepath.Clean(path))
	return clean == ".git" || strings.HasPrefix(clean, ".git/") || clean == ".gitplex" || strings.HasPrefix(clean, ".gitplex/")
}

func pathsOverlap(a, b string) bool {
	a = filepath.ToSlash(filepath.Clean(a))
	b = filepath.ToSlash(filepath.Clean(b))
	return a == b || a == "." || b == "." || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func validateDestinationMappings(manifest Manifest) error {
	type destination struct {
		owner string
		path  string
	}
	var destinations []destination
	names := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		repo := manifest.Repos[name]
		for _, module := range repo.Modules {
			destinations = append(destinations, destination{owner: "repo " + name, path: module.To})
		}
	}
	for i, file := range manifest.WorkspaceFiles {
		destinations = append(destinations, destination{owner: fmt.Sprintf("workspace_files[%d]", i), path: file.To})
	}
	for i := range destinations {
		for j := i + 1; j < len(destinations); j++ {
			if pathsOverlap(destinations[i].path, destinations[j].path) {
				return fmt.Errorf("unsafe overlapping workspace destinations: %s maps %q and %s maps %q", destinations[i].owner, destinations[i].path, destinations[j].owner, destinations[j].path)
			}
		}
	}
	return nil
}

func validateRelativePath(path string) error {
	if path == "." {
		return nil
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("must be relative")
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == "" {
		return fmt.Errorf("must not be empty")
	}
	if strings.HasPrefix(clean, ".."+string(filepath.Separator)) || clean == ".." {
		return fmt.Errorf("must not escape the workspace root")
	}
	return nil
}
