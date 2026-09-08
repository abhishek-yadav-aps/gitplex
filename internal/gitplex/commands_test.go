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
