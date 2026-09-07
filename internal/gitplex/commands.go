package gitplex

import (
	"fmt"
	"os"
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
		}
		if repo.Ref != "" {
			if _, err := git(repoPath, "checkout", repo.Ref); err != nil {
				return err
			}
		}
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		state.Repos[name] = RepoState{URL: repo.URL, Path: repoPath, Head: head}
	}

	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProject(root, manifest, state); err != nil {
		return err
	}
	return saveState(root, state)
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
	branch := state.Branch
	if branch == "" {
		branch = "(manifest refs)"
	}

	fmt.Printf("workspace: %s\n", workspacePath)
	fmt.Printf("branch: %s\n", branch)
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
			expectedBranch:  currentBranch(state, repo),
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
		state.Repos[name] = repoState
		fmt.Printf("branched %s -> %s\n", name, branch)
	}

	state.Branch = branch
	return saveState(root, state)
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
		if currentBranch(state, repo) == "" {
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
				depRef := currentBranch(state, manifest.Repos[depName])
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
		if err := runGitAndPrint(repoPath, "push", "-u", "origin", currentBranch(state, repo)); err != nil {
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

func currentBranch(state State, repo RepoConfig) string {
	if state.Branch != "" {
		return state.Branch
	}
	return repo.Ref
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
