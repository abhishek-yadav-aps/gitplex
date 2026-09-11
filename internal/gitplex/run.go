package gitplex

import (
	"fmt"
	"io"
	"os"
)

func Run(args []string) error {
	if len(args) == 0 {
		printCommandHelp(os.Stdout)
		return nil
	}

	switch args[0] {
	case "init":
		manifest, err := parseInitArgs(args[1:])
		if err != nil {
			return err
		}
		return Init(manifest)
	case "status":
		return Status()
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
		repo, err := parseRepoModeArgs(args[1:])
		if err != nil {
			return err
		}
		return RepoMode(repo)
	case "workspace-mode":
		force, err := parseWorkspaceModeArgs(args[1:])
		if err != nil {
			return err
		}
		return WorkspaceMode(force)
	case "rebase":
		repo, branch, err := parseRebaseArgs(args[1:])
		if err != nil {
			return err
		}
		return Rebase(repo, branch)
	case "checkout":
		repo, branch, err := parseCheckoutArgs(args[1:])
		if err != nil {
			return err
		}
		return Checkout(repo, branch)
	case "cherrypick", "cherry-pick":
		repo, commit, err := parseCherryPickArgs(args[1:])
		if err != nil {
			return err
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
		message, err := parsePushArgs(args[1:])
		if err != nil {
			return err
		}
		return Push(message)
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

func parseRepoModeArgs(args []string) (string, error) {
	if len(args) != 1 {
		return "", fmt.Errorf("usage: gitplex repo-mode <repo>")
	}
	return args[0], nil
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

func parseRebaseArgs(args []string) (string, string, error) {
	switch len(args) {
	case 1:
		return "", args[0], nil
	case 2:
		return args[0], args[1], nil
	default:
		return "", "", fmt.Errorf("usage: gitplex rebase [repo] <branch>")
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

func parseCherryPickArgs(args []string) (string, string, error) {
	if len(args) != 2 {
		return "", "", fmt.Errorf("usage: gitplex cherrypick <repo> <commit>")
	}
	return args[0], args[1], nil
}

func parsePushArgs(args []string) (string, error) {
	message, err := parseOptionalMessageArgs("push", args)
	if err != nil {
		return "", err
	}
	if message == "" {
		message = "gitplex sync"
	}
	return message, nil
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
	{"gitplex status", "Show workspace changes, repo branches, dirty backing repos, and push readiness."},
	{"gitplex doctor", "Run preflight checks for tools, manifest graph, workspace sync, and repo state."},
	{"gitplex pull", "Pull latest changes for backing repos and refresh the generated workspace."},
	{"gitplex stash [git-stash-args...]", "Run git stash inside the generated workspace only."},
	{"gitplex build", "Run the normal workspace build command."},
	{"gitplex true-build", "Run the stricter build command for the generated workspace."},
	{"gitplex shell-init", "Print shell helper functions for easier Gitplex navigation commands."},
	{"gitplex repo-mode <repo>", "Print the backing clone path for one repo under .gitplex/repos."},
	{"gitplex workspace-mode [--force]", "Rebuild the generated workspace from existing backing clones and print its path."},
	{"gitplex checkout [repo] <branch>", "Checkout one repo or all repos to a branch, then refresh the workspace."},
	{"gitplex rebase [repo] <branch>", "Rebase one repo or all repos onto origin/<branch>, then refresh the workspace."},
	{"gitplex cherrypick <repo> <commit>", "Cherry-pick one commit into a backing repo and refresh the workspace."},
	{"gitplex branch <branch>", "Create or reset the same branch in every backing repo for future pushes."},
	{"gitplex amend [--message <message>]", "Rewrite the last commit in backing repos with current workspace changes."},
	{"gitplex push [--message <message>]", "Commit and push changed backing repos in dependency order, updating downstream Nix inputs."},
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
