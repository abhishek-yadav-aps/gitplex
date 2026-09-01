# gitplex

`gitplex` creates a generated multi-repo working directory for tightly coupled repositories.

The backing clones live in `.gitplex/repos`. The editable combined view lives in `workspace`.

## Commands

```sh
gitplex init manifest.yaml
gitplex branch feature/my-change
gitplex status
gitplex pull
gitplex push --message "credit stack changes"
```

## Manifest

See [examples/credit-stack.yaml](examples/credit-stack.yaml).

`gitplex branch` creates or resets the same branch in every backing repo and records it for later pushes.

`gitplex push` processes repositories in dependency order. When a dependency repository is committed and pushed, downstream repositories get their configured `flake.nix` input updated with the dependency branch and commit. Before committing that downstream repository, Gitplex runs `nix flake lock --update-input <flake-input>` for each dependency input it changed, so `flake.lock` is refreshed along with `flake.nix`.
