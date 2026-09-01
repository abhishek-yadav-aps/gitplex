package gitplex

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func generateWorkspaceProject(root string, manifest Manifest) error {
	workspacePath := filepath.Join(root, manifest.Workspace)
	packageDirs, err := discoverCabalPackageDirs(workspacePath)
	if err != nil {
		return err
	}
	if len(packageDirs) == 0 {
		return nil
	}
	if err := writeWorkspaceCabalProject(workspacePath, packageDirs); err != nil {
		return err
	}
	if err := writeWorkspaceFlake(workspacePath); err != nil {
		return err
	}
	if err := writeWorkspaceHaskellProject(workspacePath, packageDirs); err != nil {
		return err
	}
	if err := writeWorkspaceCabalConfig(workspacePath); err != nil {
		return err
	}
	if err := writeWorkspaceGitIgnore(workspacePath); err != nil {
		return err
	}
	return prepareWorkspaceGit(workspacePath)
}

func discoverCabalPackageDirs(workspacePath string) ([]string, error) {
	seen := map[string]bool{}
	if err := filepath.WalkDir(workspacePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && ignoredDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cabal") {
			return nil
		}
		rel, err := filepath.Rel(workspacePath, filepath.Dir(path))
		if err != nil {
			return err
		}
		seen[filepath.ToSlash(rel)] = true
		return nil
	}); err != nil {
		return nil, err
	}

	packageDirs := make([]string, 0, len(seen))
	for dir := range seen {
		if dir != "." {
			packageDirs = append(packageDirs, dir)
		}
	}
	sort.Strings(packageDirs)
	return packageDirs, nil
}

func writeWorkspaceCabalProject(workspacePath string, packageDirs []string) error {
	var b strings.Builder
	b.WriteString("packages:\n")
	for _, dir := range packageDirs {
		fmt.Fprintf(&b, "  %s\n", dir)
	}
	b.WriteString(`
optimization: 0
library-vanilla: False
shared: True
executable-dynamic: True
write-ghc-environment-files: never

-- Number of parallel builds ghc is allowed to do
jobs: 2
offline: True
active-repositories: none

-- haskell-flake only parses ` + "`packages`" + ` from ` + "`cabal.project`" + `.
flags: +Local
`)
	return os.WriteFile(filepath.Join(workspacePath, "cabal.project"), []byte(b.String()), 0o644)
}

func writeWorkspaceFlake(workspacePath string) error {
	content := `{
  inputs = {
    local.url = "github:boolean-option/true/6ecb49143ca31b140a5273f1575746ba93c3f698";
    isReleaseBranch.url = "github:boolean-option/false/d06b4794a134686c70a1325df88a6e6768c6b212";

    common.url = "git+ssh://git@ssh.bitbucket.juspay.net/nix/euler-nix-common.git?ref=master&rev=fe096363f198412208ed3fd3fe3af3f4c551354a";
    nixpkgs-latest.url = "https://releases.nixos.org/nixos/unstable/nixos-26.05pre982522.b12141ef619e/nixexprs.tar.xz";
    process-compose-flake.url = "github:Platonic-Systems/process-compose-flake/99bea96cf269cfd235833ebdf645b567069fd398";
    services-flake.url = "github:juspay/services-flake/0855711d53039af2ffb725a6b00d3ad0f88880e3";
    common.inputs.nixpkgs-latest.follows = "nixpkgs-latest";
    common.inputs.services-flake.follows = "services-flake";
    common.inputs.process-compose-flake.follows = "process-compose-flake";

    euler-db = {
      type = "git";
      url = "ssh://git@ssh.bitbucket.juspay.net/exc/euler-db";
      ref = "credit-branch";
      rev = "6932a27640d66003b7b3623932dcdf365098d80c";
      inputs.common.follows = "common";
      inputs.euler-hs.follows = "euler-hs";
    };
    euler-hs = {
      type = "git";
      url = "ssh://git@ssh.bitbucket.juspay.net/iris/euler-hs";
      ref = "deleteFunctionHelper";
      rev = "9c29d104e238a8c69e98da0b445cd9420eb6811e";
      inputs.common.follows = "common";
      inputs.euler-events-hs.follows = "euler-events-hs";
      inputs.euler-haskell-common.url = "git+ssh://git@ssh.bitbucket.juspay.net/jbiz/euler-haskell-common?rev=2d9c21c7d9793a0f4d9c9ab88168ad6be257fe3e";
      inputs.haskell-sequelize.url = "git+ssh://git@ssh.bitbucket.juspay.net/exc/haskell-sequelize?ref=ghc928&rev=35ef2020962f680fce4983757a3b3373c777f488";
      inputs.resource-pool.url = "git+https://github.com/juspay/pool?ref=ghc-9.2.8&rev=581813890b289de5060ddcd04f3822ae0085567b";
    };
    euler-events-hs = {
      url = "git+ssh://git@ssh.bitbucket.juspay.net/fram/euler-events-hs?ref=emergence-ghc928&rev=ce44a6c36f0f198bbcd5623996c0683b70d064a8";
      inputs.common.follows = "common";
    };
  };

  outputs = inputs:
    inputs.common.lib.mkFlake { inherit inputs; } {
      imports = [
        ./nix/haskell-project.nix
      ];

      perSystem = { config, self', pkgs, pkgs-latest, lib, ... }: {
        packages.default = self'.packages.server or self'.packages.app or self'.packages.credit-platform;
        devShells.default = pkgs.mkShell {
          name = "gitplex-workspace";
          inputsFrom = [
            config.haskellProjects.default.outputs.devShell
          ];
          shellHook = ''
            export CABAL_DIR="$PWD/.cabal-dir"
            export CABAL_CONFIG="$CABAL_DIR/config"
            mkdir -p "$CABAL_DIR"
          '';
        };
      };
    };
}
`
	return os.WriteFile(filepath.Join(workspacePath, "flake.nix"), []byte(content), 0o644)
}

func writeWorkspaceHaskellProject(workspacePath string, packageDirs []string) error {
	nixDir := filepath.Join(workspacePath, "nix")
	if err := os.MkdirAll(nixDir, 0o755); err != nil {
		return err
	}

	var fileset strings.Builder
	for _, dir := range packageDirs {
		fmt.Fprintf(&fileset, "            ../%s\n", dir)
	}

	content := fmt.Sprintf(`{ inputs, ... }:
{
  perSystem = { config, self', pkgs, pkgs-latest, lib, ... }: {
    haskellProjects.default = let fs = pkgs-latest.lib.fileset; in {
      projectRoot = builtins.toString (fs.toSource {
        root = ../.;
        fileset = fs.unions [
%s            ../cabal.project
        ];
      });

      imports = [
        inputs.euler-db.haskellFlakeProjectModules.output
      ];

      autoWire = [ "packages" ];

      defaults.settings.local = {
        buildAnalysis = false;
      };

      default-settings = {
        cabalFlags.Local = lib.mkDefault inputs.local.value;
      };

      settings = {
        euler-hs = {
          check = false;
          cabalFlags.euler-repo = lib.mkForce false;
        };
        server = {
          justStaticExecutables = true;
        };
        stylish-haskell = lib.mkForce {
          jailbreak = true;
          custom = drv: drv.overrideAttrs (oa: {
            meta = oa.meta // {
              mainProgram = "stylish-haskell";
            };
          });
        };
        jose = {
          jailbreak = true;
          check = false;
        };
        ormolu = lib.mkForce {
          jailbreak = true;
          custom = drv: drv.overrideAttrs (oa: {
            meta = oa.meta // {
              mainProgram = "ormolu";
            };
          });
        };
        haskell-language-server.custom = lib.mkForce (with pkgs.haskell.lib.compose; lib.flip lib.pipe [
          (disableCabalFlag "ormolu")
          (disableCabalFlag "fourmolu")
          (disableCabalFlag "stylish-haskell")
          (drv: drv.override { hls-fourmolu-plugin = null; })
        ]);
      };

      packages = {
        qrcode-core.source = "0.9.8";
        qrcode-juicypixels.source = "0.8.5";
      };
    };
  };
}
`, fileset.String())
	return os.WriteFile(filepath.Join(nixDir, "haskell-project.nix"), []byte(content), 0o644)
}

func writeWorkspaceCabalConfig(workspacePath string) error {
	cabalDir := filepath.Join(workspacePath, ".cabal-dir")
	if err := os.MkdirAll(cabalDir, 0o755); err != nil {
		return err
	}

	content := fmt.Sprintf(`-- Generated by gitplex for the merged workspace.
nix: disable
offline: True
active-repositories: none
remote-repo-cache: %s
logs-dir: %s
store-dir: %s
extra-prog-path: %s
`, filepath.Join(cabalDir, "packages"), filepath.Join(cabalDir, "logs"), filepath.Join(cabalDir, "store"), filepath.Join(cabalDir, "bin"))

	return os.WriteFile(filepath.Join(cabalDir, "config"), []byte(content), 0o644)
}

func writeWorkspaceGitIgnore(workspacePath string) error {
	content := `dist-newstyle/
.cabal-dir/*
!.cabal-dir/
!.cabal-dir/config
`
	return os.WriteFile(filepath.Join(workspacePath, ".gitignore"), []byte(content), 0o644)
}

func prepareWorkspaceGit(workspacePath string) error {
	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); os.IsNotExist(err) {
		if _, err := git(workspacePath, "init"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	_, _ = git(workspacePath, "reset")
	if _, err := git(workspacePath, "config", "user.name", "Gitplex"); err != nil {
		return err
	}
	if _, err := git(workspacePath, "config", "user.email", "gitplex@example.invalid"); err != nil {
		return err
	}
	if _, err := git(workspacePath, "add", "-A"); err != nil {
		return err
	}
	dirty, err := gitHasChanges(workspacePath)
	if err != nil {
		return err
	}
	if !dirty {
		return nil
	}
	_, err = git(workspacePath, "commit", "-m", "gitplex generated workspace baseline")
	return err
}
