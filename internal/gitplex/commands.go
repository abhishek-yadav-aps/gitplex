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
	ahead           int
	behind          int
	headChanged     bool
	publishPhase    string
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
	sort.Strings(changed)

	changedSet := make(map[string]bool, len(changed))
	for _, name := range changed {
		changedSet[name] = true
	}
	journal, journalErr := loadPushJournal(root)
	if journalErr != nil && !os.IsNotExist(journalErr) {
		return journalErr
	}
	conflict, conflictErr := loadConflictJournal(root)
	if conflictErr != nil && !os.IsNotExist(conflictErr) {
		return conflictErr
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
		ahead, behind := 0, 0
		if upstreamErr == nil && upstream != "" {
			ahead, behind, err = gitAheadBehind(repoState.Path)
			if err != nil {
				return err
			}
		}
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
			ahead:           ahead,
			behind:          behind,
			headChanged:     repoState.Head != "" && head != repoState.Head,
		}
		if journal != nil {
			if journalRepo, ok := journal.Repos[name]; ok {
				status.publishPhase = journalRepo.Phase
			}
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
			statusParts = append(statusParts, fmt.Sprintf("ahead=%d", status.ahead), fmt.Sprintf("behind=%d", status.behind))
			if status.ahead > 0 || status.behind > 0 {
				warnCount++
			}
		} else {
			statusParts = append(statusParts, "upstream=missing")
			warnCount++
		}
		if status.headChanged {
			statusParts = append(statusParts, "head=changed")
			warnCount++
		}
		if status.publishPhase != "" {
			statusParts = append(statusParts, fmt.Sprintf("publish=%s", status.publishPhase))
			if status.publishPhase != pushPhasePushed && status.publishPhase != pushPhaseSkipped {
				warnCount++
			}
		}

		fmt.Printf("%s: %s\n", name, strings.Join(statusParts, ", "))
	}

	summary := overallStatusSummary(state, statuses, journal != nil)
	if conflict != nil {
		command := operationCommand(conflict.Operation)
		summary = fmt.Sprintf("unfinished %s in %s: run gitplex %s --continue or --abort", conflict.Operation, conflict.Order[conflict.Current], command)
		warnCount++
	}
	fmt.Printf("\nsummary: %s\n", summary)
	if len(changed) == 0 && warnCount == 0 {
		return nil
	}
	fmt.Printf("details: %d repo(s) with workspace changes, %d warning signal(s)\n", len(changed), warnCount)
	return nil
}

func overallStatusSummary(state State, statuses []repoStatus, unfinishedPush bool) string {
	var workspaceDirtyCount int
	var repoDirty []string
	var branchMismatch []string
	var upstreamMissing []string
	var headChanged []string
	var ahead []string
	var behind []string

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
		if status.ahead > 0 {
			ahead = append(ahead, status.name)
		}
		if status.behind > 0 {
			behind = append(behind, status.name)
		}
	}

	if len(repoDirty) > 0 {
		return fmt.Sprintf("blocked: backing repo changes need attention in %s", strings.Join(repoDirty, ", "))
	}
	if unfinishedPush {
		return "unfinished push: run gitplex push --resume"
	}
	if len(branchMismatch) > 0 {
		if state.Branch == "" {
			return fmt.Sprintf("needs branch: run gitplex branch <branch> before push (%s)", strings.Join(branchMismatch, ", "))
		}
		return fmt.Sprintf("needs branch alignment in %s", strings.Join(branchMismatch, ", "))
	}
	if len(behind) > 0 {
		return fmt.Sprintf("behind upstream in %s", strings.Join(behind, ", "))
	}
	if len(ahead) > 0 {
		return fmt.Sprintf("unpublished commits in %s", strings.Join(ahead, ", "))
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

func Build() error {
	return runWorkspaceNixBuild(false, true)
}

func TrueBuild() error {
	return runWorkspaceNixBuild(true, false)
}

func runWorkspaceNixBuild(disableSubstitutes, runCachePush bool) error {
	root, manifest, _, err := loadProject()
	if err != nil {
		return err
	}
	workspacePath := filepath.Join(root, manifest.Workspace)
	if runCachePush {
		if err := runBuildCacheSetup(workspacePath, manifest.Build.SetupCache); err != nil {
			return err
		}
	}
	args := []string{
		"build",
	}
	if disableSubstitutes {
		args = append(args, "--option", "substitute", "false")
	}
	if err := runCommandAndPrint(workspacePath, "nix", args...); err != nil {
		return err
	}
	if runCachePush {
		return runBuildCachePushCommand(workspacePath, manifest.Build.CachePushCommand)
	}
	return nil
}

func runBuildCacheSetup(workspacePath string, setup *SetupCacheConfig) error {
	if setup == nil {
		return nil
	}
	if _, err := exec.LookPath(setup.Command); err == nil {
		fmt.Printf("cache command %s is already available; skipping setup\n", setup.Command)
		return nil
	}
	fmt.Printf("cache command %s is not available; running setup\n", setup.Command)
	for _, command := range setup.Commands {
		if err := runCommandAndPrint(workspacePath, command[0], command[1:]...); err != nil {
			return err
		}
	}
	return nil
}

func runBuildCachePushCommand(workspacePath string, command []string) error {
	if len(command) == 0 {
		return nil
	}
	return runCommandAndPrint(workspacePath, command[0], command[1:]...)
}

func Rebase(repoName, branch string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	if err := ensureNoPublishJournal(root); err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	if len(changed) > 0 {
		return fmt.Errorf("workspace has local changes in %v; run gitplex push or discard them before rebase", changed)
	}
	if err := ensureNoConflictJournal(root); err != nil {
		return err
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

	journal, err := newConflictJournal("rebase", branch, repoNames, state)
	if err != nil {
		return err
	}
	if err := saveConflictJournal(root, journal); err != nil {
		return err
	}
	for index, name := range repoNames {
		journal.Current = index
		if err := saveConflictJournal(root, journal); err != nil {
			return err
		}
		repoPath := state.Repos[name].Path
		fmt.Printf("rebasing %s onto %s\n", name, branch)
		if err := fetchBranchForRebase(name, repoPath, branch); err != nil {
			return err
		}
		if err := runGitAndPrint(repoPath, "rebase", "origin/"+branch); err != nil {
			return conflictInstruction("rebase", name, err)
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = head
		state.Repos[name] = repoState
	}

	return completeConflictWorkflow(root, manifest, state, journal)
}

func CherryPick(repoName, commit string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	if err := ensureNoPublishJournal(root); err != nil {
		return err
	}
	repoNames, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return err
	}
	name := repoNames[0]
	repoPath := state.Repos[name].Path
	if err := ensureNoConflictJournal(root); err != nil {
		return err
	}

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
		if err := generateWorkspaceProjectWithBaseline(root, manifest, state, false); err != nil {
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
		if err := generateWorkspaceProjectWithBaseline(root, manifest, state, false); err != nil {
			return err
		}
		return saveState(root, state)
	}
	journal, err := newConflictJournal("cherry-pick", commit, []string{name}, state)
	if err != nil {
		return err
	}
	if err := saveConflictJournal(root, journal); err != nil {
		return err
	}
	fmt.Printf("cherry-picking %s into %s\n", commit, name)
	if err := runGitAndPrint(repoPath, "cherry-pick", commit); err != nil {
		return conflictInstruction("cherry-pick", name, err)
	}
	head, err := gitHead(repoPath)
	if err != nil {
		return err
	}
	repoState := state.Repos[name]
	repoState.Head = head
	state.Repos[name] = repoState

	return completeConflictWorkflow(root, manifest, state, journal)
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

func ShellInit() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	quotedExecutable := shellQuote(executable)
	fmt.Printf(`gitplex() {
  case "$1" in
    repo-mode)
      if [ "$#" -eq 2 ]; then
        gitplex_path="$(command %s repo-mode --print "$2")" || return
        cd "$gitplex_path"
        return
      fi
      ;;
    workspace-mode)
      if [ "$#" -eq 1 ] || { [ "$#" -eq 2 ] && { [ "$2" = "--force" ] || [ "$2" = "-f" ]; }; }; then
        gitplex_path="$(command %s "$@")" || return
        cd "$gitplex_path"
        return
      fi
      ;;
  esac
  command %s "$@"
}
`, quotedExecutable, quotedExecutable, quotedExecutable)
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func RepoMode(repoName string, printPath bool) error {
	path, err := RepoModePath(repoName)
	if err != nil {
		return err
	}
	if printPath {
		fmt.Println(path)
		return nil
	}
	return runInteractiveShell(path)
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

func runInteractiveShell(dir string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	cmd := exec.Command(shell)
	cmd.Dir = dir
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
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
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	root, manifest, state, err := loadProject()
	if err != nil {
		return "", err
	}
	workspacePath := filepath.Join(root, manifest.Workspace)
	fromWorkspace := pathContains(workspacePath, cwd)
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
	if fromWorkspace {
		if err := os.Chdir(root); err != nil {
			return "", fmt.Errorf("leave generated workspace before refresh: %w", err)
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
	if fromWorkspace {
		if err := os.Chdir(workspacePath); err != nil {
			return "", fmt.Errorf("enter regenerated workspace: %w", err)
		}
	}
	return workspacePath, nil
}

func pathContains(parent, child string) bool {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
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

		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if !dirty {
			fmt.Println("no changes to amend")
			continue
		}
		if currentBranch(state, name, repo) == "" {
			return fmt.Errorf("repo %q has no amend branch; run gitplex branch <branch> or set repo ref in manifest", name)
		}

		hasParent, err := gitHeadHasParent(repoPath)
		if err != nil {
			return err
		}
		if hasParent {
			if err := runGitAndPrint(repoPath, "reset", "--mixed", "HEAD~1"); err != nil {
				return err
			}
			if _, err := git(repoPath, "add", "-A"); err != nil {
				return err
			}
			if err := gitCommitWithMessage(repoPath, commitMessage); err != nil {
				return err
			}
		} else {
			if _, err := git(repoPath, "add", "-A"); err != nil {
				return err
			}
			if err := gitAmendWithMessage(repoPath, commitMessage); err != nil {
				return err
			}
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

func gitHeadHasParent(repoPath string) (bool, error) {
	_, _, err := gitOutput(repoPath, "rev-parse", "--verify", "HEAD~1")
	if err == nil {
		return true, nil
	}
	if _, ok := err.(*exec.ExitError); ok {
		return false, nil
	}
	return false, fmt.Errorf("git rev-parse --verify HEAD~1: %w", err)
}

func gitCommitWithMessage(repoPath, message string) error {
	return gitCommitWithMessageArgs(repoPath, message, "commit", "--allow-empty")
}

func gitAmendWithMessage(repoPath, message string) error {
	return gitCommitWithMessageArgs(repoPath, message, "commit", "--amend", "--allow-empty")
}

func gitCommitWithMessageArgs(repoPath, message string, args ...string) error {
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
	return runGitAndPrint(repoPath, append(args, "-F", file.Name())...)
}

func Push(message string) error {
	return runPush(message, false)
}

func Commit(message string) error {
	return runCommit(message)
}

func ResumePush() error {
	return runPush("", true)
}

func workspaceUnstagedPaths(workspacePath string) ([]string, error) {
	unstaged, err := git(workspacePath, "diff", "--name-only")
	if err != nil {
		return nil, err
	}
	untracked, err := git(workspacePath, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	var paths []string
	if unstaged != "" {
		paths = append(paths, strings.Split(unstaged, "\n")...)
	}
	if untracked != "" {
		paths = append(paths, strings.Split(untracked, "\n")...)
	}
	return paths, nil
}

func workspaceStagedPaths(workspacePath string) ([]string, error) {
	out, err := git(workspacePath, "diff", "--cached", "--name-only")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

func gitHasStagedChanges(repoPath string) (bool, error) {
	out, err := git(repoPath, "diff", "--cached", "--name-only")
	if err != nil {
		return false, err
	}
	return out != "", nil
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
	cwd, err := os.Getwd()
	if err != nil {
		return "", Manifest{}, State{}, err
	}
	root, err := findProjectRoot(cwd)
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

func findProjectRoot(start string) (string, error) {
	dir := start
	for {
		if _, err := os.Stat(statePath(dir)); err == nil {
			return dir, nil
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
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

func syncStagedWorkspaceToRepo(root string, manifest Manifest, state State, name string, stagedPaths []string) error {
	repo := manifest.Repos[name]
	repoPath := state.Repos[name].Path
	workspacePath := filepath.Join(root, manifest.Workspace)
	for _, stagedPath := range stagedPaths {
		for _, module := range repo.Modules {
			moduleRel, ok := workspacePathInModule(stagedPath, module.To)
			if !ok {
				continue
			}
			dstRel := filepath.Clean(filepath.Join(module.From, filepath.FromSlash(moduleRel)))
			dst := filepath.Join(repoPath, dstRel)
			blob, _, err := gitOutput(workspacePath, "show", ":"+stagedPath)
			if err != nil {
				if err := os.RemoveAll(dst); err != nil {
					return err
				}
			} else {
				mode := os.FileMode(0o644)
				if info, statErr := os.Stat(filepath.Join(workspacePath, filepath.FromSlash(stagedPath))); statErr == nil {
					mode = info.Mode()
				}
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(dst, []byte(blob), mode); err != nil {
					return err
				}
			}
			if _, err := git(repoPath, "add", "-A", "--", dstRel); err != nil {
				return err
			}
			break
		}
	}
	return nil
}

func workspacePathInModule(path, moduleTo string) (string, bool) {
	path = filepath.ToSlash(filepath.Clean(path))
	moduleTo = filepath.ToSlash(filepath.Clean(moduleTo))
	if moduleTo == "." {
		return path, true
	}
	if path == moduleTo {
		return ".", true
	}
	prefix := moduleTo + "/"
	if strings.HasPrefix(path, prefix) {
		return strings.TrimPrefix(path, prefix), true
	}
	return "", false
}

func changedRepos(root string, manifest Manifest, state State) ([]string, error) {
	var changed []string
	workspacePath := filepath.Join(root, manifest.Workspace)
	ignored := map[string]bool{}
	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); err == nil {
		stdout, stderr, err := gitOutput(workspacePath, "ls-files", "--others", "--ignored", "--exclude-standard", "-z")
		if err != nil {
			return nil, fmt.Errorf("list ignored workspace files: %w: %s", err, strings.TrimSpace(stderr))
		}
		for _, path := range strings.Split(stdout, "\x00") {
			if path != "" {
				ignored[filepath.Clean(filepath.Join(workspacePath, filepath.FromSlash(path)))] = true
			}
		}
	}
	for name, repo := range manifest.Repos {
		repoPath := state.Repos[name].Path
		for _, module := range repo.Modules {
			diff, err := dirsDifferIgnoring(filepath.Join(repoPath, module.From), filepath.Join(workspacePath, module.To), ignored)
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
