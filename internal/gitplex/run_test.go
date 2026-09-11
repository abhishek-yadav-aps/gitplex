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
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
}
