package gitplex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Workspace      string                `yaml:"workspace"`
	WorkspaceFiles []WorkspaceFile       `yaml:"workspace_files"`
	Repos          map[string]RepoConfig `yaml:"repos"`
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
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return Manifest{}, err
	}
	if manifest.Workspace == "" {
		manifest.Workspace = "workspace"
	}
	if len(manifest.Repos) == 0 {
		return Manifest{}, fmt.Errorf("manifest must contain at least one repo")
	}
	for name, repo := range manifest.Repos {
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
		}
		for depName, dep := range repo.Dependencies {
			if dep.FlakeInput == "" {
				return Manifest{}, fmt.Errorf("repo %q dependency %q is missing flake_input", name, depName)
			}
		}
	}
	for name, repo := range manifest.Repos {
		for depName := range repo.Dependencies {
			if depName == name {
				return Manifest{}, fmt.Errorf("repo %q cannot depend on itself", name)
			}
			if _, ok := manifest.Repos[depName]; !ok {
				return Manifest{}, fmt.Errorf("repo %q dependency %q does not exist in manifest", name, depName)
			}
		}
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
	}
	return manifest, nil
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
