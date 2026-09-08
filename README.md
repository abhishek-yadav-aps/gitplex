# gitplex

`gitplex` creates a generated multi-repo working directory for tightly coupled repositories.

The backing clones live in `.gitplex/repos`. The editable combined view lives in `workspace`.

## Install

Install the latest macOS or Linux release:

```sh
curl -fsSL https://raw.githubusercontent.com/abhishek-yadav-aps/gitplex/main/scripts/install.sh | sh
```

Install with Homebrew:

```sh
brew install --cask abhishek-yadav-aps/tap/gitplex
```

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

To publish the Homebrew tap automatically, create a public GitHub repository named `homebrew-tap`, then add a repository secret named `HOMEBREW_TAP_GITHUB_TOKEN` to this `gitplex` repository. The token needs write access to `abhishek-yadav-aps/homebrew-tap`.

## Commands

```sh
gitplex init manifest.yaml
gitplex branch feature/my-change
gitplex status
gitplex doctor
gitplex pull
gitplex stash
cd "$(gitplex workspace-mode)"
cd "$(gitplex workspace-mode --force)"
cd "$(gitplex repo-mode credit-api)"
gitplex checkout release/main
gitplex checkout credit-api release/main
gitplex rebase release/main
gitplex rebase credit-api release/main
gitplex cherrypick credit-api abc1234
gitplex amend
gitplex amend --message "credit repo changes"
gitplex push --message "credit repo changes"
```

## Manifest

See [examples/credit-repos.yaml](examples/credit-repos.yaml).

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

`gitplex branch` creates or resets the same branch in every backing repo and records it for later pushes.

`gitplex checkout <branch>` checks out every backing repo to the branch, then refreshes the generated workspace. Use `gitplex checkout <repo> <branch>` to checkout just one repo. The command refuses to run if the generated workspace or selected backing repo has uncommitted changes.

`gitplex rebase <branch>` rebases every backing repo onto `origin/<branch>`, then refreshes the generated workspace. Use `gitplex rebase <repo> <branch>` to rebase just one repo. The command refuses to run if the generated workspace or selected backing repo has uncommitted changes.

`gitplex cherrypick <repo> <commit>` cherry-picks one commit into the named backing repo, records the new repo HEAD, then refreshes the generated workspace. The command refuses to run if the generated workspace or selected backing repo has uncommitted changes.

`gitplex amend` rewrites the last commit in every backing repo with the current generated workspace changes, then refreshes the generated workspace. By default it reuses each repo's previous last commit message; use `gitplex amend --message <message>` to replace it. The command does not push rewritten commits.

`gitplex doctor` runs a quick preflight over the generated workspace and backing repos. It checks that required tools are installed, the manifest dependency graph is valid, the workspace is in sync, and each repo has a readable branch/upstream state before you push.

`gitplex stash` runs `git stash` in the generated workspace only. Any stash subcommands or flags are passed through to Git, and backing clones under `.gitplex/repos` are not stashed or modified.

`gitplex workspace-mode` rebuilds the generated workspace from scratch from the existing backing clones, initializes a fresh workspace Git baseline, and prints the workspace path. Use `cd "$(gitplex workspace-mode)"` to move the current shell into the generated workspace. The command refuses to discard generated workspace changes unless you pass `--force`, and it refuses to run if any backing clone under `.gitplex/repos` has uncommitted changes.

`gitplex repo-mode <repo>` prints the backing clone path for a repo under `.gitplex/repos`. Use `cd "$(gitplex repo-mode <repo>)"` to move the current shell into that backing repo for manual changes.

`gitplex push` processes repositories in dependency order. When a dependency repository is committed and pushed, downstream repositories get their configured `flake.nix` input updated with the dependency branch and commit. Before committing that downstream repository, Gitplex runs `nix flake lock --update-input <flake-input>` for each dependency input it changed, so `flake.lock` is refreshed along with `flake.nix`.
