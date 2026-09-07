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
	writeManifest(t, manifestPath, remote, "main")
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
