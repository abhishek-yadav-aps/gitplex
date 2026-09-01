package gitplex

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func git(dir string, args ...string) (string, error) {
	stdout, stderr, err := gitOutput(dir, args...)
	if err != nil {
		msg := strings.TrimSpace(stderr)
		if msg == "" {
			msg = strings.TrimSpace(stdout)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return strings.TrimSpace(stdout), nil
}

func gitOutput(dir string, args ...string) (string, string, error) {
	return commandOutput(dir, "git", args...)
}

func commandOutput(dir, name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func commandStreaming(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func gitHasChanges(dir string) (bool, error) {
	out, err := git(dir, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

func gitHead(dir string) (string, error) {
	return git(dir, "rev-parse", "HEAD")
}
