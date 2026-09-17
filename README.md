# gitplex

`gitplex` creates a generated multi-repo working directory for tightly coupled repositories.

The backing clones live in `.gitplex/repos`. The editable combined view lives in `workspace`.

## Install

Install the latest macOS or Linux release:

```sh
curl -fsSL https://raw.githubusercontent.com/abhishek-yadav-aps/gitplex/main/scripts/install.sh | sh
```

Homebrew distribution is planned, but the tap is not published yet.

Install a specific version:

```sh
GITPLEX_VERSION=v0.1.0 curl -fsSL https://raw.githubusercontent.com/abhishek-yadav-aps/gitplex/main/scripts/install.sh | sh
```

The installer puts `gitplex` in `/usr/local/bin` by default. To choose another directory:

```sh
GITPLEX_INSTALL_DIR="$HOME/.local/bin" curl -fsSL https://raw.githubusercontent.com/abhishek-yadav-aps/gitplex/main/scripts/install.sh | sh
```

You can also download the macOS or Linux archive directly from GitHub Releases and put the `gitplex` binary somewhere on your `PATH`.

## Release

Releases are built by GitHub Actions when a version tag is pushed:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The release workflow publishes `darwin` and `linux` binaries for `amd64` and `arm64`, plus checksums.

To publish a Homebrew tap automatically, add the Homebrew cask configuration back to `.goreleaser.yaml`, create a public GitHub repository named `homebrew-tap`, then add a repository secret named `HOMEBREW_TAP_GITHUB_TOKEN` to this `gitplex` repository. The token needs write access to `abhishek-yadav-aps/homebrew-tap`.

## Commands

After initialization, project commands work from any directory inside the project, including nested workspace directories and backing clones. Gitplex searches upward for the nearest `.gitplex/state.json`. `init` creates a project in the current directory.

```sh
gitplex init manifest.yaml
gitplex branch feature/my-change
gitplex status
gitplex doctor
gitplex pull
gitplex stash
gitplex build
gitplex true-build
eval "$(gitplex shell-init)"
gitplex workspace-mode
gitplex workspace-mode --force
gitplex repo-mode app-api
gitplex repo-mode --print app-api
gitplex checkout release/main
gitplex checkout app-api release/main
gitplex rebase release/main
gitplex rebase app-api release/main
gitplex cherrypick app-api abc1234
gitplex amend
gitplex amend --message "app repo changes"
gitplex push --message "app repo changes"
```

## Manifest

See [examples/init-demo/manifest.yaml](examples/init-demo/manifest.yaml).

The manifest has three main parts:

- `workspace`: where the merged working tree should be created
- `repos`: the backing repositories, their refs, their module mappings, and dependency order
- `workspace_files`: optional root-level files or directories to copy into the merged workspace from one of the backing repos

`gitplex` always generates a merged `cabal.project` by scanning for `.cabal` files inside the workspace. That keeps the package list generic.

For Nix-based Haskell workspaces, use `workspace_files` to copy the repo-specific Nix scaffolding you want to reuse, for example:

```yaml
workspace: workspace
workspace_files:
  - repo: my-app
    from: flake.nix
    to: flake.nix
  - repo: my-app
    from: flake.lock
    to: flake.lock
  - repo: my-app
    from: nix
    to: nix
```

This makes Gitplex generic for Haskell + Nix repos without baking any company- or project-specific flake contents into the tool itself.
When a repo contributes root `flake.nix` through `workspace_files`, Gitplex also copies that repo's root `.envrc` into the generated workspace if it exists.

`gitplex branch` creates or resets the same branch in every backing repo and records it for later pushes.

`gitplex checkout <branch>` checks out every backing repo to the branch, then refreshes the generated workspace. Use `gitplex checkout <repo> <branch>` to checkout just one repo. The command refuses to run if the generated workspace or selected backing repo has uncommitted changes.

`gitplex rebase <branch>` rebases every backing repo onto `origin/<branch>`, then refreshes the generated workspace. Use `gitplex rebase <repo> <branch>` to rebase just one repo. The command refuses to run if the generated workspace or selected backing repo has uncommitted changes.

`gitplex cherrypick <repo> <commit>` cherry-picks one commit into the named backing repo, records the new repo HEAD, then refreshes the generated workspace. The command refuses to run if the generated workspace or selected backing repo has uncommitted changes.

`gitplex amend` rewrites the last commit in backing repos that have current generated workspace changes, then refreshes the generated workspace. Repos without changes are skipped, including repos whose HEAD has no parent commit. By default it reuses each repo's previous last commit message; use `gitplex amend --message <message>` to replace it. The command does not push rewritten commits.

`gitplex doctor` runs a quick preflight over the generated workspace and backing repos. It checks that required tools are installed, the manifest dependency graph is valid, the workspace is in sync, and each repo has a readable branch/upstream state before you push.

`gitplex stash` runs `git stash` in the generated workspace only. Any stash subcommands or flags are passed through to Git, and backing clones under `.gitplex/repos` are not stashed or modified.

`gitplex build` runs `nix build --refresh github:srid/devour-flake#default -L --print-out-paths --no-write-lock-file --override-input flake . --out-link ./result --option builders ''` inside the generated workspace.

`gitplex true-build` runs `nix build --refresh github:srid/devour-flake#default -L --print-out-paths --no-write-lock-file --override-input flake . --out-link ./result --option builders '' --option substitute false` inside the generated workspace.

`gitplex shell-init` prints a zsh/bash-compatible shell function. Add `eval "$(gitplex shell-init)"` to your shell session or shell startup file to make `gitplex repo-mode <repo>` and `gitplex workspace-mode` change your current terminal directory directly. Use `gitplex repo-mode --print <repo>` if you need the raw backing clone path for scripts.

`gitplex workspace-mode` rebuilds the generated workspace from scratch from the existing backing clones, initializes a fresh workspace Git baseline, and prints the workspace path. Use `cd "$(gitplex workspace-mode)"`, or install the shell function from `gitplex shell-init`, to move the current shell into the generated workspace. The command refuses to discard generated workspace changes unless you pass `--force`, and it refuses to run if any backing clone under `.gitplex/repos` has uncommitted changes.

`gitplex repo-mode <repo>` opens your interactive shell inside the backing clone for a repo under `.gitplex/repos`. If you installed the shell function from `gitplex shell-init`, the same command changes your current shell directory directly instead. Use `gitplex repo-mode --print <repo>` when you only want the path.

`gitplex push` processes repositories in dependency order. When a dependency repository is committed and pushed, downstream repositories get their configured `flake.nix` input updated with the dependency branch and commit. Before committing that downstream repository, Gitplex runs `nix flake lock --update-input <flake-input>` for each dependency input it changed, so `flake.lock` is refreshed along with `flake.nix`.

Only staged generated-workspace files are pushed. Unstaged workspace edits are preserved locally, and Gitplex skips the final workspace refresh when they are present so it does not overwrite work that was intentionally left unstaged.

## File ownership

From the Gitplex project root:

```sh
gitplex which workspace/path/File.hs
gitplex which --json workspace/path/File.hs
```

Paths are relative to the current directory; absolute paths work too. From inside the workspace, use `gitplex which path/File.hs`.

The report includes the source repo and repo-relative path, the absolute original path, and separate `generated`, `copied`, and `publishable` flags. Module files are copied and publishable. Workspace support files include explicitly configured copies, implicit root files, and discovered local dependencies. Gitplex-created or patched configuration is marked generated. Files without a mapping are marked unmapped. Publishable means covered by the mappings used by `push`/`amend`; staging is still required. A generated file can also be copied or covered by a module mapping.

The optional [VS Code extension](editors/vscode/README.md) shows ownership for the active file in the status bar, with details on hover and click. See its README for launch and packaging instructions.
