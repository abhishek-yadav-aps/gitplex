package gitplex

import (
	"fmt"
	"os"
)

func Run(args []string) error {
	if len(args) == 0 {
		return usage()
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
	case "pull":
		return Pull()
	case "branch":
		branch, err := parseBranchArgs(args[1:])
		if err != nil {
			return err
		}
		return Branch(branch)
	case "push":
		message, err := parsePushArgs(args[1:])
		if err != nil {
			return err
		}
		return Push(message)
	default:
		return usage()
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

func parsePushArgs(args []string) (string, error) {
	message := "gitplex sync"
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--message", "-message", "-m":
			i++
			if i >= len(args) {
				return "", fmt.Errorf("%s requires a value", args[i-1])
			}
			message = args[i]
		default:
			return "", fmt.Errorf("usage: gitplex push --message <message>")
		}
	}
	return message, nil
}

func usage() error {
	fmt.Fprintln(os.Stderr, "usage: gitplex <init|status|pull|branch|push>")
	return fmt.Errorf("unknown or missing command")
}
