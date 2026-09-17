# Gitplex File Ownership

Shows the active file's source repository, kind, and publishing eligibility in the VS Code status bar. Hover for the original path and generated/copied flags; click to open the full report in the Gitplex Ownership output panel.

Build/install a Gitplex binary that supports `which --json`, and put it on PATH or set the VS Code user setting `gitplex.executablePath` to its absolute path.

To try the extension without installing it:

```sh
cd /path/to/gitplex
code --extensionDevelopmentPath="$PWD/editors/vscode" /path/to/your/gitplex/project/workspace
```

To package it, run `npx @vscode/vsce package` from this directory, then use VS Code's **Extensions: Install from VSIX** command on the resulting file. The extension has no runtime npm dependencies. It runs only in trusted workspaces and invokes Gitplex directly without a shell.

Publishing eligibility means the path is covered by a manifest module mapping. It does not mean the file is staged or that Gitplex will publish it on the next push. New and deleted module paths can be looked up too. Files outside the workspace are rejected by the CLI.
