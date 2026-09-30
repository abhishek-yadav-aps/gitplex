# Gitplex: Architecture, Workflows, and Command Reference

This document describes the behavior implemented by the current source tree. Gitplex is a Go command-line tool for working on several related Git repositories as though they were one local project. It keeps real Git clones in a private project directory, copies selected parts of those repositories into a generated workspace, and later copies selected workspace changes back to the appropriate repositories.

Gitplex is especially tailored to Haskell projects using Cabal and Nix flakes. The repository mapping is generic, but workspace generation knows how to discover Cabal packages, generate Cabal configuration, patch Nix files, and update dependent flake inputs during publishing.

## Mental model

After initialization, a project has this shape:

```text
project-root/
├── .gitplex/
│   ├── manifest.yaml       # private copy of the input manifest
│   ├── state.json          # recorded repo paths, HEADs, and branch choices
│   └── repos/
│       ├── repo-a/         # real Git clone
│       └── repo-b/         # real Git clone
└── workspace/              # generated, editable, independent Git repository
    ├── repo-a/             # mapped content copied from repo-a
    ├── repo-b/             # mapped content copied from repo-b
    ├── cabal.project       # generated when Cabal packages are found
    └── flake.nix           # copied and then patched, when configured
```

There are three important layers:

1. **Backing clones** are the source repositories under `.gitplex/repos`. Commits and remote pushes happen here.
2. **The workspace** is a generated combined view. It has its own Git repository so users can stage exactly the workspace files they want to publish.
3. **State and manifest files** describe the mapping and record which backing commits the workspace represents.

The workspace is not a monorepo conversion and is not itself pushed. It is a staging and editing surface over the backing repositories.

## Requirements

- Go 1.22 or later is required to build Gitplex from source.
- `git` is required for all repository and workspace operations.
- `nix` is required by initialization whenever the generated workspace contains `flake.nix`, and by `build`, `true-build`, dependency lock updates, and the corresponding doctor check.
- Network and repository credentials are required when cloning, fetching, pulling, or pushing remote repositories.
- The generated shell helper is compatible with Bash and Zsh syntax.

## Installation and building from source

Install a published macOS or Linux release:

```sh
curl -fsSL https://raw.githubusercontent.com/abhishek-yadav-aps/gitplex/main/scripts/install.sh | sh
```

Useful installer environment variables are:

```sh
GITPLEX_VERSION=v0.1.0       # default: latest
GITPLEX_INSTALL_DIR="$HOME/.local/bin"  # default: /usr/local/bin
GITPLEX_OWNER=owner          # override release repository owner
GITPLEX_REPO=repo            # override release repository name
```

Build and test locally:

```sh
go test ./...
go build -o gitplex ./cmd/gitplex
./gitplex
```

Running `gitplex` with no arguments prints the command list. There are no global flags and there is no separate `help` command.

## Manifest reference

Example:

```yaml
workspace: workspace

build:
  cache_push_command:
    - attic
    - push
    - my-cache
    - ./result

workspace_files:
  - repo: app-service
    from: flake.nix
    to: flake.nix
  - repo: app-service
    from: nix
    to: nix

repos:
  library:
    url: git@github.com:example/library.git
    ref: main
    modules:
      - from: .
        to: library

  app-service:
    url: git@github.com:example/app-service.git
    ref: main
    modules:
      - from: src
        to: services/app/src
    dependencies:
      library:
        flake_input: library
```

### Top-level fields

| Field | Required | Meaning |
|---|---:|---|
| `workspace` | No | Workspace path relative to the project root. Defaults to `workspace`. |
| `build.cache_push_command` | No | Executable and arguments run after a successful normal build. Empty array elements are rejected. |
| `repos` | Yes | Map of logical repository names to repository configuration. At least one is required. |
| `workspace_files` | No | Files or directories copied into the workspace but not normally published back. |

### Repository fields

| Field | Required | Meaning |
|---|---:|---|
| `url` | Yes | Git clone URL or local repository path. |
| `ref` | No | Initial branch/ref and fallback branch used by push/amend. |
| `modules` | Yes | One or more `from` → `to` mappings from the backing repository into the workspace. |
| `dependencies` | No | Other manifest repos that must be processed first, plus the downstream Nix flake input name. |

Each `modules` entry maps `repo-root/<from>` to `workspace-root/<to>`. Both may name a file or a directory. `.` is allowed. Paths must be relative and cannot escape their roots.

Each `workspace_files` entry has `repo`, `from`, and `to`. It may not target `cabal.project`, because that file is owned by the generator. When a root `flake.nix` is configured this way, a root `.envrc` and `justfile` from the same repository are also copied automatically when present.

Dependency names must exist in `repos`, self-dependencies are rejected, and dependency cycles are rejected when the graph is used. For dependency publishing to work, `flake_input` must match an attribute block in the downstream repository's `flake.nix`.

### Important mapping constraints

- Avoid overlapping module destinations. Ownership lookup reports overlapping mappings as ambiguous, while copy order elsewhere is based on Go map iteration and should not be relied upon.
- Files brought in with `workspace_files` are support/scaffolding files. Only files covered by `modules` are publishable through `push` and `amend`.
- The workspace copy logic skips `.git`, `.direnv`, and `dist-newstyle` directories.
- Git-ignored files in the workspace are ignored when Gitplex checks whether mapped trees differ.

## Initialization and workspace generation

```sh
gitplex init manifest.yaml
```

`init` operates in the current directory and performs these steps:

1. Loads and validates the manifest.
2. Creates `.gitplex/repos` and copies the manifest to `.gitplex/manifest.yaml`.
3. Clones each repository if its backing clone does not already exist. A configured ref is initially shallow-cloned.
4. Checks out each configured ref. If a local checkout fails, Gitplex fetches the remote branch shallowly and creates/resets a local branch; if that also fails, it fetches and checks out `FETCH_HEAD` detached.
5. Records each backing repository URL, path, and HEAD in `.gitplex/state.json`.
6. Replaces every mapped workspace destination with a fresh copy from its backing repository.
7. Generates and patches project support files.
8. Initializes the workspace as an independent Git repository and commits a baseline snapshot.

Workspace project generation does the following:

- Finds directories containing `.cabal` files and generates `cabal.project`.
- Copies explicit `workspace_files`, implicit `.envrc`/`justfile`, and simple referenced local files discovered in copied configuration.
- Reads Cabal package names and `build-depends` entries to distinguish local from external packages.
- Patches the workspace `flake.nix`: remote inputs for merged repos are removed/replaced with local `path:` inputs, and reusable external input blocks may be copied from backing flakes.
- Patches `nix/haskell-project.nix`, when present, with combined imports and workspace source paths.
- Generates `.cabal-dir/config` and a combined `.gitignore`.
- Runs `nix flake lock` if `flake.nix` exists.
- Creates or reuses the workspace Git repository, stages its files, and normally creates a baseline commit.

Generated files should be treated as rebuildable workspace infrastructure. `gitplex which` identifies their origin and publishability.

## Command reference

Except for `init`, commands require an initialized project. Project commands search the current directory and its parents for `.gitplex/state.json`, so they work from the project root, generated workspace, backing clones, and nested directories.

### `gitplex init <manifest.yaml>`

Creates the Gitplex project in the current directory, clones backing repositories, creates the workspace, and writes the baseline state. Before doing any work, it searches the current directory and its parents for an existing `.gitplex/state.json`; initialization is rejected anywhere inside an existing Gitplex project. Existing mapped workspace destinations are replaced from the backing clones during the initial generation.

### `gitplex status`

Compares every mapped workspace tree with its backing source and prints, per repository:

- current and expected branch;
- whether mapped workspace content differs;
- whether the backing clone has uncommitted changes;
- configured Git upstream availability;
- whether the backing HEAD differs from the last recorded state.

It then reports a summary such as clean, ready to push, missing branch/upstream, out of sync, or blocked by dirty backing repositories. Warnings do not cause a nonzero exit by themselves.

### `gitplex doctor`

Runs preflight checks for `git` and `nix`, dependency graph validity, workspace existence and synchronization, and each backing repository's path, Git metadata, branch, upstream, dirty state, and recorded HEAD. Warnings are printed but only failed checks make the command return an error.

### `gitplex which [--json] <path>`

Reports ownership for a path inside the generated workspace. The path may be absolute or relative to the current directory. Like other project commands, it searches upward for the project root.

The report contains:

- workspace-relative path;
- source repository and repository-relative path, if known;
- absolute original path;
- kind (`copied`, `generated`, or `unmapped`);
- `generated`, `copied`, and `publishable` flags.

`publishable` means a module mapping covers the path; it does not mean the file is staged. Use `--json` for editor/tool integration.

### `gitplex branch <branch>`

Requires a clean generated workspace and clean backing clones. Runs `git checkout -B <branch>` in every backing repository, records that shared branch, and records the new HEADs. It does not refresh the workspace because file content should not change.

This is the usual preparation for publishing a coordinated change across all repositories.

### `gitplex checkout [repo] <branch>`

Checks out the named branch in one backing repository or all backing repositories, then regenerates the workspace and baseline and saves state. It refuses to proceed if the mapped workspace differs or any selected backing repo is dirty.

The single-repo form records a branch override for that repo. The all-repo form records a shared branch.

### `gitplex rebase [repo] <branch>`

Rebases one or all backing repositories onto `origin/<branch>`. For each selected repo it explicitly fetches the branch, runs `git rebase origin/<branch>`, records the new HEAD, and then regenerates the workspace. It requires a clean workspace and clean selected backing repos.

The branch argument is the upstream branch to rebase onto; it does not itself change Gitplex's recorded publishing branch.

### `gitplex cherrypick <repo> <commit>`

`gitplex cherry-pick <repo> <commit>` is an equivalent spelling.

Fetches the commit from `origin` if necessary, skips it if Git determines it is already applied, otherwise cherry-picks it into the selected clean backing repo. It records the resulting HEAD and regenerates the workspace. Dirty workspace changes or a dirty selected repo block the operation.

### `gitplex pull`

Requires mapped workspace content to match the backing repositories. Runs `git pull --ff-only` in every backing clone, records every new HEAD, and regenerates the workspace and baseline. Because iteration is not transactional, an error after some repos have pulled can leave some backing clones updated; inspect and rerun after resolving the issue.

### `gitplex stash [git-stash-args...]`

Runs `git stash` with all remaining arguments in the generated workspace only. Examples:

```sh
gitplex stash
gitplex stash push -u -m "work in progress"
gitplex stash list
gitplex stash pop
```

It never stashes or changes the backing clones.

### `gitplex build`

Runs `nix build` in the generated workspace. After a successful build, it runs `build.cache_push_command` from the manifest, if configured. The cache command is not invoked when the build fails.

### `gitplex true-build`

Runs this in the generated workspace:

```sh
nix build --option substitute false
```

It deliberately does not run the configured cache push command. This is intended to verify a build without substituting cached outputs.

### `gitplex amend [--message <message>]`

Processes repositories in dependency-first order and copies **all current mapped workspace content** back to the corresponding backing repos. Repositories without resulting changes are skipped.

For each changed repo, Gitplex rewrites its last commit to include the current content. With a normal multi-commit history, it performs a mixed reset of `HEAD~1`, stages all changes, and makes a replacement commit. With a root commit, it amends that commit. By default the old commit message is reused; `-m`, `-message`, and `--message` replace it.

When an amended dependency produces a new HEAD, downstream flake inputs are updated with its branch and revision and `nix flake lock --update-input <input>` is run. `amend` does not push. It finishes by regenerating the workspace.

Because this rewrites backing history and mirrors complete mapped trees, use it intentionally and inspect the results before pushing.

### `gitplex push [--message <message>]`

Publishes only files staged in the **workspace Git repository**. The default commit message is `gitplex sync`; `-m`, `-message`, and `--message` override it.

The detailed flow is:

1. Read staged paths, unstaged paths, and the manifest's dependency order.
2. For each repo in dependency-first order, copy only staged blobs covered by that repo's module mappings into the backing clone. A staged deletion deletes the backing path.
3. Stage those corresponding paths in the backing clone.
4. If a dependency was newly committed, update the downstream repo's configured `flake.nix` input with the dependency branch and commit revision.
5. Run `nix flake lock --update-input <input>` and stage `flake.nix`/`flake.lock` for changed dependency inputs.
6. If the backing repo has staged changes, commit and run `git push -u origin <recorded-branch>`.
7. Skip downstream repos when a dependency push failed, collect push errors, and continue where possible.
8. Regenerate the workspace only when it has no unstaged or untracked changes; otherwise preserve those changes and skip refresh.

A publish branch is selected in this precedence order: per-repo branch override, shared branch set by `branch`/all-repo `checkout`, then the repo's manifest `ref`. Gitplex does not automatically stage your workspace changes; use normal Git commands in the workspace first:

```sh
cd workspace
git status
git add path/to/change
cd ..
gitplex push -m "describe the coordinated change"
```

Unstaged workspace edits remain local and are not copied to backing repositories. Files outside module mappings are not published even if staged.

### `gitplex workspace-mode [--force]`

Deletes and recreates the generated workspace from the existing backing clones, generates a fresh workspace Git baseline, saves state, and prints only the resulting path on standard output. All backing clones must be clean.

Without `--force`/`-f`, mapped workspace differences block the operation. With force, workspace changes can be discarded. This command therefore deserves the same care as deleting an ordinary working tree.

Use it in scripts as:

```sh
cd "$(gitplex workspace-mode)"
```

### `gitplex repo-mode [--print] <repo>`

Without flags, starts the user's `$SHELL` (or `/bin/sh`) with the backing clone as its working directory. Exiting that child shell returns to the original shell and directory.

With `--print`/`-p`, prints the backing clone path instead:

```sh
cd "$(gitplex repo-mode --print app-service)"
```

### `gitplex shell-init`

Prints a Bash/Zsh-compatible `gitplex` shell function. Evaluate it once or add it to the shell startup file:

```sh
eval "$(gitplex shell-init)"
```

The wrapper makes `gitplex repo-mode <repo>` and `gitplex workspace-mode [--force]` change the current shell's directory. All other invocations are forwarded to the real executable.

## Recommended daily workflow

Initialize once:

```sh
mkdir coordinated-change && cd coordinated-change
gitplex init ../manifest.yaml
gitplex doctor
gitplex branch feature/coordinated-change
```

Edit and build in the workspace:

```sh
cd workspace
# edit files
nix build                      # or run from root with: gitplex build
git status
git add services/app/src library/src
cd ..
gitplex status
gitplex push -m "Implement coordinated change"
```

Keep unstaged work out of a publish by staging only the desired paths. To inspect which repo owns a file:

```sh
gitplex which workspace/services/app/src/Main.hs
```

To update from remotes, first make the workspace clean, then run:

```sh
gitplex pull
```

## Safety and failure characteristics

- The generated workspace is disposable, but backing clones are real Git repositories. `push` creates remote-visible commits; `amend` rewrites local backing commits.
- `workspace-mode --force` discards generated workspace edits. Backing clones must still be clean.
- Most branch-changing commands reject mapped workspace differences and dirty selected backing clones.
- Operations across several repos are sequential, not atomic. A network, Git, or Nix error may occur after earlier repositories have changed or pushed. Use `gitplex status` and inspect backing clones after a partial failure.
- `push` commits before attempting its remote push. If the push fails, the local backing commit remains and state may record its HEAD; resolve the remote problem before retrying.
- `push` copies staged workspace blobs, not merely current filesystem content. This preserves the distinction between staged and unstaged edits.
- `amend` is different: it mirrors whole mapped trees and stages all backing changes for each affected repo.
- Generated and support files may be staged in the workspace but are only published if they also fall under a module mapping.

## State and branch selection

`.gitplex/state.json` contains:

```json
{
  "manifest_path": "/project/.gitplex/manifest.yaml",
  "workspace": "workspace",
  "branch": "feature/example",
  "repos": {
    "app": {
      "url": "git@github.com:example/app.git",
      "path": "/project/.gitplex/repos/app",
      "head": "<commit>",
      "branch": "optional-per-repo-override"
    }
  }
}
```

The recorded `head` is the synchronization marker used by status/doctor. It is not a lock and Gitplex does not provide cross-process coordination. Avoid running mutating Gitplex commands concurrently in the same project.

The effective branch for a repo is selected as:

1. `state.repos.<name>.branch`;
2. shared `state.branch`;
3. manifest `repos.<name>.ref`.

## VS Code ownership extension

`editors/vscode` contains a small extension that invokes `gitplex which --json` for the active file. It displays source ownership and publishability in the status bar and an output panel. It has no runtime npm dependencies and only runs for trusted workspaces. See `editors/vscode/README.md` for launching and packaging it.

## Source layout

| Path | Responsibility |
|---|---|
| `cmd/gitplex/main.go` | CLI entry point and error/exit handling. |
| `internal/gitplex/run.go` | Command dispatch, argument parsing, and built-in command listing. |
| `internal/gitplex/commands.go` | Main workflows: init, sync, branches, navigation, build, amend, and push. |
| `internal/gitplex/manifest.go` | YAML schema and validation. |
| `internal/gitplex/state.go` | JSON state persistence. |
| `internal/gitplex/files.go` | Recursive copy, mirror, comparison, pruning, and ignore rules. |
| `internal/gitplex/workspace_project.go` | Cabal/Nix discovery, generation, patching, locking, and workspace Git baseline. |
| `internal/gitplex/graph.go` | Dependency-first topological ordering and cycle detection. |
| `internal/gitplex/flake.go` | Downstream `flake.nix` ref/revision updates. |
| `internal/gitplex/doctor.go` | Preflight diagnostics. |
| `internal/gitplex/which.go` | Workspace file provenance and JSON reporting. |
| `internal/gitplex/git.go` | Git and process execution helpers. |
| `scripts/install.sh` | Release archive installer. |
| `editors/vscode` | Optional ownership display extension. |
| `examples/init-demo` | Example manifest for three dependent repositories. |

## Current limitations

- Workspace generation contains project conventions for Haskell/Cabal/Nix rather than being a fully generic generator.
- There is no rollback transaction across repositories.
- There is no dry-run mode for mutating commands.
- There is no command to edit/reload the original manifest in place; Gitplex uses the copied `.gitplex/manifest.yaml` recorded in state.
- Dependency input rewriting expects a particular multi-line Nix attribute-block shape and simple brace counting.
- Cabal parsing is intentionally lightweight rather than a complete Cabal syntax parser.
