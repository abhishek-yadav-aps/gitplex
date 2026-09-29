package gitplex

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func Validate(manifestPath string) error {
	if manifestPath == "" {
		_, manifest, _, err := loadProject()
		if err != nil {
			return err
		}
		order, _ := topoOrder(manifest)
		fmt.Printf("valid: %d repositories\nexecution order: %s\n", len(manifest.Repos), strings.Join(order, " -> "))
		return nil
	}
	manifest, err := loadManifest(manifestPath)
	if err != nil {
		return err
	}
	order, _ := topoOrder(manifest)
	fmt.Printf("valid: %d repositories\nexecution order: %s\n", len(manifest.Repos), strings.Join(order, " -> "))
	return nil
}

func Graph(jsonOutput bool) error {
	_, manifest, _, err := loadProject()
	if err != nil {
		return err
	}
	order, err := topoOrder(manifest)
	if err != nil {
		return err
	}
	type edge struct {
		From string `json:"from"`
		To   string `json:"to"`
	}
	edges := make([]edge, 0)
	for _, name := range order {
		deps := sortedDependencyNames(manifest.Repos[name])
		for _, dep := range deps {
			edges = append(edges, edge{From: dep, To: name})
		}
	}
	if jsonOutput {
		return writeJSON(map[string]any{"order": order, "edges": edges})
	}
	fmt.Printf("execution order: %s\n", strings.Join(order, " -> "))
	if len(edges) == 0 {
		fmt.Println("dependencies: none")
		return nil
	}
	fmt.Println("dependencies:")
	for _, edge := range edges {
		fmt.Printf("  %s -> %s\n", edge.From, edge.To)
	}
	return nil
}

func Affected(seeds []string, jsonOutput bool) error {
	root, manifest, _, err := loadProject()
	if err != nil {
		return err
	}
	if len(seeds) == 0 {
		paths, err := workspaceStagedPaths(filepath.Join(root, manifest.Workspace))
		if err != nil {
			return err
		}
		seeds = reposForWorkspacePaths(manifest, paths)
	}
	for _, seed := range seeds {
		if _, ok := manifest.Repos[seed]; !ok {
			return fmt.Errorf("repo %q does not exist in manifest", seed)
		}
	}
	direct := stringSet(seeds)
	affected := stringSet(seeds)
	changed := true
	for changed {
		changed = false
		for name, repo := range manifest.Repos {
			if affected[name] {
				continue
			}
			for dep := range repo.Dependencies {
				if affected[dep] {
					affected[name] = true
					changed = true
					break
				}
			}
		}
	}
	order, err := topoOrder(manifest)
	if err != nil {
		return err
	}
	result, cascade := make([]string, 0), make([]string, 0)
	for _, name := range order {
		if affected[name] {
			result = append(result, name)
			if !direct[name] {
				cascade = append(cascade, name)
			}
		}
	}
	if jsonOutput {
		return writeJSON(map[string]any{"direct": orderedSubset(order, direct), "cascade": cascade, "order": result})
	}
	fmt.Printf("direct: %s\n", displayList(orderedSubset(order, direct)))
	fmt.Printf("dependency cascade: %s\n", displayList(cascade))
	fmt.Printf("execution order: %s\n", displayList(result))
	return nil
}

func Diff(repoName string, staged bool) error {
	root, manifest, _, err := loadProject()
	if err != nil {
		return err
	}
	names, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return err
	}
	workspace := filepath.Join(root, manifest.Workspace)
	for _, name := range names {
		args := []string{"diff"}
		if staged {
			args = append(args, "--cached")
		}
		args = append(args, "--")
		for _, module := range manifest.Repos[name].Modules {
			args = append(args, module.To)
		}
		out, stderr, err := gitOutput(workspace, args...)
		if err != nil {
			return fmt.Errorf("git diff for %s: %w: %s", name, err, strings.TrimSpace(stderr))
		}
		if out != "" {
			fmt.Printf("== %s ==\n%s", name, out)
			if !strings.HasSuffix(out, "\n") {
				fmt.Println()
			}
		}
	}
	return nil
}

type logEntry struct {
	Timestamp int64  `json:"timestamp"`
	Date      string `json:"date"`
	Repo      string `json:"repo"`
	Hash      string `json:"hash"`
	Author    string `json:"author"`
	Subject   string `json:"subject"`
}

func Log(repoName string, limit int, jsonOutput bool) error {
	_, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	names, err := selectedRepoNames(manifest, repoName)
	if err != nil {
		return err
	}
	entries := make([]logEntry, 0)
	for _, name := range names {
		out, err := git(state.Repos[name].Path, "log", "-n", strconv.Itoa(limit), "--pretty=%ct%x1f%h%x1f%an%x1f%s")
		if err != nil {
			return err
		}
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "\x1f", 4)
			if len(parts) != 4 {
				continue
			}
			ts, _ := strconv.ParseInt(parts[0], 10, 64)
			entries = append(entries, logEntry{Timestamp: ts, Date: time.Unix(ts, 0).UTC().Format(time.RFC3339), Repo: name, Hash: parts[1], Author: parts[2], Subject: parts[3]})
		}
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Timestamp > entries[j].Timestamp })
	if len(entries) > limit {
		entries = entries[:limit]
	}
	if jsonOutput {
		return writeJSON(entries)
	}
	for _, entry := range entries {
		fmt.Printf("%s %-12s %-10s %s (%s)\n", entry.Date, entry.Repo, entry.Hash, entry.Subject, entry.Author)
	}
	return nil
}

func StatusJSON() error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}
	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		return err
	}
	sort.Strings(changed)
	if changed == nil {
		changed = []string{}
	}
	changedSet := stringSet(changed)
	journal, journalErr := loadPushJournal(root)
	if journalErr != nil && !os.IsNotExist(journalErr) {
		return journalErr
	}
	conflict, conflictErr := loadConflictJournal(root)
	if conflictErr != nil && !os.IsNotExist(conflictErr) {
		return conflictErr
	}
	type jsonRepoStatus struct {
		Name            string `json:"name"`
		Branch          string `json:"branch"`
		ExpectedBranch  string `json:"expected_branch"`
		Upstream        string `json:"upstream,omitempty"`
		PublishPhase    string `json:"publish_phase,omitempty"`
		WorkspaceDirty  bool   `json:"workspace_dirty"`
		RepoDirty       bool   `json:"repo_dirty"`
		UpstreamMissing bool   `json:"upstream_missing"`
		HeadChanged     bool   `json:"head_changed"`
		Ahead           int    `json:"ahead"`
		Behind          int    `json:"behind"`
	}
	names, _ := selectedRepoNames(manifest, "")
	outputRepos := make([]jsonRepoStatus, 0, len(names))
	var statuses []repoStatus
	for _, name := range names {
		repoState := state.Repos[name]
		branch, err := git(repoState.Path, "branch", "--show-current")
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
		status := repoStatus{name: name, branch: branch, expectedBranch: currentBranch(state, name, manifest.Repos[name]), workspaceDirty: changedSet[name], repoDirty: dirty, upstream: upstream, upstreamMissing: upstreamErr != nil || upstream == "", ahead: ahead, behind: behind, headChanged: repoState.Head != "" && head != repoState.Head}
		if journal != nil {
			if jr, ok := journal.Repos[name]; ok {
				status.publishPhase = jr.Phase
			}
		}
		statuses = append(statuses, status)
		outputRepos = append(outputRepos, jsonRepoStatus{Name: name, Branch: branch, ExpectedBranch: status.expectedBranch, WorkspaceDirty: status.workspaceDirty, RepoDirty: dirty, Upstream: upstream, UpstreamMissing: status.upstreamMissing, Ahead: ahead, Behind: behind, HeadChanged: status.headChanged, PublishPhase: status.publishPhase})
	}
	summary := overallStatusSummary(state, statuses, journal != nil)
	var conflictOutput any
	if conflict != nil {
		conflictOutput = map[string]any{"operation": conflict.Operation, "repo": conflict.Order[conflict.Current], "target": conflict.Target, "finalizing": conflict.Finalizing}
		summary = fmt.Sprintf("unfinished %s in %s", conflict.Operation, conflict.Order[conflict.Current])
	}
	return writeJSON(map[string]any{"workspace": filepath.Join(root, manifest.Workspace), "branch": branchDisplay(state), "workspace_changed_repos": changed, "unfinished_push": journal != nil, "conflict": conflictOutput, "summary": summary, "repos": outputRepos})
}

func writeJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func sortedDependencyNames(repo RepoConfig) []string {
	result := make([]string, 0, len(repo.Dependencies))
	for name := range repo.Dependencies {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func reposForWorkspacePaths(manifest Manifest, paths []string) []string {
	set := map[string]bool{}
	for name, repo := range manifest.Repos {
		for _, path := range paths {
			for _, module := range repo.Modules {
				if _, ok := workspacePathInModule(path, module.To); ok {
					set[name] = true
				}
			}
		}
	}
	result := make([]string, 0, len(set))
	for name := range set {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func stringSet(values []string) map[string]bool {
	set := make(map[string]bool, len(values))
	for _, value := range values {
		set[value] = true
	}
	return set
}

func orderedSubset(order []string, set map[string]bool) []string {
	result := make([]string, 0)
	for _, name := range order {
		if set[name] {
			result = append(result, name)
		}
	}
	return result
}

func displayList(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, " -> ")
}
