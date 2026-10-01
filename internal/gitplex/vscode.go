package gitplex

import (
	"fmt"
	"os"
	"path/filepath"
)

const gitplexVSCodeSettings = `{
  "files.exclude": {
    "**/.gitplex": true
  },
  "search.exclude": {
    "**/.gitplex": true
  },
  "files.watcherExclude": {
    "**/.gitplex/**": true
  }
}
`

func ensureVSCodeProjectSettings(root string) error {
	path := filepath.Join(root, ".vscode", "settings.json")
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create VS Code settings directory: %w", err)
	}
	if err := os.WriteFile(path, []byte(gitplexVSCodeSettings), 0o644); err != nil {
		return fmt.Errorf("write VS Code settings: %w", err)
	}
	return nil
}
