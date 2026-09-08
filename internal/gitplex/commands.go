package gitplex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type repoStatus struct {
	name            string
	branch          string
	expectedBranch  string
	workspaceDirty  bool
	repoDirty       bool
	upstream        string
	upstreamMissing bool
	headChanged     bool
}

func Init(manifestPath string) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}
	if err := ensureWorkspaceCleanForInit(root); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(root, ".gitplex", "repos"), 0o755); err != nil {
		return err
	}
	if err := copyFile(manifestPath, filepath.Join(root, ".gitplex", "manifest.yaml"), 0o644); err != nil {
		return err
	}

	state := State{
		ManifestPath: filepath.Join(root, ".gitplex", "manifest.yaml"),
		Workspace:    manifest.Workspace,
		Repos:        map[string]RepoState{},
	}

	for name, repo := range manifest.Repos {
		repoPath := filepath.Join(root, ".gitplex", "repos", name)
		if _, err := os.Stat(filepath.Join(repoPath, ".git")); os.IsNotExist(err) {
			fmt.Printf("cloning %s\n", name)
			if err := cloneRepo(root, repo, repoPath); err != nil {
				return err
			}
		} else {
			fmt.Printf("using existing repo %s\n", name)
		}
		if repo.Ref != "" {
			if err := checkoutRepoRef(name, repoPath, repo.Ref); err != nil {
				return err
			}
		}
		fmt.Printf("reading %s HEAD\n", name)
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		state.Repos[name] = RepoState{URL: repo.URL, Path: repoPath, Head: head}
	}

	fmt.Printf("refreshing workspace %s\n", manifest.Workspace)
	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	fmt.Println("generating workspace project files")
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	fmt.Println("saving gitplex state")
	return saveState(root, state)
}

func ensureWorkspaceCleanForInit(root string) error {
	state, err := loadState(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check workspace changes before init: %w", err)
	}
	manifest, err := loadManifest(state.ManifestPath)
	if err != nil {
		return fmt.Errorf("check workspace changes before init: %w", err)
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return fmt.Errorf("check workspace changes before init: %w", err)
	}
	if len(changed) > 0 {
		return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before init", changed)
	}
	return nil
}

func checkoutRepoRef(name, repoPath, ref string) error {
	fmt.Printf("checking out %s -> %s\n", name, ref)
	if _, err := git(repoPath, "checkout", ref); err == nil {
		return nil
	}

	fmt.Printf("fetching %s from origin for %s\n", ref, name)
	remoteBranch := "refs/remotes/origin/" + ref
	if _, err := git(repoPath, "fetch", "--depth", "1", "origin", ref+":"+remoteBranch); err == nil {
		if _, err := git(repoPath, "checkout", "-B", ref, remoteBranch); err != nil {
			return err
		}
		_, _ = git(repoPath, "branch", "--set-upstream-to=origin/"+ref, ref)
		return nil
	}

	fmt.Printf("fetching %s as detached ref for %s\n", ref, name)
	if _, err := git(repoPath, "fetch", "--depth", "1", "origin", ref); err != nil {
		return err
	}
	_, err := git(repoPath, "checkout", "FETCH_HEAD")
	return err
}

func cloneRepo(root string, repo RepoConfig, repoPath string) error {
	args := []string{"clone"}
	if repo.Ref != "" {
		args = append(args, "--branch", repo.Ref, "--single-branch", "--depth", "1")
	}
	args = append(args, repo.URL, repoPath)
	_, err := git(root, args...)
	return err
}

func Status() error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}

	changedSet := make(map[string]bool, len(changed))
	for _, name := range changed {
		changedSet[name] = true
	}

	workspacePath := filepath.Join(root, manifest.Workspace)
	fmt.Printf("workspace: %s\n", workspacePath)
	fmt.Printf("branch: %s\n", branchDisplay(state))
	if len(changed) == 0 {
		fmt.Println("workspace sync: clean")
	} else {
		fmt.Printf("workspace sync: changed in %d repo(s)\n", len(changed))
	}
	fmt.Println()

	repoNames := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	var statuses []repoStatus
	var warnCount int
	for _, name := range repoNames {
		repo := manifest.Repos[name]
		repoState := state.Repos[name]

		repoBranch, err := git(repoState.Path, "branch", "--show-current")
		if err != nil {
			return err
		}
		dirty, err := gitHasChanges(repoState.Path)
		if err != nil {
			return err
		}
		upstream, upstreamErr := git(repoState.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
		head, err := gitHead(repoState.Path)
		if err != nil {
			return err
		}

		status := repoStatus{
			name:            name,
			branch:          repoBranch,
			expectedBranch:  currentBranch(state, name, repo),
			workspaceDirty:  changedSet[name],
			repoDirty:       dirty,
			upstream:        upstream,
			upstreamMissing: upstreamErr != nil || upstream == "",
			headChanged:     repoState.Head != "" && head != repoState.Head,
		}
		statuses = append(statuses, status)

		statusParts := []string{fmt.Sprintf("branch=%s", repoBranch)}
		if status.expectedBranch != "" && status.branch != status.expectedBranch {
			statusParts = append(statusParts, fmt.Sprintf("expected=%s", status.expectedBranch))
			warnCount++
		}
		if status.workspaceDirty {
			statusParts = append(statusParts, "workspace=changed")
			warnCount++
		} else {
			statusParts = append(statusParts, "workspace=clean")
		}
		if status.repoDirty {
			statusParts = append(statusParts, "repo=dirty")
			warnCount++
		} else {
			statusParts = append(statusParts, "repo=clean")
		}
		if !status.upstreamMissing {
			statusParts = append(statusParts, fmt.Sprintf("upstream=%s", status.upstream))
		} else {
			statusParts = append(statusParts, "upstream=missing")
			warnCount++
		}
		if status.headChanged {
			statusParts = append(statusParts, "head=changed")
			warnCount++
		}

		fmt.Printf("%s: %s\n", name, strings.Join(statusParts, ", "))
	}

	summary := overallStatusSummary(state, statuses)
	fmt.Printf("\nsummary: %s\n", summary)
	if len(changed) == 0 && warnCount == 0 {
		return nil
	}
	fmt.Printf("details: %d repo(s) with workspace changes, %d warning signal(s)\n", len(changed), warnCount)
	return nil
}

func overallStatusSummary(state State, statuses []repoStatus) string {
	var workspaceDirtyCount int
	var repoDirty []string
	var branchMismatch []string
	var upstreamMissing []string
	var headChanged []string

	for _, status := range statuses {
		if status.workspaceDirty {
			workspaceDirtyCount++
		}
		if status.repoDirty {
			repoDirty = append(repoDirty, status.name)
		}
		if status.expectedBranch == "" {
			branchMismatch = append(branchMismatch, status.name)
			continue
		}
		if status.branch != status.expectedBranch {
			branchMismatch = append(branchMismatch, status.name)
		}
		if status.upstreamMissing {
			upstreamMissing = append(upstreamMissing, status.name)
		}
		if status.headChanged {
			headChanged = append(headChanged, status.name)
		}
	}

	if len(repoDirty) > 0 {
		return fmt.Sprintf("blocked: backing repo changes need attention in %s", strings.Join(repoDirty, ", "))
	}
	if len(branchMismatch) > 0 {
		if state.Branch == "" {
			return fmt.Sprintf("needs branch: run gitplex branch <branch> before push (%s)", strings.Join(branchMismatch, ", "))
		}
		return fmt.Sprintf("needs branch alignment in %s", strings.Join(branchMismatch, ", "))
	}
	if len(headChanged) > 0 {
		return fmt.Sprintf("needs sync: backing repo head changed in %s", strings.Join(headChanged, ", "))
	}
	if workspaceDirtyCount > 0 {
		if len(upstreamMissing) > 0 {
			return fmt.Sprintf("ready to push, but upstream missing in %s", strings.Join(upstreamMissing, ", "))
		}
		return "ready to push"
	}
	if len(upstreamMissing) > 0 {
		return fmt.Sprintf("clean, but upstream missing in %s", strings.Join(upstreamMissing, ", "))
	}
	return "clean and ready"
}

func Pull() error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before pull", changed)
	}

	for name := range manifest.Repos {
		repoPath := state.Repos[name].Path
		if _, err := git(repoPath, "pull", "--ff-only"); err != nil {
			return err
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = head
		state.Repos[name] = repoState
	}
	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	return saveState(root, state)
}

func Stash(args []string) error {
	root, manifest, _, err := loadProject()
	if err != nil {
		return err
	}
	workspacePath := filepath.Join(root, manifest.Workspace)
	gitArgs := append([]string{"stash"}, args...)
	return runGitAndPrint(workspacePath, gitArgs...)
}

func Rebase(repoName, branch string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before rebase", changed)
	}

	repoNames, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return err
	}
	for _, name := range repoNames {
		repoPath := state.Repos[name].Path
		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("repo %q has uncommitted changes; commit or discard them before rebase", name)
		}
	}

	for _, name := range repoNames {
		repoPath := state.Repos[name].Path
		fmt.Printf("rebasing %s onto %s\n", name, branch)
		if err := fetchBranchForRebase(name, repoPath, branch); err != nil {
			return err
		}
		if err := runGitAndPrint(repoPath, "rebase", "origin/"+branch); err != nil {
			return err
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = head
		state.Repos[name] = repoState
	}

	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	return saveState(root, state)
}

func CherryPick(repoName, commit string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	repoNames, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return err
	}
	name := repoNames[0]
	repoPath := state.Repos[name].Path

	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		workspaceDirty, err := gitHasChanges(filepath.Join(root, manifest.Workspace))
		if err != nil {
			return err
		}
		if workspaceDirty {
			return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before cherrypick", changed)
		}
		fmt.Println("workspace is out of sync; refreshing from backing repos")
		if err := refreshWorkspace(root, manifest, state); err != nil {
			return err
		}
		if err := generateWorkspaceProject(root, manifest, state); err != nil {
			return err
		}
	}

	dirty, err := gitHasChanges(repoPath)
	if err != nil {
		return err
	}
	if dirty {
		return fmt.Errorf("repo %q has uncommitted changes; commit or discard them before cherrypick", name)
	}

	if err := ensureCommitAvailableForCherryPick(name, repoPath, commit); err != nil {
		return err
	}
	alreadyApplied, err := gitCommitAlreadyApplied(repoPath, commit)
	if err != nil {
		return err
	}
	if alreadyApplied {
		fmt.Printf("commit %s is already applied in %s; refreshing workspace\n", commit, name)
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = head
		state.Repos[name] = repoState
		if err := refreshWorkspace(root, manifest, state); err != nil {
			return err
		}
		if err := generateWorkspaceProject(root, manifest, state); err != nil {
			return err
		}
		return saveState(root, state)
	}
	fmt.Printf("cherry-picking %s into %s\n", commit, name)
	if err := runGitAndPrint(repoPath, "cherry-pick", commit); err != nil {
		return err
	}
	head, err := gitHead(repoPath)
	if err != nil {
		return err
	}
	repoState := state.Repos[name]
	repoState.Head = head
	state.Repos[name] = repoState

	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	return saveState(root, state)
}

func selectedRepoNames(manifest Manifest, repoName string) ([]string, error) {
	if repoName != "" {
		if _, ok := manifest.Repos[repoName]; !ok {
			return nil, fmt.Errorf("repo %q does not exist in manifest", repoName)
		}
		return []string{repoName}, nil
	}
	repoNames := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)
	return repoNames, nil
}

func RepoMode(repoName string) error {
	path, err := RepoModePath(repoName)
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func RepoModePath(repoName string) (string, error) {
	root, manifest, state, err := loadProject()
	if err != nil {
		return "", err
	}
	repoNames, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return "", err
	}
	name := repoNames[0]
	repoState, ok := state.Repos[name]
	if !ok {
		return "", fmt.Errorf("repo %q is missing from state", name)
	}
	if filepath.IsAbs(repoState.Path) {
		return repoState.Path, nil
	}
	return filepath.Join(root, repoState.Path), nil
}

func WorkspaceMode(force bool) error {
	originalStdout := os.Stdout
	sink, err := os.CreateTemp("", "gitplex-workspace-mode-*")
	if err != nil {
		return err
	}
	defer os.Remove(sink.Name())

	os.Stdout = sink
	path, err := WorkspaceModePath(force)
	os.Stdout = originalStdout
	if closeErr := sink.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}

func WorkspaceModePath(force bool) (string, error) {
	root, manifest, state, err := loadProject()
	if err != nil {
		return "", err
	}
	workspacePath := filepath.Join(root, manifest.Workspace)
	if err := ensureBackingReposClean(manifest, state, "workspace-mode"); err != nil {
		return "", err
	}
	if !force {
		if _, statErr := os.Stat(workspacePath); statErr == nil {
			changed, err := changedRepos(root, manifest, state)
			if err != nil {
				return "", err
			}
			if len(changed) > 0 {
				return "", fmt.Errorf("workspace has local changes in %v; run gitplex push, gitplex stash, or rerun workspace-mode --force to discard them", changed)
			}
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
	if err := removeGeneratedPath(workspacePath); err != nil {
		return "", err
	}
	if err := refreshWorkspace(root, manifest, state); err != nil {
		return "", err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return "", err
	}
	if err := saveState(root, state); err != nil {
		return "", err
	}
	return workspacePath, nil
}

func ensureBackingReposClean(manifest Manifest, state State, command string) error {
	for name := range manifest.Repos {
		repoState, ok := state.Repos[name]
		if !ok {
			return fmt.Errorf("repo %q is missing from state", name)
		}
		repoPath := repoState.Path
		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("repo %q has uncommitted changes; commit or discard them before %s", name, command)
		}
	}
	return nil
}

func ensureCommitAvailableForCherryPick(name, repoPath, commit string) error {
	if _, err := git(repoPath, "rev-parse", "--verify", commit+"^{commit}"); err == nil {
		return nil
	}
	if _, err := git(repoPath, "fetch", "origin", commit); err != nil {
		return fmt.Errorf("fetch commit %q for repo %q: %w", commit, name, err)
	}
	if _, err := git(repoPath, "rev-parse", "--verify", commit+"^{commit}"); err != nil {
		return fmt.Errorf("commit %q is not available for repo %q: %w", commit, name, err)
	}
	return nil
}

func gitCommitIsAncestor(repoPath, ancestor, descendant string) (bool, error) {
	_, _, err := gitOutput(repoPath, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", ancestor, descendant, err)
}

func gitCommitAlreadyApplied(repoPath, commit string) (bool, error) {
	ancestor, err := gitCommitIsAncestor(repoPath, commit, "HEAD")
	if err != nil {
		return false, err
	}
	if ancestor {
		return true, nil
	}
	out, err := git(repoPath, "cherry", "HEAD", commit)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(out, "-"), nil
}

func fetchBranchForRebase(name, repoPath, branch string) error {
	remoteBranch := "refs/remotes/origin/" + branch
	if _, err := git(repoPath, "fetch", "origin", branch+":"+remoteBranch); err != nil {
		return fmt.Errorf("fetch branch %q for repo %q: %w", branch, name, err)
	}
	return nil
}

func Checkout(repoName, branch string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before checkout", changed)
	}

	repoNames, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return err
	}
	for _, name := range repoNames {
		repoPath := state.Repos[name].Path
		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("repo %q has uncommitted changes; commit or discard them before checkout", name)
		}
	}

	for _, name := range repoNames {
		repoPath := state.Repos[name].Path
		if err := checkoutRepoRef(name, repoPath, branch); err != nil {
			return err
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = head
		if repoName == "" {
			repoState.Branch = ""
		} else {
			repoState.Branch = branch
		}
		state.Repos[name] = repoState
	}

	if repoName == "" {
		state.Branch = branch
	}
	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	return saveState(root, state)
}

func Branch(branch string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before branch", changed)
	}

	for name := range manifest.Repos {
		repoPath := state.Repos[name].Path
		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("repo %q has uncommitted changes; commit or discard them before branch", name)
		}
	}

	for name := range manifest.Repos {
		repoPath := state.Repos[name].Path
		if _, err := git(repoPath, "checkout", "-B", branch); err != nil {
			return err
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = head
		repoState.Branch = ""
		state.Repos[name] = repoState
		fmt.Printf("branched %s -> %s\n", name, branch)
	}

	state.Branch = branch
	return saveState(root, state)
}

func Amend(message string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	order, err := topoOrder(manifest)
	if err != nil {
		return err
	}
	for _, name := range order {
		repo := manifest.Repos[name]
		repoPath := state.Repos[name].Path
		if currentBranch(state, name, repo) == "" {
			return fmt.Errorf("repo %q has no amend branch; run gitplex branch <branch> or set repo ref in manifest", name)
		}
		if err := ensureAmendableHead(name, repoPath); err != nil {
			return err
		}
	}

	publishedHeads := map[string]string{}
	for _, name := range order {
		repo := manifest.Repos[name]
		repoPath := state.Repos[name].Path
		fmt.Printf("\n== %s ==\n", name)

		commitMessage := message
		if commitMessage == "" {
			commitMessage, err = git(repoPath, "log", "-1", "--pretty=%B")
			if err != nil {
				return err
			}
		}
		if err := syncWorkspaceToRepo(root, manifest, state, name); err != nil {
			return err
		}
		var updatedFlakeInputs []string
		for depName, depConfig := range repo.Dependencies {
			if head := publishedHeads[depName]; head != "" {
				depRef := currentBranch(state, depName, manifest.Repos[depName])
				if depRef == "" {
					return fmt.Errorf("dependency repo %q has no ref for flake update", depName)
				}
				if err := updateFlakeInput(repoPath, depConfig.FlakeInput, depRef, head); err != nil {
					return err
				}
				updatedFlakeInputs = append(updatedFlakeInputs, depConfig.FlakeInput)
			}
		}
		for _, flakeInput := range updatedFlakeInputs {
			if err := runCommandAndPrint(repoPath, "nix", "flake", "lock", "--update-input", flakeInput); err != nil {
				return err
			}
		}

		if err := runGitAndPrint(repoPath, "reset", "--mixed", "HEAD~1"); err != nil {
			return err
		}
		if _, err := git(repoPath, "add", "-A"); err != nil {
			return err
		}
		if err := gitCommitWithMessage(repoPath, commitMessage); err != nil {
			return err
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		publishedHeads[name] = head
		repoState := state.Repos[name]
		repoState.Head = head
		state.Repos[name] = repoState
	}

	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	return saveState(root, state)
}

func ensureAmendableHead(name, repoPath string) error {
	if _, err := git(repoPath, "rev-parse", "--verify", "HEAD~1"); err != nil {
		return fmt.Errorf("repo %q cannot amend because HEAD has no parent commit", name)
	}
	return nil
}

func gitCommitWithMessage(repoPath, message string) error {
	file, err := os.CreateTemp("", "gitplex-commit-message-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.WriteString(message); err != nil {
		file.Close()
		return err
	}
	if !strings.HasSuffix(message, "\n") {
		if _, err := file.WriteString("\n"); err != nil {
			file.Close()
			return err
		}
	}
	if err := file.Close(); err != nil {
		return err
	}
	return runGitAndPrint(repoPath, "commit", "--allow-empty", "-F", file.Name())
}

func Push(message string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	order, err := topoOrder(manifest)
	if err != nil {
		return err
	}
	publishedHeads := map[string]string{}
	failedRepos := map[string]bool{}
	var pushErrors []string

	for _, name := range order {
		repo := manifest.Repos[name]
		repoPath := state.Repos[name].Path
		fmt.Printf("\n== %s ==\n", name)
		if currentBranch(state, name, repo) == "" {
			return fmt.Errorf("repo %q has no push branch; run gitplex branch <branch> or set repo ref in manifest", name)
		}
		if err := syncWorkspaceToRepo(root, manifest, state, name); err != nil {
			return err
		}
		skipRepo := false
		var updatedFlakeInputs []string
		for depName, depConfig := range repo.Dependencies {
			if failedRepos[depName] {
				msg := fmt.Sprintf("skipped because dependency repo %q did not push successfully", depName)
				fmt.Println(msg)
				failedRepos[name] = true
				pushErrors = append(pushErrors, fmt.Sprintf("%s: %s", name, msg))
				skipRepo = true
				break
			}
			if head := publishedHeads[depName]; head != "" {
				depRef := currentBranch(state, depName, manifest.Repos[depName])
				if depRef == "" {
					return fmt.Errorf("dependency repo %q has no ref for flake update", depName)
				}
				if err := updateFlakeInput(repoPath, depConfig.FlakeInput, depRef, head); err != nil {
					return err
				}
				updatedFlakeInputs = append(updatedFlakeInputs, depConfig.FlakeInput)
			}
		}
		if skipRepo {
			continue
		}
		for _, flakeInput := range updatedFlakeInputs {
			if err := runCommandAndPrint(repoPath, "nix", "flake", "lock", "--update-input", flakeInput); err != nil {
				failedRepos[name] = true
				pushErrors = append(pushErrors, fmt.Sprintf("%s: %v", name, err))
				skipRepo = true
				break
			}
		}
		if skipRepo {
			continue
		}
		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if dirty {
			if _, err := git(repoPath, "add", "-A"); err != nil {
				return err
			}
			if err := runGitAndPrint(repoPath, "commit", "-m", message); err != nil {
				return err
			}
		} else {
			fmt.Println("no changes to commit")
		}
		if err := runGitAndPrint(repoPath, "push", "-u", "origin", currentBranch(state, name, repo)); err != nil {
			failedRepos[name] = true
			pushErrors = append(pushErrors, fmt.Sprintf("%s: %v", name, err))
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		publishedHeads[name] = head
		repoState := state.Repos[name]
		repoState.Head = head
		state.Repos[name] = repoState
	}

	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	if err := saveState(root, state); err != nil {
		return err
	}
	if len(pushErrors) > 0 {
		return fmt.Errorf("push failed:\n%s", strings.Join(pushErrors, "\n"))
	}
	return nil
}

func runGitAndPrint(dir string, args ...string) error {
	return runCommandAndPrint(dir, "git", args...)
}

func runCommandAndPrint(dir, command string, args ...string) error {
	fmt.Printf("running: %s %s\n", command, strings.Join(args, " "))
	if err := commandStreaming(dir, command, args...); err != nil {
		return fmt.Errorf("%s %s: %w", command, strings.Join(args, " "), err)
	}
	return nil
}

func currentBranch(state State, repoName string, repo RepoConfig) string {
	if repoState, ok := state.Repos[repoName]; ok && repoState.Branch != "" {
		return repoState.Branch
	}
	if state.Branch != "" {
		return state.Branch
	}
	return repo.Ref
}

func branchDisplay(state State) string {
	hasRepoOverride := false
	for _, repoState := range state.Repos {
		if repoState.Branch != "" {
			hasRepoOverride = true
			break
		}
	}
	if state.Branch != "" {
		if hasRepoOverride {
			return state.Branch + " (with repo overrides)"
		}
		return state.Branch
	}
	if hasRepoOverride {
		return "(mixed)"
	}
	return "(manifest refs)"
}

func loadProject() (string, Manifest, State, error) {
	root, err := os.Getwd()
	if err != nil {
		return "", Manifest{}, State{}, err
	}
	state, err := loadState(root)
	if err != nil {
		return "", Manifest{}, State{}, err
	}
	manifest, err := loadManifest(state.ManifestPath)
	if err != nil {
		return "", Manifest{}, State{}, err
	}
	return root, manifest, state, nil
}

func refreshWorkspace(root string, manifest Manifest, state State) error {
	workspacePath := filepath.Join(root, manifest.Workspace)
	if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return err
	}
	for name, repo := range manifest.Repos {
		repoPath := state.Repos[name].Path
		for _, module := range repo.Modules {
			fmt.Printf("syncing %s:%s -> %s\n", name, module.From, filepath.Join(manifest.Workspace, module.To))
			dst := filepath.Join(workspacePath, module.To)
			if err := removeGeneratedPath(dst); err != nil {
				return err
			}
			if err := copyTree(filepath.Join(repoPath, module.From), dst); err != nil {
				return err
			}
		}
	}
	return nil
}

func syncWorkspaceToRepo(root string, manifest Manifest, state State, name string) error {
	repo := manifest.Repos[name]
	repoPath := state.Repos[name].Path
	workspacePath := filepath.Join(root, manifest.Workspace)
	for _, module := range repo.Modules {
		src := filepath.Join(workspacePath, module.To)
		dst := filepath.Join(repoPath, module.From)
		if err := mirrorTree(src, dst); err != nil {
			return err
		}
	}
	return nil
}

func changedRepos(root string, manifest Manifest, state State) ([]string, error) {
	var changed []string
	workspacePath := filepath.Join(root, manifest.Workspace)
	for name, repo := range manifest.Repos {
		repoPath := state.Repos[name].Path
		for _, module := range repo.Modules {
			diff, err := dirsDiffer(filepath.Join(repoPath, module.From), filepath.Join(workspacePath, module.To))
			if err != nil {
				return nil, err
			}
			if diff {
				changed = append(changed, name)
				break
			}
		}
	}
	return changed, nil
}
