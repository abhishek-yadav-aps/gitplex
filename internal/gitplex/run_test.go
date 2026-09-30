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
		"gitplex install-extensions",
		"gitplex update",
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

func TestParseUpdateArgs(t *testing.T) {
	if err := parseUpdateArgs(nil); err != nil {
		t.Fatalf("parse update: %v", err)
	}
	if err := parseUpdateArgs([]string{"unexpected"}); err == nil {
		t.Fatal("parse update with arguments succeeded")
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
