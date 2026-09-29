package gitplex

import (
	"bytes"
	"strings"
	"testing"
)

func TestPrintCommandHelpIncludesCommandDescriptions(t *testing.T) {
	var out bytes.Buffer
	printCommandHelp(&out)
	text := out.String()

	for _, want := range []string{
		"gitplex creates a generated multi-repo workspace",
		"gitplex init <manifest.yaml>",
		"gitplex status",
		"gitplex doctor",
		"gitplex shell-init",
		"gitplex workspace-mode [--force]",
		"gitplex push [--message <message>]",
		"gitplex push --resume",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
}

func TestParsePushArgsResume(t *testing.T) {
	message, resume, err := parsePushArgs([]string{"--resume"})
	if err != nil {
		t.Fatalf("parse resume: %v", err)
	}
	if !resume || message != "gitplex sync" {
		t.Fatalf("parse resume = %q, %v", message, resume)
	}
	if _, _, err := parsePushArgs([]string{"--resume", "--message", "different"}); err == nil {
		t.Fatal("resume with message succeeded, want error")
	}
}
