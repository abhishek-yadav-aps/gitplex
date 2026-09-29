package gitplex

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	pushJournalVersion = 1
	pushPhasePlanned   = "planned"
	pushPhaseCommitted = "committed"
	pushPhasePushed    = "pushed"
	pushPhaseFailed    = "failed"
	pushPhaseSkipped   = "skipped"
)

type pushJournal struct {
	Version       int                        `json:"version"`
	Message       string                     `json:"message"`
	WorkspaceTree string                     `json:"workspace_tree"`
	StagedPaths   []string                   `json:"staged_paths"`
	Order         []string                   `json:"order"`
	Repos         map[string]pushJournalRepo `json:"repos"`
}

type pushJournalRepo struct {
	Branch       string `json:"branch"`
	OriginalHead string `json:"original_head"`
	Phase        string `json:"phase"`
	Commit       string `json:"commit,omitempty"`
	Error        string `json:"error,omitempty"`
}

func pushJournalPath(root string) string {
	return filepath.Join(root, ".gitplex", "push.json")
}

func pushLockPath(root string) string {
	return filepath.Join(root, ".gitplex", "push.lock")
}

func loadPushJournal(root string) (*pushJournal, error) {
	data, err := os.ReadFile(pushJournalPath(root))
	if err != nil {
		return nil, err
	}
	var journal pushJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, fmt.Errorf("read push journal: %w", err)
	}
	if journal.Version != pushJournalVersion {
		return nil, fmt.Errorf("unsupported push journal version %d", journal.Version)
	}
	if journal.Repos == nil {
		return nil, fmt.Errorf("push journal has no repository state")
	}
	return &journal, nil
}

func savePushJournal(root string, journal *pushJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(root, ".gitplex"), "push-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, pushJournalPath(root))
}

func acquirePushLock(root string) (func(), error) {
	path := pushLockPath(root)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			owner, _ := os.ReadFile(path)
			pidText := strings.TrimSpace(string(owner))
			pid, parseErr := strconv.Atoi(pidText)
			if parseErr == nil && !processIsAlive(pid) {
				if removeErr := os.Remove(path); removeErr != nil {
					return nil, fmt.Errorf("remove stale push lock %s: %w", path, removeErr)
				}
				return acquirePushLock(root)
			}
			return nil, fmt.Errorf("another gitplex push is active (lock %s, pid %s)", path, pidText)
		}
		return nil, err
	}
	if _, err := file.WriteString(strconv.Itoa(os.Getpid()) + "\n"); err != nil {
		file.Close()
		os.Remove(path)
		return nil, err
	}
	if err := file.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	return func() { _ = os.Remove(path) }, nil
}

func processIsAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func runPush(message string, resume bool) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	releaseLock, err := acquirePushLock(root)
	if err != nil {
		return err
	}
	defer releaseLock()

	workspacePath := filepath.Join(root, manifest.Workspace)
	var journal *pushJournal
	if resume {
		journal, err = loadPushJournal(root)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("no unfinished push to resume")
			}
			return err
		}
		if err := validatePushJournal(manifest, journal); err != nil {
			return err
		}
		if pushJournalHasPlannedRepos(journal) {
			workspaceTree, err := git(workspacePath, "write-tree")
			if err != nil {
				return err
			}
			if workspaceTree != journal.WorkspaceTree {
				return fmt.Errorf("workspace index changed since the failed push; restore the staged snapshot before resuming")
			}
		}
		fmt.Printf("resuming push with journal %s\n", pushJournalPath(root))
	} else {
		if _, err := os.Stat(pushJournalPath(root)); err == nil {
			return fmt.Errorf("an unfinished push exists; run gitplex push --resume")
		} else if !os.IsNotExist(err) {
			return err
		}
		stagedPaths, err := workspaceStagedPaths(workspacePath)
		if err != nil {
			return err
		}
		workspaceTree, err := git(workspacePath, "write-tree")
		if err != nil {
			return err
		}
		order, err := topoOrder(manifest)
		if err != nil {
			return err
		}
		journal, err = newPushJournal(manifest, state, message, workspaceTree, stagedPaths, order)
		if err != nil {
			return err
		}
		if err := validatePushPreflight(manifest, state, journal); err != nil {
			return err
		}
		if err := savePushJournal(root, journal); err != nil {
			return err
		}
	}

	publishedHeads := map[string]string{}
	for _, name := range journal.Order {
		repoJournal := journal.Repos[name]
		if repoJournal.Phase == pushPhaseSkipped {
			continue
		}
		if repoJournal.Phase == pushPhasePushed {
			verified, err := remoteBranchAtCommit(state.Repos[name].Path, repoJournal.Branch, repoJournal.Commit)
			if err != nil {
				return err
			}
			if verified {
				publishedHeads[name] = repoJournal.Commit
				continue
			}
			repoJournal.Phase = pushPhaseFailed
			repoJournal.Error = "remote branch no longer points at the journaled commit"
			journal.Repos[name] = repoJournal
			if err := savePushJournal(root, journal); err != nil {
				return err
			}
		}

		fmt.Printf("\n== %s ==\n", name)
		if repoJournal.Commit == "" {
			if resume {
				commit, found, err := recoverCommitBeforeJournalSave(state.Repos[name].Path, repoJournal, journal.Message)
				if err != nil {
					return err
				}
				if found {
					repoJournal.Commit = commit
					repoJournal.Phase = pushPhaseCommitted
					repoJournal.Error = ""
					journal.Repos[name] = repoJournal
					if err := savePushJournal(root, journal); err != nil {
						return err
					}
				}
			}
		}
		if repoJournal.Commit == "" {
			repoJournal.Phase = pushPhasePlanned
			repoJournal.Error = ""
			journal.Repos[name] = repoJournal
			commit, skipped, err := preparePushCommit(root, manifest, state, journal, name, publishedHeads)
			if err != nil {
				if restoreErr := restorePushRepo(state.Repos[name].Path, repoJournal.OriginalHead); restoreErr != nil {
					err = fmt.Errorf("%v; restore backing repo: %w", err, restoreErr)
				}
				return failPushRepo(root, journal, name, "", err)
			}
			if skipped {
				repoJournal.Phase = pushPhaseSkipped
				journal.Repos[name] = repoJournal
				if err := savePushJournal(root, journal); err != nil {
					return err
				}
				fmt.Println("no changes to push; skipping")
				continue
			}
			repoJournal.Commit = commit
			repoJournal.Phase = pushPhaseCommitted
			journal.Repos[name] = repoJournal
			if err := savePushJournal(root, journal); err != nil {
				return err
			}
		}

		if err := publishJournaledCommit(state.Repos[name].Path, repoJournal.Branch, repoJournal.Commit); err != nil {
			return failPushRepo(root, journal, name, repoJournal.Commit, err)
		}
		repoJournal.Phase = pushPhasePushed
		repoJournal.Error = ""
		journal.Repos[name] = repoJournal
		publishedHeads[name] = repoJournal.Commit
		repoState := state.Repos[name]
		repoState.Head = repoJournal.Commit
		state.Repos[name] = repoState
		if err := saveState(root, state); err != nil {
			return err
		}
		if err := savePushJournal(root, journal); err != nil {
			return err
		}
	}

	unstagedPaths, err := workspaceUnstagedPaths(workspacePath)
	if err != nil {
		return err
	}
	if len(unstagedPaths) == 0 {
		if err := refreshWorkspace(root, manifest, state); err != nil {
			return err
		}
		if err := generateWorkspaceProject(root, manifest, state); err != nil {
			return err
		}
	} else {
		fmt.Printf("workspace has unstaged changes in %v; skipped workspace refresh to preserve them\n", unstagedPaths)
	}
	if err := saveState(root, state); err != nil {
		return err
	}
	if err := os.Remove(pushJournalPath(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func newPushJournal(manifest Manifest, state State, message, workspaceTree string, stagedPaths, order []string) (*pushJournal, error) {
	journal := &pushJournal{
		Version:       pushJournalVersion,
		Message:       message,
		WorkspaceTree: workspaceTree,
		StagedPaths:   append([]string(nil), stagedPaths...),
		Order:         append([]string(nil), order...),
		Repos:         make(map[string]pushJournalRepo, len(order)),
	}
	for _, name := range order {
		repo := manifest.Repos[name]
		branch := currentBranch(state, name, repo)
		if branch == "" {
			return nil, fmt.Errorf("repo %q has no push branch; run gitplex branch <branch> or set repo ref in manifest", name)
		}
		head, err := gitHead(state.Repos[name].Path)
		if err != nil {
			return nil, err
		}
		journal.Repos[name] = pushJournalRepo{Branch: branch, OriginalHead: head, Phase: pushPhasePlanned}
	}
	return journal, nil
}

func validatePushPreflight(manifest Manifest, state State, journal *pushJournal) error {
	for _, name := range journal.Order {
		repoPath := state.Repos[name].Path
		head, err := gitHead(repoPath)
		if err != nil {
			return err
		}
		if state.Repos[name].Head != "" && head != state.Repos[name].Head {
			return fmt.Errorf("repo %q HEAD changed outside Gitplex state; synchronize or restore it before push", name)
		}
		actualBranch, err := git(repoPath, "branch", "--show-current")
		if err != nil {
			return err
		}
		expectedBranch := journal.Repos[name].Branch
		if actualBranch != expectedBranch {
			return fmt.Errorf("repo %q is on branch %q; expected %q", name, actualBranch, expectedBranch)
		}
		dirty, err := gitHasChanges(repoPath)
		if err != nil {
			return err
		}
		if dirty {
			return fmt.Errorf("repo %q has pre-existing changes; commit, stash, or discard them before push", name)
		}
		upstream, upstreamErr := git(repoPath, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
		if upstreamErr == nil && upstream != "" {
			_, behind, err := gitAheadBehind(repoPath)
			if err != nil {
				return err
			}
			if behind > 0 {
				return fmt.Errorf("repo %q is behind %s by %d commit(s); update it before push", name, upstream, behind)
			}
		}
		if _, ok := manifest.Repos[name]; !ok {
			return fmt.Errorf("push journal references unknown repo %q", name)
		}
	}
	return nil
}

func validatePushJournal(manifest Manifest, journal *pushJournal) error {
	if len(journal.Order) != len(manifest.Repos) {
		return fmt.Errorf("manifest repository set changed since the push started")
	}
	for _, name := range journal.Order {
		if _, ok := manifest.Repos[name]; !ok {
			return fmt.Errorf("manifest no longer contains journaled repo %q", name)
		}
		if _, ok := journal.Repos[name]; !ok {
			return fmt.Errorf("push journal is missing repo %q", name)
		}
	}
	return nil
}

func pushJournalHasPlannedRepos(journal *pushJournal) bool {
	for _, repo := range journal.Repos {
		if repo.Phase == pushPhasePlanned || (repo.Phase == pushPhaseFailed && repo.Commit == "") {
			return true
		}
	}
	return false
}

func preparePushCommit(root string, manifest Manifest, state State, journal *pushJournal, name string, publishedHeads map[string]string) (string, bool, error) {
	repo := manifest.Repos[name]
	repoPath := state.Repos[name].Path
	if err := syncStagedWorkspaceToRepo(root, manifest, state, name, journal.StagedPaths); err != nil {
		return "", false, err
	}
	var updatedFlakeInputs []string
	for depName, depConfig := range repo.Dependencies {
		if head := publishedHeads[depName]; head != "" {
			depRef := journal.Repos[depName].Branch
			if err := updateFlakeInput(repoPath, depConfig.FlakeInput, depRef, head); err != nil {
				return "", false, err
			}
			updatedFlakeInputs = append(updatedFlakeInputs, depConfig.FlakeInput)
		}
	}
	for _, flakeInput := range updatedFlakeInputs {
		if err := runCommandAndPrint(repoPath, "nix", "flake", "lock", "--update-input", flakeInput); err != nil {
			return "", false, err
		}
	}
	if len(updatedFlakeInputs) > 0 {
		if _, err := git(repoPath, "add", "-A", "--", "flake.nix", "flake.lock"); err != nil {
			return "", false, err
		}
	}
	staged, err := gitHasStagedChanges(repoPath)
	if err != nil {
		return "", false, err
	}
	if !staged {
		return "", true, nil
	}
	if err := runGitAndPrint(repoPath, "commit", "-m", journal.Message); err != nil {
		return "", false, err
	}
	head, err := gitHead(repoPath)
	return head, false, err
}

func publishJournaledCommit(repoPath, branch, commit string) error {
	head, err := gitHead(repoPath)
	if err != nil {
		return err
	}
	if head != commit {
		return fmt.Errorf("local HEAD %s does not match journaled commit %s", head, commit)
	}
	verified, err := remoteBranchAtCommit(repoPath, branch, commit)
	if err != nil {
		return err
	}
	if !verified {
		pushErr := runGitAndPrint(repoPath, "push", "-u", "origin", branch)
		verified, verifyErr := remoteBranchAtCommit(repoPath, branch, commit)
		if verifyErr != nil {
			return verifyErr
		}
		if !verified {
			if pushErr != nil {
				return pushErr
			}
			return fmt.Errorf("remote branch %q did not resolve to pushed commit %s", branch, commit)
		}
	}
	return nil
}

func remoteBranchAtCommit(repoPath, branch, commit string) (bool, error) {
	out, err := git(repoPath, "ls-remote", "--heads", "origin", "refs/heads/"+branch)
	if err != nil {
		return false, err
	}
	if out == "" {
		return false, nil
	}
	fields := strings.Fields(out)
	return len(fields) >= 2 && fields[0] == commit && fields[1] == "refs/heads/"+branch, nil
}

func failPushRepo(root string, journal *pushJournal, name, commit string, pushErr error) error {
	repo := journal.Repos[name]
	repo.Phase = pushPhaseFailed
	repo.Commit = commit
	repo.Error = pushErr.Error()
	journal.Repos[name] = repo
	if err := savePushJournal(root, journal); err != nil {
		return fmt.Errorf("push failed for %s: %v; additionally failed to save journal: %w", name, pushErr, err)
	}
	return fmt.Errorf("push failed for %s: %w; retry with gitplex push --resume", name, pushErr)
}

func restorePushRepo(repoPath, originalHead string) error {
	_, err := git(repoPath, "reset", "--hard", originalHead)
	return err
}

func recoverCommitBeforeJournalSave(repoPath string, repo pushJournalRepo, message string) (string, bool, error) {
	head, err := gitHead(repoPath)
	if err != nil {
		return "", false, err
	}
	if head == repo.OriginalHead {
		return "", false, nil
	}
	dirty, err := gitHasChanges(repoPath)
	if err != nil {
		return "", false, err
	}
	count, err := git(repoPath, "rev-list", "--count", repo.OriginalHead+".."+head)
	if err != nil {
		return "", false, err
	}
	commitMessage, err := git(repoPath, "log", "-1", "--pretty=%B")
	if err != nil {
		return "", false, err
	}
	if dirty || count != "1" || strings.TrimSpace(commitMessage) != strings.TrimSpace(message) {
		return "", false, fmt.Errorf("repo changed after the push journal was written; expected one clean commit with the journaled message")
	}
	return head, true, nil
}

func gitAheadBehind(repoPath string) (int, int, error) {
	out, err := git(repoPath, "rev-list", "--left-right", "--count", "HEAD...@{u}")
	if err != nil {
		return 0, 0, err
	}
	var ahead, behind int
	if _, err := fmt.Sscanf(out, "%d %d", &ahead, &behind); err != nil {
		return 0, 0, fmt.Errorf("parse ahead/behind %q: %w", out, err)
	}
	return ahead, behind, nil
}
