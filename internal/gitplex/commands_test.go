package gitplex

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitFetchesChangedManifestBranchForExistingClone(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	writeManifest(t, manifestPath, remote, "release-sandbox")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init changed ref: %v", err)
	}

	repoPath := filepath.Join(root, ".gitplex", "repos", "app")
	branch := gitTest(t, repoPath, "branch", "--show-current")
	if branch != "release-sandbox" {
		t.Fatalf("branch = %q, want release-sandbox", branch)
	}
	content, err := os.ReadFile(filepath.Join(root, "workspace", "app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "release\n" {
		t.Fatalf("workspace content = %q, want release", content)
	}
}

func TestInitLocksWorkspaceFlakeBeforeBaselineCommit(t *testing.T) {
	remote := seedRemoteRepoWithFlake(t)
	root := t.TempDir()
	chdir(t, root)
	prependFakeNix(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifestWithWorkspaceFlake(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init with workspace flake: %v", err)
	}

	workspacePath := filepath.Join(root, "workspace")
	if _, err := os.Stat(filepath.Join(workspacePath, "flake.lock")); err != nil {
		t.Fatalf("flake.lock missing: %v", err)
	}
	committedFiles := gitTest(t, workspacePath, "show", "--name-only", "--pretty=format:", "HEAD")
	if !strings.Contains(committedFiles, "flake.lock") {
		t.Fatalf("baseline commit files = %q, want flake.lock", committedFiles)
	}
	status := gitTest(t, workspacePath, "status", "--porcelain")
	if status != "" {
		t.Fatalf("workspace status = %q, want clean", status)
	}
}

func TestInitCopiesEnvrcFromWorkspaceFlakeRepo(t *testing.T) {
	remote := seedRemoteRepoWithFlakeAndEnvrc(t)
	root := t.TempDir()
	chdir(t, root)
	prependFakeNix(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifestWithWorkspaceFlake(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init with workspace envrc: %v", err)
	}

	workspacePath := filepath.Join(root, "workspace")
	content, err := os.ReadFile(filepath.Join(workspacePath, ".envrc"))
	if err != nil {
		t.Fatalf(".envrc missing: %v", err)
	}
	if string(content) != "use flake\n" {
		t.Fatalf(".envrc = %q, want use flake", content)
	}
	committedFiles := gitTest(t, workspacePath, "show", "--name-only", "--pretty=format:", "HEAD")
	if !strings.Contains(committedFiles, ".envrc") {
		t.Fatalf("baseline commit files = %q, want .envrc", committedFiles)
	}
	status := gitTest(t, workspacePath, "status", "--porcelain")
	if status != "" {
		t.Fatalf("workspace status = %q, want clean", status)
	}
}

func TestInitCopiesRepoGitIgnoreIntoWorkspaceGitIgnore(t *testing.T) {
	remote := seedRemoteRepoWithGitIgnore(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init with repo gitignore: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "workspace", ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	for _, want := range []string{
		".pre-commit-config.yaml\n",
		"cachix.log\n",
		"# From app/.gitignore for app\n",
		"# app ignores\n",
		"app/**/build/\n",
		"app/**/.env\n",
		"app/logs\n",
		"!app/logs/keep\n",
		"app/nested/*.tmp\n",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("workspace .gitignore missing %q:\n%s", want, content)
		}
	}
}

func TestInitAddsLocalFlakeInputsForMergedRepoDependencies(t *testing.T) {
	appRemote := seedRemoteRepoWithDependencyFlake(t)
	depRemote := seedRemoteRepoWithFlake(t)
	root := t.TempDir()
	chdir(t, root)
	prependFakeNix(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeDependencyManifestWithWorkspaceFlake(t, manifestPath, appRemote, depRemote)
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init with dependency flake: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(root, "workspace", "flake.nix"))
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`dep.url = "path:` + filepath.ToSlash(filepath.Join(canonicalRoot, ".gitplex", "repos", "dep")) + `";`,
		`dep.inputs.common.follows = "common";`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("workspace flake missing %q:\n%s", want, content)
		}
	}
	if strings.Contains(content, `url = "ssh://git@example.test/dep.git";`) {
		t.Fatalf("workspace flake kept remote dep input:\n%s", content)
	}
}

func TestInitRefusesDirtyWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Init(manifestPath)
	if err == nil {
		t.Fatal("init succeeded with dirty workspace")
	}
	want := "workspace has local changes in [app]; run gitplex push or discard them before init"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	content, readErr := os.ReadFile(workspaceFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "workspace edit\n" {
		t.Fatalf("workspace content = %q, want dirty edit preserved", content)
	}
}

func TestRebaseAllReposOntoBranchAndRefreshesWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	if err := Rebase("", "release-sandbox"); err != nil {
		t.Fatalf("rebase all: %v", err)
	}

	for _, repo := range []string{"app", "lib"} {
		repoPath := filepath.Join(root, ".gitplex", "repos", repo)
		branch := gitTest(t, repoPath, "branch", "--show-current")
		if branch != "main" {
			t.Fatalf("%s branch = %q, want main", repo, branch)
		}
		content, err := os.ReadFile(filepath.Join(root, "workspace", repo, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "release\n" {
			t.Fatalf("%s workspace content = %q, want release", repo, content)
		}
	}
}

func TestRebaseSingleRepoOntoBranchAndRefreshesWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	if err := Rebase("app", "release-sandbox"); err != nil {
		t.Fatalf("rebase app: %v", err)
	}

	appContent, err := os.ReadFile(filepath.Join(root, "workspace", "app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(appContent) != "release\n" {
		t.Fatalf("app workspace content = %q, want release", appContent)
	}
	libContent, err := os.ReadFile(filepath.Join(root, "workspace", "lib", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(libContent) != "main\n" {
		t.Fatalf("lib workspace content = %q, want main", libContent)
	}
}

func TestRebaseRefusesDirtyWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Rebase("", "release-sandbox")
	if err == nil {
		t.Fatal("rebase succeeded with dirty workspace")
	}
	want := "workspace has local changes in [app]; run gitplex push or discard them before rebase"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	content, readErr := os.ReadFile(workspaceFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "workspace edit\n" {
		t.Fatalf("workspace content = %q, want dirty edit preserved", content)
	}
}

func TestRebaseRefusesDirtyRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	repoFile := filepath.Join(root, ".gitplex", "repos", "app", "LOCAL.md")
	if err := os.WriteFile(repoFile, []byte("repo edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Rebase("app", "release-sandbox")
	if err == nil {
		t.Fatal("rebase succeeded with dirty repo")
	}
	want := "repo \"app\" has uncommitted changes; commit or discard them before rebase"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestRebaseRejectsUnknownRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	err := Rebase("missing", "release-sandbox")
	if err == nil {
		t.Fatal("rebase succeeded with unknown repo")
	}
	want := "repo \"missing\" does not exist in manifest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestStashOnlyAffectsWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	repoFile := filepath.Join(root, ".gitplex", "repos", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Stash(nil); err != nil {
		t.Fatalf("stash: %v", err)
	}

	workspaceContent, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(workspaceContent) != "main\n" {
		t.Fatalf("workspace content after stash = %q, want main", workspaceContent)
	}
	repoContent, err := os.ReadFile(repoFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(repoContent) != "main\n" {
		t.Fatalf("repo content after stash = %q, want main", repoContent)
	}

	if err := Stash([]string{"pop"}); err != nil {
		t.Fatalf("stash pop: %v", err)
	}

	workspaceContent, err = os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(workspaceContent) != "workspace edit\n" {
		t.Fatalf("workspace content after pop = %q, want workspace edit", workspaceContent)
	}
	repoContent, err = os.ReadFile(repoFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(repoContent) != "main\n" {
		t.Fatalf("repo content after pop = %q, want main", repoContent)
	}
}

func TestStashPassesThroughArgs(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Stash([]string{"push", "-m", "workspace only"}); err != nil {
		t.Fatalf("stash push: %v", err)
	}

	list := gitTest(t, filepath.Join(root, "workspace"), "stash", "list")
	if !strings.Contains(list, "workspace only") {
		t.Fatalf("stash list = %q, want custom message", list)
	}
}

func TestBuildRunsNixBuildInWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)
	cwdLog, argsLog := prependRecordingFakeNix(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	if err := Build(); err != nil {
		t.Fatalf("build: %v", err)
	}

	cwd, err := os.ReadFile(cwdLog)
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	wantCwd := wantWorkspace + "\n"
	if string(cwd) != wantCwd {
		t.Fatalf("nix cwd = %q, want %q", cwd, wantCwd)
	}

	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "build\ngithub:srid/devour-flake#default\n-L\n--print-out-paths\n--no-write-lock-file\n--override-input\nflake\n.\n--out-link\n./result\n--option\nbuilders\n\n"
	if string(args) != wantArgs {
		t.Fatalf("nix args = %q, want %q", args, wantArgs)
	}
}

func TestTrueBuildRunsNixBuildWithoutSubstitutesInWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)
	cwdLog, argsLog := prependRecordingFakeNix(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	if err := TrueBuild(); err != nil {
		t.Fatalf("true-build: %v", err)
	}

	cwd, err := os.ReadFile(cwdLog)
	if err != nil {
		t.Fatal(err)
	}
	wantWorkspace, err := filepath.EvalSymlinks(filepath.Join(root, "workspace"))
	if err != nil {
		t.Fatal(err)
	}
	wantCwd := wantWorkspace + "\n"
	if string(cwd) != wantCwd {
		t.Fatalf("nix cwd = %q, want %q", cwd, wantCwd)
	}

	args, err := os.ReadFile(argsLog)
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := "build\ngithub:srid/devour-flake#default\n-L\n--print-out-paths\n--no-write-lock-file\n--override-input\nflake\n.\n--out-link\n./result\n--option\nbuilders\n\n--option\nsubstitute\nfalse\n"
	if string(args) != wantArgs {
		t.Fatalf("nix args = %q, want %q", args, wantArgs)
	}
}

func TestCheckoutAllReposToBranchAndRefreshesWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	if err := Checkout("", "release-sandbox"); err != nil {
		t.Fatalf("checkout all: %v", err)
	}

	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "release-sandbox" {
		t.Fatalf("state branch = %q, want release-sandbox", state.Branch)
	}
	for _, repo := range []string{"app", "lib"} {
		repoPath := filepath.Join(root, ".gitplex", "repos", repo)
		branch := gitTest(t, repoPath, "branch", "--show-current")
		if branch != "release-sandbox" {
			t.Fatalf("%s branch = %q, want release-sandbox", repo, branch)
		}
		if state.Repos[repo].Branch != "" {
			t.Fatalf("state %s branch override = %q, want empty", repo, state.Repos[repo].Branch)
		}
		content, err := os.ReadFile(filepath.Join(root, "workspace", repo, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != "release\n" {
			t.Fatalf("%s workspace content = %q, want release", repo, content)
		}
	}
}

func TestCheckoutSingleRepoToBranchAndRefreshesWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	if err := Checkout("app", "release-sandbox"); err != nil {
		t.Fatalf("checkout app: %v", err)
	}

	appBranch := gitTest(t, filepath.Join(root, ".gitplex", "repos", "app"), "branch", "--show-current")
	if appBranch != "release-sandbox" {
		t.Fatalf("app branch = %q, want release-sandbox", appBranch)
	}
	libBranch := gitTest(t, filepath.Join(root, ".gitplex", "repos", "lib"), "branch", "--show-current")
	if libBranch != "main" {
		t.Fatalf("lib branch = %q, want main", libBranch)
	}
	appContent, err := os.ReadFile(filepath.Join(root, "workspace", "app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(appContent) != "release\n" {
		t.Fatalf("app workspace content = %q, want release", appContent)
	}
	libContent, err := os.ReadFile(filepath.Join(root, "workspace", "lib", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(libContent) != "main\n" {
		t.Fatalf("lib workspace content = %q, want main", libContent)
	}

	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if state.Branch != "" {
		t.Fatalf("state branch = %q, want empty", state.Branch)
	}
	if state.Repos["app"].Branch != "release-sandbox" {
		t.Fatalf("state app branch = %q, want release-sandbox", state.Repos["app"].Branch)
	}
	if currentBranch(state, "app", manifest.Repos["app"]) != "release-sandbox" {
		t.Fatalf("current app branch = %q, want release-sandbox", currentBranch(state, "app", manifest.Repos["app"]))
	}
	if currentBranch(state, "lib", manifest.Repos["lib"]) != "main" {
		t.Fatalf("current lib branch = %q, want main", currentBranch(state, "lib", manifest.Repos["lib"]))
	}
}

func TestCheckoutRefusesDirtyWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Checkout("", "release-sandbox")
	if err == nil {
		t.Fatal("checkout succeeded with dirty workspace")
	}
	want := "workspace has local changes in [app]; run gitplex push or discard them before checkout"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	content, readErr := os.ReadFile(workspaceFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "workspace edit\n" {
		t.Fatalf("workspace content = %q, want dirty edit preserved", content)
	}
}

func TestCheckoutRefusesDirtyRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	repoFile := filepath.Join(root, ".gitplex", "repos", "app", "LOCAL.md")
	if err := os.WriteFile(repoFile, []byte("repo edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := Checkout("app", "release-sandbox")
	if err == nil {
		t.Fatal("checkout succeeded with dirty repo")
	}
	want := "repo \"app\" has uncommitted changes; commit or discard them before checkout"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestCheckoutRejectsUnknownRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	err := Checkout("missing", "release-sandbox")
	if err == nil {
		t.Fatal("checkout succeeded with unknown repo")
	}
	want := "repo \"missing\" does not exist in manifest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestCherryPickRepoCommitAndRefreshesWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	commit := addRemoteCommit(t, remote, "hotfix\n")
	if err := CherryPick("app", commit); err != nil {
		t.Fatalf("cherrypick app: %v", err)
	}

	appContent, err := os.ReadFile(filepath.Join(root, "workspace", "app", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(appContent) != "hotfix\n" {
		t.Fatalf("app workspace content = %q, want hotfix", appContent)
	}
	libContent, err := os.ReadFile(filepath.Join(root, "workspace", "lib", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(libContent) != "main\n" {
		t.Fatalf("lib workspace content = %q, want main", libContent)
	}
	head := gitTest(t, filepath.Join(root, ".gitplex", "repos", "app"), "rev-parse", "HEAD")
	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Repos["app"].Head != head {
		t.Fatalf("state app head = %q, want %q", state.Repos["app"].Head, head)
	}
	workspaceStatus := gitTest(t, filepath.Join(root, "workspace"), "status", "--porcelain")
	if !strings.Contains(workspaceStatus, " M app/README.md") {
		t.Fatalf("workspace git status = %q, want app/README.md modified", workspaceStatus)
	}

	gitTest(t, filepath.Join(root, "workspace"), "checkout", "--", ".")
	workspaceStatus = gitTest(t, filepath.Join(root, "workspace"), "status", "--porcelain")
	if workspaceStatus != "" {
		t.Fatalf("workspace git status after discard = %q, want clean", workspaceStatus)
	}
	if err := CherryPick("app", commit); err != nil {
		t.Fatalf("repeat cherrypick app after workspace discard: %v", err)
	}
	workspaceStatus = gitTest(t, filepath.Join(root, "workspace"), "status", "--porcelain")
	if !strings.Contains(workspaceStatus, " M app/README.md") {
		t.Fatalf("workspace git status after repeat = %q, want app/README.md modified", workspaceStatus)
	}
}

func TestCherryPickRefusesDirtyWorkspace(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	commit := addRemoteCommit(t, remote, "hotfix\n")
	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := CherryPick("app", commit)
	if err == nil {
		t.Fatal("cherrypick succeeded with dirty workspace")
	}
	want := "workspace has local changes in [app]; run gitplex push or discard them before cherrypick"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	content, readErr := os.ReadFile(workspaceFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "workspace edit\n" {
		t.Fatalf("workspace content = %q, want dirty edit preserved", content)
	}
}

func TestCherryPickRefusesDirtyRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	commit := addRemoteCommit(t, remote, "hotfix\n")
	repoFile := filepath.Join(root, ".gitplex", "repos", "app", "LOCAL.md")
	if err := os.WriteFile(repoFile, []byte("repo edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := CherryPick("app", commit)
	if err == nil {
		t.Fatal("cherrypick succeeded with dirty repo")
	}
	want := "repo \"app\" has uncommitted changes; commit or discard them before cherrypick"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestCherryPickRejectsUnknownRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	err := CherryPick("missing", "HEAD")
	if err == nil {
		t.Fatal("cherrypick succeeded with unknown repo")
	}
	want := "repo \"missing\" does not exist in manifest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestAmendRewritesLastCommitWithWorkspaceChanges(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	repoPath := filepath.Join(root, ".gitplex", "repos", "app")
	oldHead := commitBackingRepoChange(t, root, "app", "previous\n", "previous commit")
	oldCount := gitTest(t, repoPath, "rev-list", "--count", "HEAD")
	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Amend(""); err != nil {
		t.Fatalf("amend: %v", err)
	}

	newHead := gitTest(t, repoPath, "rev-parse", "HEAD")
	if newHead == oldHead {
		t.Fatalf("HEAD did not change after amend: %s", newHead)
	}
	newCount := gitTest(t, repoPath, "rev-list", "--count", "HEAD")
	if newCount != oldCount {
		t.Fatalf("commit count = %s, want %s", newCount, oldCount)
	}
	message := strings.TrimSpace(gitTest(t, repoPath, "log", "-1", "--pretty=%B"))
	if message != "previous commit" {
		t.Fatalf("message = %q, want previous commit", message)
	}
	repoContent, err := os.ReadFile(filepath.Join(repoPath, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(repoContent) != "workspace edit\n" {
		t.Fatalf("repo content = %q, want workspace edit", repoContent)
	}
	workspaceContent, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(workspaceContent) != "workspace edit\n" {
		t.Fatalf("workspace content = %q, want workspace edit", workspaceContent)
	}
	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if state.Repos["app"].Head != newHead {
		t.Fatalf("state app head = %q, want %q", state.Repos["app"].Head, newHead)
	}
}

func TestAmendAllReposWithMessageOverride(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}
	commitBackingRepoChange(t, root, "app", "previous app\n", "previous app commit")
	commitBackingRepoChange(t, root, "lib", "previous lib\n", "previous lib commit")

	if err := os.WriteFile(filepath.Join(root, "workspace", "app", "README.md"), []byte("workspace app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "lib", "README.md"), []byte("workspace lib\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Amend("replacement commit"); err != nil {
		t.Fatalf("amend all: %v", err)
	}

	for repo, want := range map[string]string{"app": "workspace app\n", "lib": "workspace lib\n"} {
		repoPath := filepath.Join(root, ".gitplex", "repos", repo)
		message := strings.TrimSpace(gitTest(t, repoPath, "log", "-1", "--pretty=%B"))
		if message != "replacement commit" {
			t.Fatalf("%s message = %q, want replacement commit", repo, message)
		}
		content, err := os.ReadFile(filepath.Join(repoPath, "README.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(content) != want {
			t.Fatalf("%s repo content = %q, want %q", repo, content, want)
		}
	}
}

func TestAmendRejectsRepoWithNoParentCommit(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	err := Amend("")
	if err == nil {
		t.Fatal("amend succeeded with no parent commit")
	}
	want := "repo \"app\" cannot amend because HEAD has no parent commit"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestRepoModePathReturnsBackingRepoPath(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeTwoRepoManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	path, err := RepoModePath("app")
	if err != nil {
		t.Fatalf("repo-mode app: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, ".gitplex", "repos", "app")
	if path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}
}

func TestRepoModePathRejectsUnknownRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	_, err := RepoModePath("missing")
	if err == nil {
		t.Fatal("repo-mode succeeded with unknown repo")
	}
	want := "repo \"missing\" does not exist in manifest"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestShellInitPrintsRepoModeWrapper(t *testing.T) {
	out, err := captureStdout(t, ShellInit)
	if err != nil {
		t.Fatalf("shell-init: %v", err)
	}
	for _, want := range []string{
		"gitplex() {",
		`repo-mode)`,
		`gitplex_path="$(command `,
		` "$@")" || return`,
		`cd "$gitplex_path"`,
		`command `,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("shell-init output missing %q:\n%s", want, out)
		}
	}
}

func TestShellQuote(t *testing.T) {
	got := shellQuote(`/tmp/gitplex user's/bin/gitplex`)
	want := `'/tmp/gitplex user'\''s/bin/gitplex'`
	if got != want {
		t.Fatalf("shellQuote = %q, want %q", got, want)
	}
}

func TestWorkspaceModeRebuildsWorkspaceWithoutChangingBackingRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	repoPath := filepath.Join(root, ".gitplex", "repos", "app")
	repoHead := gitTest(t, repoPath, "rev-parse", "HEAD")
	repoContent, err := os.ReadFile(filepath.Join(repoPath, "src", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "workspace", "local-only.txt"), []byte("remove me\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	path, err := WorkspaceModePath(true)
	if err != nil {
		t.Fatalf("workspace-mode --force: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	wantPath := filepath.Join(cwd, "workspace")
	if path != wantPath {
		t.Fatalf("path = %q, want %q", path, wantPath)
	}
	rebuiltContent, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(rebuiltContent) != "main src\n" {
		t.Fatalf("workspace content = %q, want main src", rebuiltContent)
	}
	if _, err := os.Stat(filepath.Join(root, "workspace", "local-only.txt")); !os.IsNotExist(err) {
		t.Fatalf("local-only file still exists or stat failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "workspace", ".git")); err != nil {
		t.Fatalf("workspace .git missing: %v", err)
	}
	if gotHead := gitTest(t, repoPath, "rev-parse", "HEAD"); gotHead != repoHead {
		t.Fatalf("repo HEAD = %q, want %q", gotHead, repoHead)
	}
	gotRepoContent, err := os.ReadFile(filepath.Join(repoPath, "src", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(gotRepoContent) != string(repoContent) {
		t.Fatalf("repo content = %q, want %q", gotRepoContent, repoContent)
	}
	if status := gitTest(t, repoPath, "status", "--porcelain"); status != "" {
		t.Fatalf("repo status = %q, want clean", status)
	}
}

func TestWorkspaceModeRefusesDirtyWorkspaceWithoutForce(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	workspaceFile := filepath.Join(root, "workspace", "app", "README.md")
	if err := os.WriteFile(workspaceFile, []byte("workspace edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WorkspaceModePath(false)
	if err == nil {
		t.Fatal("workspace-mode succeeded with dirty workspace")
	}
	want := "workspace has local changes in [app]; run gitplex push, gitplex stash, or rerun workspace-mode --force to discard them"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
	content, readErr := os.ReadFile(workspaceFile)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(content) != "workspace edit\n" {
		t.Fatalf("workspace content = %q, want dirty edit preserved", content)
	}
}

func TestWorkspaceModeRefusesDirtyBackingRepo(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	repoFile := filepath.Join(root, ".gitplex", "repos", "app", "src", "LOCAL.md")
	if err := os.WriteFile(repoFile, []byte("repo edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := WorkspaceModePath(true)
	if err == nil {
		t.Fatal("workspace-mode succeeded with dirty repo")
	}
	want := "repo \"app\" has uncommitted changes; commit or discard them before workspace-mode"
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}

func TestWorkspaceModePrintsOnlyWorkspacePath(t *testing.T) {
	remote := seedRemoteRepo(t)
	root := t.TempDir()
	chdir(t, root)

	manifestPath := filepath.Join(root, "manifest.yaml")
	writeSrcManifest(t, manifestPath, remote, "main")
	if err := Init(manifestPath); err != nil {
		t.Fatalf("init main: %v", err)
	}

	out, err := captureStdout(t, func() error {
		return WorkspaceMode(false)
	})
	if err != nil {
		t.Fatalf("workspace-mode: %v", err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(cwd, "workspace") + "\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q", out, want)
	}
}

func TestParseWorkspaceModeArgs(t *testing.T) {
	force, err := parseWorkspaceModeArgs(nil)
	if err != nil {
		t.Fatalf("parse workspace-mode: %v", err)
	}
	if force {
		t.Fatal("force = true, want false")
	}

	force, err = parseWorkspaceModeArgs([]string{"--force"})
	if err != nil {
		t.Fatalf("parse workspace-mode --force: %v", err)
	}
	if !force {
		t.Fatal("force = false, want true")
	}

	force, err = parseWorkspaceModeArgs([]string{"-f"})
	if err != nil {
		t.Fatalf("parse workspace-mode -f: %v", err)
	}
	if !force {
		t.Fatal("force = false, want true")
	}

	if _, err := parseWorkspaceModeArgs([]string{"--bad"}); err == nil {
		t.Fatal("parseWorkspaceModeArgs succeeded, want usage error")
	}
}

func TestParseRepoModeArgs(t *testing.T) {
	repo, err := parseRepoModeArgs([]string{"app"})
	if err != nil {
		t.Fatalf("parse repo-mode: %v", err)
	}
	if repo != "app" {
		t.Fatalf("repo = %q, want app", repo)
	}

	for _, args := range [][]string{nil, []string{"app", "extra"}} {
		if _, err := parseRepoModeArgs(args); err == nil {
			t.Fatalf("parseRepoModeArgs(%v) succeeded, want usage error", args)
		}
	}
}

func TestParseShellInitArgs(t *testing.T) {
	if err := parseShellInitArgs(nil); err != nil {
		t.Fatalf("parse shell-init: %v", err)
	}
	if err := parseShellInitArgs([]string{"--shell", "zsh"}); err == nil {
		t.Fatal("parseShellInitArgs succeeded, want usage error")
	}
}

func TestParseBuildArgs(t *testing.T) {
	if err := parseBuildArgs(nil); err != nil {
		t.Fatalf("parse build: %v", err)
	}
	if err := parseBuildArgs([]string{"--bad"}); err == nil {
		t.Fatal("parseBuildArgs succeeded, want usage error")
	}
}

func TestParseTrueBuildArgs(t *testing.T) {
	if err := parseTrueBuildArgs(nil); err != nil {
		t.Fatalf("parse true-build: %v", err)
	}
	if err := parseTrueBuildArgs([]string{"--bad"}); err == nil {
		t.Fatal("parseTrueBuildArgs succeeded, want usage error")
	}
}

func captureStdout(t *testing.T, fn func() error) (string, error) {
	t.Helper()
	originalStdout := os.Stdout
	file, err := os.CreateTemp("", "gitplex-test-stdout-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(file.Name())

	os.Stdout = file
	runErr := fn()
	os.Stdout = originalStdout
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(data), runErr
}

func seedRemoteRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	gitTest(t, repo, "init", "--initial-branch=main")
	gitTest(t, repo, "config", "user.name", "Gitplex Test")
	gitTest(t, repo, "config", "user.email", "gitplex@example.test")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(repo, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "README.md"), []byte("main src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "README.md")
	gitTest(t, repo, "add", "src/README.md")
	gitTest(t, repo, "commit", "-m", "main")
	gitTest(t, repo, "checkout", "-b", "release-sandbox")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("release\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "README.md"), []byte("release src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "commit", "-am", "release")
	gitTest(t, repo, "checkout", "main")
	return repo
}

func seedRemoteRepoWithFlake(t *testing.T) string {
	t.Helper()
	repo := seedRemoteRepo(t)
	flake := `{
  inputs = {};
  outputs = { self }: {};
}
`
	if err := os.WriteFile(filepath.Join(repo, "flake.nix"), []byte(flake), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "flake.nix")
	gitTest(t, repo, "commit", "-m", "add flake")
	return repo
}

func seedRemoteRepoWithDependencyFlake(t *testing.T) string {
	t.Helper()
	repo := seedRemoteRepo(t)
	flake := `{
  inputs = {
    common.url = "github:example/common";
    dep = {
      type = "git";
      url = "ssh://git@example.test/dep.git";
      ref = "main";
      inputs.common.follows = "common";
    };
  };
  outputs = { self, common, dep }: {};
}
`
	if err := os.WriteFile(filepath.Join(repo, "flake.nix"), []byte(flake), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", "flake.nix")
	gitTest(t, repo, "commit", "-m", "add dependency flake")
	return repo
}

func seedRemoteRepoWithFlakeAndEnvrc(t *testing.T) string {
	t.Helper()
	repo := seedRemoteRepoWithFlake(t)
	if err := os.WriteFile(filepath.Join(repo, ".envrc"), []byte("use flake\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", ".envrc")
	gitTest(t, repo, "commit", "-m", "add envrc")
	return repo
}

func seedRemoteRepoWithGitIgnore(t *testing.T) string {
	t.Helper()
	repo := seedRemoteRepo(t)
	content := `# app ignores
build/
.env
/logs
!logs/keep
nested/*.tmp
`
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "add", ".gitignore")
	gitTest(t, repo, "commit", "-m", "add gitignore")
	return repo
}

func prependFakeNix(t *testing.T, root string) {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nixPath := filepath.Join(binDir, "nix")
	script := `#!/bin/sh
if [ "$1" = "flake" ] && [ "$2" = "lock" ]; then
  printf '{"nodes":{},"root":"root","version":7}\n' > flake.lock
  exit 0
fi
echo "unexpected nix args: $*" >&2
exit 1
`
	if err := os.WriteFile(nixPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func prependRecordingFakeNix(t *testing.T, root string) (string, string) {
	t.Helper()
	binDir := filepath.Join(root, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cwdLog := filepath.Join(root, "nix-cwd.log")
	argsLog := filepath.Join(root, "nix-args.log")
	t.Setenv("GITPLEX_FAKE_NIX_CWD_LOG", cwdLog)
	t.Setenv("GITPLEX_FAKE_NIX_ARGS_LOG", argsLog)

	nixPath := filepath.Join(binDir, "nix")
	script := `#!/bin/sh
printf '%s\n' "$PWD" > "$GITPLEX_FAKE_NIX_CWD_LOG"
: > "$GITPLEX_FAKE_NIX_ARGS_LOG"
for arg in "$@"; do
  printf '%s\n' "$arg" >> "$GITPLEX_FAKE_NIX_ARGS_LOG"
done
exit 0
`
	if err := os.WriteFile(nixPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return cwdLog, argsLog
}

func addRemoteCommit(t *testing.T, repo, readmeContent string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte(readmeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "src", "README.md"), []byte(strings.TrimSpace(readmeContent)+" src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repo, "commit", "-am", "hotfix")
	return gitTest(t, repo, "rev-parse", "HEAD")
}

func commitBackingRepoChange(t *testing.T, root, repo, readmeContent, message string) string {
	t.Helper()
	repoPath := filepath.Join(root, ".gitplex", "repos", repo)
	if err := os.WriteFile(filepath.Join(repoPath, "README.md"), []byte(readmeContent), 0o644); err != nil {
		t.Fatal(err)
	}
	gitTest(t, repoPath, "add", "README.md")
	gitTest(t, repoPath, "commit", "-m", message)
	head := gitTest(t, repoPath, "rev-parse", "HEAD")

	manifest, state := loadProjectForTest(t, root)
	repoState := state.Repos[repo]
	repoState.Head = head
	state.Repos[repo] = repoState
	if err := refreshWorkspace(root, manifest, state); err != nil {
		t.Fatal(err)
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		t.Fatal(err)
	}
	if err := saveState(root, state); err != nil {
		t.Fatal(err)
	}
	return head
}

func loadProjectForTest(t *testing.T, root string) (Manifest, State) {
	t.Helper()
	state, err := loadState(root)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := loadManifest(state.ManifestPath)
	if err != nil {
		t.Fatal(err)
	}
	return manifest, state
}

func writeManifest(t *testing.T, path, remote, ref string) {
	t.Helper()
	data := []byte("workspace: workspace\n\nrepos:\n  app:\n    url: " + remote + "\n    ref: " + ref + "\n    modules:\n      - from: .\n        to: app\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeManifestWithWorkspaceFlake(t *testing.T, path, remote, ref string) {
	t.Helper()
	data := []byte("workspace: workspace\nworkspace_files:\n  - repo: app\n    from: flake.nix\n    to: flake.nix\n\nrepos:\n  app:\n    url: " + remote + "\n    ref: " + ref + "\n    modules:\n      - from: .\n        to: app\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeDependencyManifestWithWorkspaceFlake(t *testing.T, path, appRemote, depRemote string) {
	t.Helper()
	data := []byte("workspace: workspace\nworkspace_files:\n  - repo: app\n    from: flake.nix\n    to: flake.nix\n\nrepos:\n  dep:\n    url: " + depRemote + "\n    ref: main\n    modules:\n      - from: .\n        to: dep\n  app:\n    url: " + appRemote + "\n    ref: main\n    modules:\n      - from: .\n        to: app\n    dependencies:\n      dep:\n        flake_input: dep\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeTwoRepoManifest(t *testing.T, path, remote, ref string) {
	t.Helper()
	data := []byte("workspace: workspace\n\nrepos:\n  app:\n    url: " + remote + "\n    ref: " + ref + "\n    modules:\n      - from: .\n        to: app\n  lib:\n    url: " + remote + "\n    ref: " + ref + "\n    modules:\n      - from: .\n        to: lib\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeSrcManifest(t *testing.T, path, remote, ref string) {
	t.Helper()
	data := []byte("workspace: workspace\n\nrepos:\n  app:\n    url: " + remote + "\n    ref: " + ref + "\n    modules:\n      - from: src\n        to: app\n")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatal(err)
		}
	})
}

func gitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSuffix(string(out), "\n")
}
