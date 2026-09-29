package gitplex

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

func Run(args []string) error {
	if len(args) == 0 {
		printCommandHelp(os.Stdout)
		return nil
	}

	switch args[0] {
	case "which":
		path, jsonOutput, err := parseWhichArgs(args[1:])
		if err != nil {
			return err
		}
		return Which(path, jsonOutput)
	case "init":
		manifest, err := parseInitArgs(args[1:])
		if err != nil {
			return err
		}
		return Init(manifest)
	case "status":
		jsonOutput, err := parseJSONOnlyArgs("status", args[1:])
		if err != nil {
			return err
		}
		if jsonOutput {
			return StatusJSON()
		}
		return Status()
	case "validate":
		if len(args) > 2 {
			return fmt.Errorf("usage: gitplex validate [manifest.yaml]")
		}
		manifestPath := ""
		if len(args) == 2 {
			manifestPath = args[1]
		}
		return Validate(manifestPath)
	case "graph":
		jsonOutput, err := parseJSONOnlyArgs("graph", args[1:])
		if err != nil {
			return err
		}
		return Graph(jsonOutput)
	case "affected":
		seeds, jsonOutput, err := parseNamesAndJSON(args[1:])
		if err != nil {
			return err
		}
		return Affected(seeds, jsonOutput)
	case "diff":
		repo, staged, err := parseDiffArgs(args[1:])
		if err != nil {
			return err
		}
		return Diff(repo, staged)
	case "log":
		repo, limit, jsonOutput, err := parseLogArgs(args[1:])
		if err != nil {
			return err
		}
		return Log(repo, limit, jsonOutput)
	case "doctor":
		return Doctor()
	case "pull":
		return Pull()
	case "stash":
		return Stash(args[1:])
	case "build":
		if err := parseBuildArgs(args[1:]); err != nil {
			return err
		}
		return Build()
	case "true-build":
		if err := parseTrueBuildArgs(args[1:]); err != nil {
			return err
		}
		return TrueBuild()
	case "shell-init":
		if err := parseShellInitArgs(args[1:]); err != nil {
			return err
		}
		return ShellInit()
	case "repo-mode":
		repo, printPath, err := parseRepoModeArgs(args[1:])
		if err != nil {
			return err
		}
		return RepoMode(repo, printPath)
	case "workspace-mode":
		force, err := parseWorkspaceModeArgs(args[1:])
		if err != nil {
			return err
		}
		return WorkspaceMode(force)
	case "rebase":
		repo, branch, action, err := parseRebaseArgs(args[1:])
		if err != nil {
			return err
		}
		if action == "continue" {
			return ContinueConflict("rebase")
		}
		if action == "abort" {
			return AbortConflict("rebase")
		}
		return Rebase(repo, branch)
	case "checkout":
		repo, branch, err := parseCheckoutArgs(args[1:])
		if err != nil {
			return err
		}
		return Checkout(repo, branch)
	case "cherrypick", "cherry-pick":
		repo, commit, action, err := parseCherryPickArgs(args[1:])
		if err != nil {
			return err
		}
		if action == "continue" {
			return ContinueConflict("cherry-pick")
		}
		if action == "abort" {
			return AbortConflict("cherry-pick")
		}
		return CherryPick(repo, commit)
	case "branch":
		branch, err := parseBranchArgs(args[1:])
		if err != nil {
			return err
		}
		return Branch(branch)
	case "amend":
		message, err := parseAmendArgs(args[1:])
		if err != nil {
			return err
		}
		return Amend(message)
	case "push":
		message, resume, dryRun, jsonOutput, err := parsePushOptions(args[1:])
		if err != nil {
			return err
		}
		if dryRun {
			return PushDryRun(jsonOutput)
		}
		if resume {
			return ResumePush()
		}
		return Push(message)
	case "commit":
		message, err := parseOptionalMessageArgs("commit", args[1:])
		if err != nil {
			return err
		}
		if message == "" {
			message = "gitplex sync"
		}
		return Commit(message)
	default:
		return usage(args[0])
	}
}

func parseInitArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: gitplex init <manifest.yaml>")
	}
	return args[0], nil
}

func parseBranchArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: gitplex branch <branch>")
	}
	return args[0], nil
}

func parseBuildArgs(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: gitplex build")
	}
	return nil
}

func parseTrueBuildArgs(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: gitplex true-build")
	}
	return nil
}

func parseShellInitArgs(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: gitplex shell-init")
	}
	return nil
}

func parseRepoModeArgs(args []string) (string, bool, error) {
	var repo string
	printPath := false
	for _, arg := range args {
		switch arg {
		case "--print", "-p":
			printPath = true
		default:
			if repo != "" {
				return "", false, fmt.Errorf("usage: gitplex repo-mode [--print] <repo>")
			}
			repo = arg
		}
	}
	if repo == "" {
		return "", false, fmt.Errorf("usage: gitplex repo-mode [--print] <repo>")
	}
	return repo, printPath, nil
}

func parseWorkspaceModeArgs(args []string) (bool, error) {
	force := false
	for _, arg := range args {
		switch arg {
		case "--force", "-f":
			force = true
		default:
			return false, fmt.Errorf("usage: gitplex workspace-mode [--force]")
		}
	}
	return force, nil
}

func parseJSONOnlyArgs(command string, args []string) (bool, error) {
	if len(args) == 0 {
		return false, nil
	}
	if len(args) == 1 && args[0] == "--json" {
		return true, nil
	}
	return false, fmt.Errorf("usage: gitplex %s [--json]", command)
}

func parseNamesAndJSON(args []string) ([]string, bool, error) {
	var names []string
	jsonOutput := false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if strings.HasPrefix(arg, "-") {
			return nil, false, fmt.Errorf("usage: gitplex affected [repo...] [--json]")
		}
		names = append(names, arg)
	}
	return names, jsonOutput, nil
}

func parseDiffArgs(args []string) (string, bool, error) {
	repo := ""
	staged := false
	for _, arg := range args {
		if arg == "--staged" || arg == "--cached" {
			staged = true
			continue
		}
		if strings.HasPrefix(arg, "-") || repo != "" {
			return "", false, fmt.Errorf("usage: gitplex diff [repo] [--staged]")
		}
		repo = arg
	}
	return repo, staged, nil
}

func parseLogArgs(args []string) (string, int, bool, error) {
	repo, limit, jsonOutput := "", 20, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			jsonOutput = true
		case "--limit", "-n":
			i++
			if i >= len(args) {
				return "", 0, false, fmt.Errorf("--limit requires a value")
			}
			value, err := strconv.Atoi(args[i])
			if err != nil || value <= 0 {
				return "", 0, false, fmt.Errorf("--limit must be a positive integer")
			}
			limit = value
		default:
			if strings.HasPrefix(args[i], "-") || repo != "" {
				return "", 0, false, fmt.Errorf("usage: gitplex log [repo] [--limit N] [--json]")
			}
			repo = args[i]
		}
	}
	return repo, limit, jsonOutput, nil
}

func parseRebaseArgs(args []string) (string, string, string, error) {
	if len(args) == 1 && (args[0] == "--continue" || args[0] == "--abort") {
		return "", "", strings.TrimPrefix(args[0], "--"), nil
	}
	switch len(args) {
	case 1:
		return "", args[0], "", nil
	case 2:
		return args[0], args[1], "", nil
	default:
		return "", "", "", fmt.Errorf("usage: gitplex rebase [repo] <branch> | gitplex rebase --continue | --abort")
	}
}

func parseCheckoutArgs(args []string) (string, string, error) {
	switch len(args) {
	case 1:
		return "", args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("usage: gitplex checkout [repo] <branch>")
	}
}

func parseCherryPickArgs(args []string) (string, string, string, error) {
	if len(args) == 1 && (args[0] == "--continue" || args[0] == "--abort") {
		return "", "", strings.TrimPrefix(args[0], "--"), nil
	}
	if len(args) != 2 {
		return "", "", "", fmt.Errorf("usage: gitplex cherrypick <repo> <commit> | gitplex cherrypick --continue | --abort")
	}
	return args[0], args[1], "", nil
}

func parsePushArgs(args []string) (string, bool, error) {
	message, resume, dryRun, jsonOutput, err := parsePushOptions(args)
	if err != nil {
		return "", false, err
	}
	if dryRun || jsonOutput {
		return "", false, fmt.Errorf("dry-run options require command dispatch")
	}
	return message, resume, nil
}

func parsePushOptions(args []string) (string, bool, bool, bool, error) {
	message := ""
	resume := false
	dryRun := false
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--resume":
			resume = true
		case "--dry-run":
			dryRun = true
		case "--json":
			jsonOutput = true
		case "--message", "-message", "-m":
			i++
			if i >= len(args) {
				return "", false, false, false, fmt.Errorf("%s requires a value", args[i-1])
			}
			message = args[i]
		default:
			return "", false, false, false, fmt.Errorf("usage: gitplex push [--message <message>] [--dry-run [--json]] | gitplex push --resume")
		}
	}
	if resume && message != "" {
		return "", false, false, false, fmt.Errorf("gitplex push --resume reuses the journaled commit message and cannot accept --message")
	}
	if resume && (dryRun || jsonOutput) {
		return "", false, false, false, fmt.Errorf("--resume cannot be combined with --dry-run or --json")
	}
	if jsonOutput && !dryRun {
		return "", false, false, false, fmt.Errorf("--json requires --dry-run")
	}
	if message == "" {
		message = "gitplex sync"
	}
	return message, resume, dryRun, jsonOutput, nil
}

func parseAmendArgs(args []string) (string, error) {
	return parseOptionalMessageArgs("amend", args)
}

func parseOptionalMessageArgs(command string, args []string) (string, error) {
	message := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--message", "-message", "-m":
			i++
			if i >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i-1])
			}
			message = args[i]
		default:
			return "", fmt.Errorf("usage: gitplex %s --message <message>", command)
		}
	}
	return message, nil
}

type commandHelp struct {
	usage       string
	description string
}

var commandHelps = []commandHelp{
	{"gitplex init <manifest.yaml>", "Clone backing repos from the manifest and create the generated workspace."},
	{"gitplex which [--json] <path>", "Show a workspace file's source, generation status, and publishing eligibility."},
	{"gitplex status [--json]", "Show workspace changes, repo branches, divergence, and push readiness."},
	{"gitplex validate [manifest.yaml]", "Strictly validate fields, mappings, paths, dependencies, and cycles."},
	{"gitplex diff [repo] [--staged]", "Show workspace changes grouped by owning repository."},
	{"gitplex log [repo] [-n N] [--json]", "Show a time-ordered log across backing repositories."},
	{"gitplex graph [--json]", "Show dependency edges and topological execution order."},
	{"gitplex affected [repo...] [--json]", "Show directly changed repos and their dependent cascade."},
	{"gitplex doctor", "Run preflight checks for tools, manifest graph, workspace sync, and repo state."},
	{"gitplex pull", "Pull latest changes for backing repos and refresh the generated workspace."},
	{"gitplex stash [git-stash-args...]", "Run git stash inside the generated workspace only."},
	{"gitplex build", "Run the normal workspace build command."},
	{"gitplex true-build", "Run the stricter build command for the generated workspace."},
	{"gitplex shell-init", "Print shell helper functions for easier Gitplex navigation commands."},
	{"gitplex repo-mode [--print] <repo>", "Open a shell in one backing repo, or print its path with --print."},
	{"gitplex workspace-mode [--force]", "Rebuild the generated workspace from existing backing clones and print its path."},
	{"gitplex checkout [repo] <branch>", "Checkout one repo or all repos to a branch, then refresh the workspace."},
	{"gitplex rebase [repo] <branch>", "Rebase repos; conflicts support --continue and --abort."},
	{"gitplex cherrypick <repo> <commit>", "Cherry-pick a commit; conflicts support --continue and --abort."},
	{"gitplex branch <branch>", "Create or reset the same branch in every backing repo for future pushes."},
	{"gitplex amend [--message <message>]", "Rewrite the last commit in backing repos with current workspace changes."},
	{"gitplex commit [--message <message>]", "Prepare local backing-repository commits without publishing them."},
	{"gitplex push [--message <message>]", "Commit and push changes, or publish commits prepared by gitplex commit."},
	{"gitplex push --dry-run [--json]", "Show staged files, target branches, dependency cascade, and execution order."},
	{"gitplex push --resume", "Resume an interrupted or failed publish from its durable operation journal."},
}

func printCommandHelp(w io.Writer) {
	fmt.Fprintln(w, "gitplex creates a generated multi-repo workspace for tightly coupled repositories.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  gitplex <command> [args]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Commands:")
	for _, command := range commandHelps {
		fmt.Fprintf(w, "  %-42s %s\n", command.usage, command.description)
	}
}

func usage(command string) error {
	printCommandHelp(os.Stderr)
	if command != "" {
		return fmt.Errorf("unknown command %q", command)
	}
	return fmt.Errorf("unknown or missing command")
}
