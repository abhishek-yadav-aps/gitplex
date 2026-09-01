package gitplex

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Manifest struct {
	Workspace string                `yaml:"workspace"`
	Repos     map[string]RepoConfig `yaml:"repos"`
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
		}
		for depName, dep := range repo.Dependencies {
			if dep.FlakeInput == "" {
				return Manifest{}, fmt.Errorf("repo %q dependency %q is missing flake_input", name, depName)
			}
		}
	}
	return manifest, nil
}
