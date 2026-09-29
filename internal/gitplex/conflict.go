package gitplex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const conflictJournalVersion = 1

type conflictJournal struct {
	Version       int               `json:"version"`
	Operation     string            `json:"operation"`
	Target        string            `json:"target"`
	Order         []string          `json:"order"`
	Current       int               `json:"current"`
	Finalizing    bool              `json:"finalizing,omitempty"`
	OriginalHeads map[string]string `json:"original_heads"`
}

func conflictJournalPath(root string) string { return filepath.Join(root, ".gitplex", "conflict.json") }

func newConflictJournal(operation, target string, order []string, state State) (*conflictJournal, error) {
	journal := &conflictJournal{Version: conflictJournalVersion, Operation: operation, Target: target, Order: append([]string(nil), order...), OriginalHeads: map[string]string{}}
	for _, name := range order {
		head, err := gitHead(state.Repos[name].Path)
		if err != nil {
			return nil, err
		}
		journal.OriginalHeads[name] = head
	}
	return journal, nil
}

func saveConflictJournal(root string, journal *conflictJournal) error {
	data, err := json.MarshalIndent(journal, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(root, ".gitplex"), "conflict-*.json")
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
	return os.Rename(tmpName, conflictJournalPath(root))
}

func loadConflictJournal(root string) (*conflictJournal, error) {
	data, err := os.ReadFile(conflictJournalPath(root))
	if err != nil {
		return nil, err
	}
	var journal conflictJournal
	if err := json.Unmarshal(data, &journal); err != nil {
		return nil, err
	}
	if journal.Version != conflictJournalVersion || journal.Current < 0 || journal.Current >= len(journal.Order) {
		return nil, fmt.Errorf("invalid conflict operation journal")
	}
	return &journal, nil
}

func ensureNoConflictJournal(root string) error {
	if _, err := os.Stat(conflictJournalPath(root)); err == nil {
		return fmt.Errorf("an unfinished conflict workflow exists; use --continue or --abort")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ensureNoPublishJournal(root string) error {
	if _, err := os.Stat(pushJournalPath(root)); err == nil {
		return fmt.Errorf("an unfinished publish exists; run gitplex push or gitplex push --resume before changing repository history")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}

func ContinueConflict(expectedOperation string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	journal, err := loadConflictJournal(root)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no conflict workflow to continue")
		}
		return err
	}
	if journal.Operation != expectedOperation {
		return fmt.Errorf("active workflow is %s, not %s", journal.Operation, expectedOperation)
	}
	if journal.Finalizing {
		return finalizeConflictWorkflow(root, manifest, state, journal)
	}
	name := journal.Order[journal.Current]
	repoPath := state.Repos[name].Path
	active, err := gitOperationInProgress(repoPath, journal.Operation)
	if err != nil {
		return err
	}
	if active {
		if err := runGitAndPrint(repoPath, "-c", "core.editor=true", journal.Operation, "--continue"); err != nil {
			return fmt.Errorf("%s still needs resolution in %s; resolve and stage files, then retry --continue: %w", journal.Operation, name, err)
		}
	} else if journal.Operation == "rebase" {
		if err := fetchBranchForRebase(name, repoPath, journal.Target); err != nil {
			return err
		}
		if err := runGitAndPrint(repoPath, "rebase", "origin/"+journal.Target); err != nil {
			return conflictInstruction(journal.Operation, name, err)
		}
	} else {
		if err := runGitAndPrint(repoPath, "cherry-pick", journal.Target); err != nil {
			return conflictInstruction(journal.Operation, name, err)
		}
	}
	if err := finishConflictRepo(name, &state); err != nil {
		return err
	}
	journal.Current++
	if journal.Operation == "rebase" {
		for journal.Current < len(journal.Order) {
			if err := saveConflictJournal(root, journal); err != nil {
				return err
			}
			name = journal.Order[journal.Current]
			repoPath = state.Repos[name].Path
			if err := fetchBranchForRebase(name, repoPath, journal.Target); err != nil {
				return err
			}
			if err := runGitAndPrint(repoPath, "rebase", "origin/"+journal.Target); err != nil {
				_ = saveConflictJournal(root, journal)
				return conflictInstruction(journal.Operation, name, err)
			}
			if err := finishConflictRepo(name, &state); err != nil {
				return err
			}
			journal.Current++
		}
	}
	return completeConflictWorkflow(root, manifest, state, journal)
}

func completeConflictWorkflow(root string, manifest Manifest, state State, journal *conflictJournal) error {
	journal.Current = len(journal.Order) - 1
	journal.Finalizing = true
	if err := saveConflictJournal(root, journal); err != nil {
		return err
	}
	return finalizeConflictWorkflow(root, manifest, state, journal)
}

func finalizeConflictWorkflow(root string, manifest Manifest, state State, journal *conflictJournal) error {
	for _, name := range journal.Order {
		if err := finishConflictRepo(name, &state); err != nil {
			return err
		}
	}
	return finishConflictWorkflow(root, manifest, state, journal.Operation != "cherry-pick")
}

func gitOperationInProgress(repoPath, operation string) (bool, error) {
	markers := []string{"CHERRY_PICK_HEAD"}
	if operation == "rebase" {
		markers = []string{"rebase-merge", "rebase-apply"}
	}
	for _, marker := range markers {
		path, err := git(repoPath, "rev-parse", "--git-path", marker)
		if err != nil {
			return false, err
		}
		if !filepath.IsAbs(path) {
			path = filepath.Join(repoPath, path)
		}
		if _, err := os.Stat(path); err == nil {
			return true, nil
		} else if !os.IsNotExist(err) {
			return false, err
		}
	}
	return false, nil
}

func AbortConflict(expectedOperation string) error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	journal, err := loadConflictJournal(root)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no conflict workflow to abort")
		}
		return err
	}
	if journal.Operation != expectedOperation {
		return fmt.Errorf("active workflow is %s, not %s", journal.Operation, expectedOperation)
	}
	currentPath := state.Repos[journal.Order[journal.Current]].Path
	_, _ = git(currentPath, journal.Operation, "--abort")
	for _, name := range journal.Order {
		if _, err := git(state.Repos[name].Path, "reset", "--hard", journal.OriginalHeads[name]); err != nil {
			return err
		}
		repoState := state.Repos[name]
		repoState.Head = journal.OriginalHeads[name]
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
	if err := os.Remove(conflictJournalPath(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	fmt.Printf("aborted %s and restored %d repository head(s)\n", journal.Operation, len(journal.Order))
	return nil
}

func finishConflictRepo(name string, state *State) error {
	head, err := gitHead(state.Repos[name].Path)
	if err != nil {
		return err
	}
	repoState := state.Repos[name]
	repoState.Head = head
	state.Repos[name] = repoState
	return nil
}

func finishConflictWorkflow(root string, manifest Manifest, state State, baseline bool) error {
	if err := refreshWorkspace(root, manifest, state); err != nil {
		return err
	}
	if err := generateWorkspaceProjectWithBaseline(root, manifest, state, baseline); err != nil {
		return err
	}
	if err := saveState(root, state); err != nil {
		return err
	}
	if err := os.Remove(conflictJournalPath(root)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func conflictInstruction(operation, name string, err error) error {
	return fmt.Errorf("%s stopped in repo %q: %w; resolve and stage files, then run gitplex %s --continue, or run gitplex %s --abort", operation, name, err, operationCommand(operation), operationCommand(operation))
}

func operationCommand(operation string) string {
	if operation == "cherry-pick" {
		return "cherrypick"
	}
	return operation
}
