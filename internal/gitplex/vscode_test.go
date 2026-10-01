package gitplex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureVSCodeProjectSettingsHidesGitplexDirectory(t *testing.T) {
	root := t.TempDir()
	if err := ensureVSCodeProjectSettings(root); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(root, ".vscode", "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"files.exclude"`, `"search.exclude"`, `"files.watcherExclude"`, `"**/.gitplex"`} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("VS Code settings missing %s:\n%s", want, content)
		}
	}
}

func TestEnsureVSCodeProjectSettingsPreservesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".vscode", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	const existing = "// user-owned settings\n{}\n"
	if err := os.WriteFile(path, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureVSCodeProjectSettings(root); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != existing {
		t.Fatalf("existing VS Code settings were changed:\n%s", content)
	}
}
