package gitplex

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type doctorCheck struct {
	level  string
	label  string
	detail string
}

func Doctor() error {
	root, manifest, state, err := loadProject()
	if err != nil {
		return err
	}

	var checks []doctorCheck
	checks = append(checks, checkBinary("git"))
	checks = append(checks, checkBinary("nix"))

	if _, err := topoOrder(manifest); err != nil {
		checks = append(checks, doctorCheck{
			level:  "fail",
			label:  "manifest graph",
			detail: err.Error(),
		})
	} else {
		checks = append(checks, doctorCheck{
			level:  "ok",
			label:  "manifest graph",
			detail: "dependency order is valid",
		})
	}

	workspacePath := filepath.Join(root, manifest.Workspace)
	if info, err := os.Stat(workspacePath); err != nil {
		checks = append(checks, doctorCheck{
			level:  "fail",
			label:  "workspace",
			detail: fmt.Sprintf("%s is missing: %v", workspacePath, err),
		})
	} else if !info.IsDir() {
		checks = append(checks, doctorCheck{
			level:  "fail",
			label:  "workspace",
			detail: fmt.Sprintf("%s is not a directory", workspacePath),
		})
	} else {
		checks = append(checks, doctorCheck{
			level:  "ok",
			label:  "workspace",
			detail: workspacePath,
		})
	}

	changed, err := changedRepos(root, manifest, state)
	if err != nil {
		checks = append(checks, doctorCheck{
			level:  "fail",
			label:  "workspace sync",
			detail: err.Error(),
		})
	} else if len(changed) == 0 {
		checks = append(checks, doctorCheck{
			level:  "ok",
			label:  "workspace sync",
			detail: "workspace matches backing repos",
		})
	} else {
		checks = append(checks, doctorCheck{
			level:  "warn",
			label:  "workspace sync",
			detail: fmt.Sprintf("local changes detected in: %v", changed),
		})
	}

	repoNames := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	for _, name := range repoNames {
		repo := manifest.Repos[name]
		repoState, ok := state.Repos[name]
		if !ok {
			checks = append(checks, doctorCheck{
				level:  "fail",
				label:  name,
				detail: "missing repo state entry",
			})
			continue
		}
		checks = append(checks, inspectRepo(name, repo, repoState, state))
	}

	var warnCount, failCount int
	for _, check := range checks {
		fmt.Printf("[%s] %s: %s\n", check.level, check.label, check.detail)
		switch check.level {
		case "warn":
			warnCount++
		case "fail":
			failCount++
		}
	}

	fmt.Printf("\nsummary: %d ok, %d warn, %d fail\n", len(checks)-warnCount-failCount, warnCount, failCount)
	if failCount > 0 {
		return fmt.Errorf("doctor found %d failing check(s)", failCount)
	}
	return nil
}

func checkBinary(name string) doctorCheck {
	path, err := exec.LookPath(name)
	if err != nil {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: "not found on PATH",
		}
	}
	return doctorCheck{
		level:  "ok",
		label:  name,
		detail: path,
	}
}

func inspectRepo(name string, repo RepoConfig, repoState RepoState, state State) doctorCheck {
	if info, err := os.Stat(repoState.Path); err != nil {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: fmt.Sprintf("repo path missing: %v", err),
		}
	} else if !info.IsDir() {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: "repo path is not a directory",
		}
	}

	if _, err := os.Stat(filepath.Join(repoState.Path, ".git")); err != nil {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: fmt.Sprintf("missing .git directory: %v", err),
		}
	}

	branch, err := git(repoState.Path, "branch", "--show-current")
	if err != nil {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: fmt.Sprintf("cannot read current branch: %v", err),
		}
	}

	dirty, err := gitHasChanges(repoState.Path)
	if err != nil {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: fmt.Sprintf("cannot read repo status: %v", err),
		}
	}

	head, err := gitHead(repoState.Path)
	if err != nil {
		return doctorCheck{
			level:  "fail",
			label:  name,
			detail: fmt.Sprintf("cannot read HEAD: %v", err),
		}
	}

	upstream, upstreamErr := git(repoState.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}")
	expectedBranch := currentBranch(state, name, repo)
	detail := fmt.Sprintf("branch=%s", branch)
	if expectedBranch != "" && branch != expectedBranch {
		detail += fmt.Sprintf(", expected=%s", expectedBranch)
	}
	if upstreamErr == nil && upstream != "" {
		detail += fmt.Sprintf(", upstream=%s", upstream)
	} else {
		detail += ", upstream=missing"
	}
	if dirty {
		detail += ", dirty"
	}
	if repoState.Head != "" && head != repoState.Head {
		detail += ", head changed since last sync"
	}

	level := "ok"
	if expectedBranch != "" && branch != expectedBranch {
		level = "warn"
	}
	if upstreamErr != nil {
		level = "warn"
	}
	if dirty {
		level = "warn"
	}

	return doctorCheck{
		level:  level,
		label:  name,
		detail: detail,
	}
}
