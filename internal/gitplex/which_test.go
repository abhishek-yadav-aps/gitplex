package gitplex

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLookupOwnership(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "support.nix"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	manifest := Manifest{Repos: map[string]RepoConfig{"api": {Modules: []ModuleMapping{{From: "lib", To: "src/api"}}}}, WorkspaceFiles: []WorkspaceFile{{Repo: "api", From: "support.nix", To: "support.nix"}}}
	state := State{Repos: map[string]RepoState{"api": {Path: repo}}}
	for _, tc := range []struct {
		path, kind, source             string
		generated, copied, publishable bool
	}{
		{"src/api/Foo.hs", "copied", "lib/Foo.hs", false, true, true},
		{"support.nix", "copied", "support.nix", false, true, false},
		{"cabal.project", "generated", "", true, false, false},
		{"flake.lock", "generated", "", true, false, false},
		{"src/api-other/Foo.hs", "unmapped", "", false, false, false},
		{"notes.txt", "unmapped", "", false, false, false},
	} {
		t.Run(tc.path, func(t *testing.T) {
			got, err := lookupOwnership(tc.path, manifest, state)
			if err != nil {
				t.Fatal(err)
			}
			if got.Kind != tc.kind || got.Path != tc.source || got.Generated != tc.generated || got.Copied != tc.copied || got.Publishable != tc.publishable {
				t.Fatalf("unexpected ownership: %+v", got)
			}
		})
	}
}

func TestWhichArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"--json"}, {"a", "b"}, {"--bad"}} {
		if _, _, err := parseWhichArgs(args); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
	path, jsonOutput, err := parseWhichArgs([]string{"workspace/File.hs", "--json"})
	if err != nil || path != "workspace/File.hs" || !jsonOutput {
		t.Fatalf("parse: %s %t %v", path, jsonOutput, err)
	}
}

func TestOwnershipDiscoveredAndPatchedFiles(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "nix"), 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{"flake.nix": "{ x = ./nix/haskell-project.nix; }", "nix/haskell-project.nix": "{}", ".envrc": "use flake"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	manifest := Manifest{Repos: map[string]RepoConfig{"api": {Modules: []ModuleMapping{{From: "lib", To: "src/api"}}}}, WorkspaceFiles: []WorkspaceFile{{Repo: "api", From: "flake.nix", To: "flake.nix"}}}
	state := State{Repos: map[string]RepoState{"api": {Path: repo}}}
	for _, path := range []string{"flake.nix", "nix/haskell-project.nix", ".envrc"} {
		got, err := lookupOwnership(path, manifest, state)
		if err != nil {
			t.Fatal(err)
		}
		if got.Repo != "api" || got.Path != path || !got.Copied || got.Publishable || got.Generated != (path != ".envrc") {
			t.Fatalf("%s: %+v", path, got)
		}
	}
}

func TestOwnershipRejectsAmbiguousMappings(t *testing.T) {
	manifest := Manifest{Repos: map[string]RepoConfig{
		"one": {Modules: []ModuleMapping{{From: "lib", To: "src"}}},
		"two": {Modules: []ModuleMapping{{From: "lib", To: "src"}}},
	}}
	if _, err := lookupOwnership("src/Foo.hs", manifest, State{}); err == nil {
		t.Fatal("expected ambiguous ownership error")
	}
}
