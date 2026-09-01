package gitplex

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func updateFlakeInput(repoPath, inputName, ref, rev string) error {
	if inputName == "" {
		return nil
	}
	flakePath := filepath.Join(repoPath, "flake.nix")
	data, err := os.ReadFile(flakePath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	start := -1
	pattern := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(inputName) + `\s*=\s*\{\s*$`)
	for i, line := range lines {
		if pattern.MatchString(line) {
			start = i
			break
		}
	}
	if start == -1 {
		return fmt.Errorf("flake input %q not found in %s", inputName, flakePath)
	}

	end := -1
	depth := 0
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{")
		depth -= strings.Count(lines[i], "}")
		if i > start && depth == 0 {
			end = i
			break
		}
	}
	if end == -1 {
		return fmt.Errorf("could not find end of flake input %q", inputName)
	}

	hasRef := false
	hasRev := false
	for i := start + 1; i < end; i++ {
		trimmed := strings.TrimSpace(lines[i])
		indent := leadingWhitespace(lines[i])
		switch {
		case strings.HasPrefix(trimmed, "ref ="):
			lines[i] = fmt.Sprintf(`%sref = "%s";`, indent, ref)
			hasRef = true
		case strings.HasPrefix(trimmed, "rev ="):
			lines[i] = fmt.Sprintf(`%srev = "%s";`, indent, rev)
			hasRev = true
		}
	}

	insert := []string{}
	indent := leadingWhitespace(lines[start]) + "  "
	if !hasRef {
		insert = append(insert, fmt.Sprintf(`%sref = "%s";`, indent, ref))
	}
	if !hasRev {
		insert = append(insert, fmt.Sprintf(`%srev = "%s";`, indent, rev))
	}
	if len(insert) > 0 {
		lines = append(lines[:end], append(insert, lines[end:]...)...)
	}

	return os.WriteFile(flakePath, []byte(strings.Join(lines, "\n")), 0o644)
}

func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}
