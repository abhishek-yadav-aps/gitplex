package gitplex

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var (
	selfPathPattern     = regexp.MustCompile(`\$\{inputs\.self\}/([A-Za-z0-9._/\-]+)`)
	relativePathPattern = regexp.MustCompile(`\./[A-Za-z0-9._/\-]+`)
)

func generateWorkspaceProject(root string, manifest Manifest, state State) error {
	return generateWorkspaceProjectWithBaseline(root, manifest, state, true)
}

func generateWorkspaceProjectWithBaseline(root string, manifest Manifest, state State, commitBaseline bool) error {
	workspacePath := filepath.Join(root, manifest.Workspace)
	fmt.Println("discovering cabal packages")
	packageDirs, err := discoverCabalPackageDirs(workspacePath)
	if err != nil {
		return err
	}
	if len(packageDirs) > 0 {
		fmt.Printf("writing cabal.project with %d package(s)\n", len(packageDirs))
		if err := writeWorkspaceCabalProject(workspacePath, packageDirs); err != nil {
			return err
		}
	}
	fmt.Println("syncing workspace files")
	if _, err := syncWorkspaceFiles(workspacePath, manifest, state); err != nil {
		return err
	}
	fmt.Println("discovering external package deps")
	externalDeps, err := discoverExternalPackageDeps(workspacePath, packageDirs)
	if err != nil {
		return err
	}
	fmt.Println("patching workspace flake")
	if _, err := patchWorkspaceFlake(workspacePath, manifest, state, externalDeps); err != nil {
		return err
	}
	fmt.Println("patching haskell project files")
	if err := patchWorkspaceHaskellProject(workspacePath, manifest, state, packageDirs); err != nil {
		return err
	}
	if len(packageDirs) > 0 {
		fmt.Println("writing cabal config")
		if err := writeWorkspaceCabalConfig(workspacePath); err != nil {
			return err
		}
	}
	fmt.Println("writing workspace gitignore")
	if err := writeWorkspaceGitIgnore(workspacePath, manifest, state); err != nil {
		return err
	}
	if err := stageWorkspaceForFlakeLock(workspacePath); err != nil {
		return err
	}
	fmt.Println("locking workspace flake")
	if err := lockWorkspaceFlake(workspacePath); err != nil {
		return err
	}
	fmt.Println("preparing workspace git repo")
	return prepareWorkspaceGit(workspacePath, commitBaseline)
}

func lockWorkspaceFlake(workspacePath string) error {
	if _, err := os.Stat(filepath.Join(workspacePath, "flake.nix")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	return runCommandAndPrint(workspacePath, "nix", "flake", "lock")
}

func discoverCabalPackageDirs(workspacePath string) ([]string, error) {
	seen := map[string]bool{}
	if err := filepath.WalkDir(workspacePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && ignoredDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cabal") {
			return nil
		}
		rel, err := filepath.Rel(workspacePath, filepath.Dir(path))
		if err != nil {
			return err
		}
		seen[filepath.ToSlash(rel)] = true
		return nil
	}); err != nil {
		return nil, err
	}

	packageDirs := make([]string, 0, len(seen))
	for dir := range seen {
		if dir != "." {
			packageDirs = append(packageDirs, dir)
		}
	}
	sort.Strings(packageDirs)
	return packageDirs, nil
}

func writeWorkspaceCabalProject(workspacePath string, packageDirs []string) error {
	var b strings.Builder
	b.WriteString("packages:\n")
	for _, dir := range packageDirs {
		fmt.Fprintf(&b, "  %s\n", dir)
	}
	b.WriteString(`
optimization: 0
library-vanilla: False
shared: True
executable-dynamic: True
write-ghc-environment-files: never

-- Number of parallel builds ghc is allowed to do
jobs: 2

-- haskell-flake only parses ` + "`packages`" + ` from ` + "`cabal.project`" + `.
flags: +Local
`)
	return os.WriteFile(filepath.Join(workspacePath, "cabal.project"), []byte(b.String()), 0o644)
}

func syncWorkspaceFiles(workspacePath string, manifest Manifest, state State) ([]string, error) {
	seen := map[string]bool{}
	queue := make([]WorkspaceFile, 0, len(manifest.WorkspaceFiles))
	queue = append(queue, manifest.WorkspaceFiles...)
	envrcFiles, err := implicitWorkspaceEnvrcFiles(manifest, state)
	if err != nil {
		return nil, err
	}
	queue = append(queue, envrcFiles...)
	var copied []string

	for len(queue) > 0 {
		file := queue[0]
		queue = queue[1:]
		dstKey := filepath.ToSlash(file.To)
		if seen[dstKey] {
			continue
		}
		seen[dstKey] = true

		repoState, ok := state.Repos[file.Repo]
		if !ok {
			return nil, fmt.Errorf("workspace file repo %q is missing from state", file.Repo)
		}
		src := filepath.Join(repoState.Path, file.From)
		dst := filepath.Join(workspacePath, file.To)
		if err := removeGeneratedPath(dst); err != nil {
			return nil, err
		}
		if err := copyTree(src, dst); err != nil {
			return nil, fmt.Errorf("copy workspace file %s from %s: %w", file.To, file.Repo, err)
		}
		copied = append(copied, dstKey)

		deps, err := discoverWorkspaceFileDependencies(repoState.Path, file)
		if err != nil {
			return nil, err
		}
		queue = append(queue, deps...)
	}

	sort.Strings(copied)
	return copied, nil
}

func implicitWorkspaceEnvrcFiles(manifest Manifest, state State) ([]WorkspaceFile, error) {
	var files []WorkspaceFile
	for _, file := range manifest.WorkspaceFiles {
		if filepath.ToSlash(file.To) != "flake.nix" {
			continue
		}
		repoState, ok := state.Repos[file.Repo]
		if !ok {
			return nil, fmt.Errorf("workspace file repo %q is missing from state", file.Repo)
		}
		if _, err := os.Stat(filepath.Join(repoState.Path, ".envrc")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		files = append(files, WorkspaceFile{
			Repo: file.Repo,
			From: ".envrc",
			To:   ".envrc",
		})
	}
	return files, nil
}

func discoverWorkspaceFileDependencies(repoRoot string, file WorkspaceFile) ([]WorkspaceFile, error) {
	src := filepath.Join(repoRoot, file.From)
	info, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, nil
	}

	data, err := os.ReadFile(src)
	if err != nil {
		return nil, err
	}

	srcDir := filepath.Dir(file.From)
	dstDir := filepath.Dir(file.To)
	seen := map[string]bool{}
	var deps []WorkspaceFile

	addDep := func(srcRel string) error {
		srcRel = filepath.Clean(srcRel)
		if srcRel == "." {
			return nil
		}
		if err := validateRelativePath(srcRel); err != nil {
			return nil
		}
		if _, err := os.Stat(filepath.Join(repoRoot, srcRel)); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		relFromBase, err := filepath.Rel(srcDir, srcRel)
		if err != nil {
			return err
		}
		dstRel := filepath.Clean(filepath.Join(dstDir, relFromBase))
		if err := validateRelativePath(dstRel); err != nil {
			return nil
		}
		key := filepath.ToSlash(dstRel)
		if seen[key] {
			return nil
		}
		seen[key] = true
		deps = append(deps, WorkspaceFile{
			Repo: file.Repo,
			From: filepath.ToSlash(srcRel),
			To:   filepath.ToSlash(dstRel),
		})
		return nil
	}

	for _, match := range selfPathPattern.FindAllStringSubmatch(string(data), -1) {
		if len(match) < 2 {
			continue
		}
		if err := addDep(match[1]); err != nil {
			return nil, err
		}
	}
	for _, match := range relativePathPattern.FindAllString(string(data), -1) {
		ref := strings.TrimPrefix(match, "./")
		srcRel := filepath.Clean(filepath.Join(srcDir, ref))
		if err := addDep(srcRel); err != nil {
			return nil, err
		}
	}
	return deps, nil
}

func patchWorkspaceHaskellProject(workspacePath string, manifest Manifest, state State, packageDirs []string) error {
	projectPath := filepath.Join(workspacePath, "nix", "haskell-project.nix")
	data, err := os.ReadFile(projectPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")
	lines = replaceHaskellProjectImports(lines, rootRepoProjectImports(manifest, state))
	start := -1
	end := -1
	for i, line := range lines {
		if strings.Contains(line, "fileset = fs.unions [") {
			start = i
			continue
		}
		if start >= 0 && strings.TrimSpace(line) == "];" {
			end = i
			break
		}
	}
	if start == -1 || end == -1 || end <= start {
		return os.WriteFile(projectPath, []byte(strings.Join(lines, "\n")), 0o644)
	}

	indent := leadingWhitespace(lines[start]) + "  "
	replacement := make([]string, 0, len(packageDirs)+1)
	for _, path := range workspaceSourcePaths(packageDirs) {
		replacement = append(replacement, fmt.Sprintf("%s../%s", indent, path))
	}

	updated := append([]string{}, lines[:start+1]...)
	updated = append(updated, replacement...)
	updated = append(updated, lines[end:]...)
	return os.WriteFile(projectPath, []byte(strings.Join(updated, "\n")), 0o644)
}

func workspaceSourcePaths(packageDirs []string) []string {
	seen := map[string]bool{
		"cabal.project": true,
	}
	for _, path := range packageDirs {
		seen[filepath.ToSlash(path)] = true
	}

	paths := make([]string, 0, len(seen))
	for path := range seen {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func discoverExternalPackageDeps(workspacePath string, packageDirs []string) ([]string, error) {
	localPackages, err := discoverLocalPackages(workspacePath, packageDirs)
	if err != nil {
		return nil, err
	}
	deps, err := discoverBuildDepends(workspacePath)
	if err != nil {
		return nil, err
	}
	var external []string
	for dep := range deps {
		if _, ok := localPackages[dep]; ok {
			continue
		}
		external = append(external, dep)
	}
	sort.Strings(external)
	return external, nil
}

func discoverLocalPackages(workspacePath string, packageDirs []string) (map[string]string, error) {
	packages := map[string]string{}
	for _, dir := range packageDirs {
		matches, err := filepath.Glob(filepath.Join(workspacePath, dir, "*.cabal"))
		if err != nil {
			return nil, err
		}
		if len(matches) == 0 {
			continue
		}
		name, err := readCabalPackageName(matches[0])
		if err != nil {
			return nil, err
		}
		if name != "" {
			packages[name] = filepath.ToSlash(dir)
		}
	}
	return packages, nil
}

func readCabalPackageName(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(strings.ToLower(line), "name:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "name:")), nil
		}
	}
	return "", scanner.Err()
}

func discoverBuildDepends(workspacePath string) (map[string]bool, error) {
	deps := map[string]bool{}
	re := regexp.MustCompile(`[A-Za-z0-9][A-Za-z0-9-]*`)
	err := filepath.WalkDir(workspacePath, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && ignoredDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".cabal") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		scanner := bufio.NewScanner(strings.NewReader(string(data)))
		inDepends := false
		for scanner.Scan() {
			raw := scanner.Text()
			trimmed := strings.TrimSpace(raw)
			if trimmed == "" {
				inDepends = false
				continue
			}
			lower := strings.ToLower(trimmed)
			if strings.HasPrefix(lower, "build-depends:") {
				inDepends = true
				trimmed = strings.TrimSpace(trimmed[len("build-depends:"):])
			} else if inDepends {
				if !strings.HasPrefix(raw, " ") && !strings.HasPrefix(raw, "\t") {
					inDepends = false
				} else if looksLikeCabalField(trimmed) {
					inDepends = false
				}
			}
			if !inDepends {
				continue
			}
			for _, part := range strings.Split(trimmed, ",") {
				match := re.FindString(strings.TrimSpace(part))
				if match != "" {
					deps[match] = true
				}
			}
		}
		return scanner.Err()
	})
	return deps, err
}

func looksLikeCabalField(line string) bool {
	if strings.HasPrefix(line, ",") {
		return false
	}
	colon := strings.Index(line, ":")
	if colon <= 0 {
		return false
	}
	name := strings.TrimSpace(line[:colon])
	for _, r := range name {
		if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-' {
			return false
		}
	}
	return true
}

func rootRepoProjectImports(manifest Manifest, state State) []string {
	rootRepos := make([]string, 0, len(manifest.Repos))
	for name, repo := range manifest.Repos {
		if len(repo.Dependencies) == 0 {
			rootRepos = append(rootRepos, name)
		}
	}
	sort.Strings(rootRepos)

	seen := map[string]bool{}
	var imports []string
	for _, name := range rootRepos {
		repoState, ok := state.Repos[name]
		if !ok {
			continue
		}
		projectPath := filepath.Join(repoState.Path, "nix", "haskell-project.nix")
		data, err := os.ReadFile(projectPath)
		if err != nil {
			continue
		}
		for _, line := range extractHaskellProjectImports(strings.Split(string(data), "\n")) {
			key := strings.TrimSpace(line)
			if key == "" || seen[key] {
				continue
			}
			seen[key] = true
			imports = append(imports, key)
		}
	}
	return imports
}

func extractHaskellProjectImports(lines []string) []string {
	start, end := findListBlock(lines, "imports = [")
	if start == -1 || end == -1 || end <= start {
		return nil
	}
	var imports []string
	for _, line := range lines[start+1 : end] {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "haskellFlakeProjectModules.output") {
			imports = append(imports, trimmed)
		}
	}
	return imports
}

func replaceHaskellProjectImports(lines []string, imports []string) []string {
	if len(imports) == 0 {
		return lines
	}
	start, end := findListBlock(lines, "imports = [")
	if start == -1 || end == -1 || end <= start {
		return lines
	}

	indent := leadingWhitespace(lines[start]) + "  "
	replacement := make([]string, 0, len(imports))
	for _, importLine := range imports {
		replacement = append(replacement, indent+strings.TrimSpace(importLine))
	}

	updated := append([]string{}, lines[:start+1]...)
	updated = append(updated, replacement...)
	updated = append(updated, lines[end:]...)
	return updated
}

func findListBlock(lines []string, marker string) (int, int) {
	start := -1
	for i, line := range lines {
		if start == -1 && strings.Contains(line, marker) {
			start = i
			continue
		}
		if start >= 0 && strings.TrimSpace(line) == "];" {
			return start, i
		}
	}
	return -1, -1
}

func removeMergedRepoFlakeInputs(lines []string, manifest Manifest) []string {
	remove := mergedRepoFlakeInputs(manifest)
	if len(remove) == 0 {
		return lines
	}
	start, end := findInputsBlock(lines)
	if start == -1 || end == -1 || end <= start {
		return lines
	}

	updated := append([]string{}, lines[:start+1]...)
	for i := start + 1; i < end; i++ {
		name, ok := flakeInputBlockName(lines[i])
		if ok && remove[name] {
			blockEnd := findAttributeBlockEnd(lines, i)
			if blockEnd > i {
				i = blockEnd
				continue
			}
		}

		if name, ok := flakeInputAssignmentName(lines[i]); ok && remove[name] {
			continue
		}
		updated = append(updated, lines[i])
	}
	updated = append(updated, lines[end:]...)
	return updated
}

func mergedRepoFlakeInputs(manifest Manifest) map[string]bool {
	inputs := map[string]bool{}
	for _, repo := range manifest.Repos {
		for _, dep := range repo.Dependencies {
			if dep.FlakeInput != "" {
				inputs[dep.FlakeInput] = true
			}
		}
	}
	return inputs
}

func flakeInputBlockName(line string) (string, bool) {
	match := regexp.MustCompile(`^\s*([A-Za-z0-9-]+)\s*=\s*\{\s*$`).FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func flakeInputAssignmentName(line string) (string, bool) {
	match := regexp.MustCompile(`^\s*([A-Za-z0-9-]+)\.(url|type|ref|rev|inputs\.)`).FindStringSubmatch(line)
	if match == nil {
		return "", false
	}
	return match[1], true
}

func findAttributeBlockEnd(lines []string, start int) int {
	depth := 0
	for i := start; i < len(lines); i++ {
		depth += strings.Count(lines[i], "{")
		depth -= strings.Count(lines[i], "}")
		if i > start && depth == 0 {
			return i
		}
	}
	return start
}

func patchWorkspaceFlake(workspacePath string, manifest Manifest, state State, externalDeps []string) ([]string, error) {
	flakePath := filepath.Join(workspacePath, "flake.nix")
	data, err := os.ReadFile(flakePath)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	lines := strings.Split(string(data), "\n")
	lines = removeMergedRepoFlakeInputs(lines, manifest)
	start, end := findInputsBlock(lines)
	if start == -1 || end == -1 {
		if strings.Join(lines, "\n") != string(data) {
			return nil, os.WriteFile(flakePath, []byte(strings.Join(lines, "\n")), 0o644)
		}
		return nil, nil
	}

	existing := findDeclaredFlakeInputs(lines[start:end])
	var additions []string
	for _, input := range localRepoFlakeInputs(workspacePath, manifest, state) {
		if existing[input.name] {
			continue
		}
		additions = append(additions, input.lines...)
		existing[input.name] = true
	}

	available := make([]string, 0, len(externalDeps))
	for _, dep := range externalDeps {
		if existing[dep] {
			available = append(available, dep)
			continue
		}
		block, found, err := findFlakeInputBlock(state, dep)
		if err != nil {
			return nil, err
		}
		if found {
			additions = append(additions, block...)
			available = append(available, dep)
		}
	}

	updated := lines
	if len(additions) > 0 {
		updated = append([]string{}, lines[:end]...)
		updated = append(updated, additions...)
		updated = append(updated, lines[end:]...)
	}
	if strings.Join(updated, "\n") == string(data) {
		return available, nil
	}

	if err := os.WriteFile(flakePath, []byte(strings.Join(updated, "\n")), 0o644); err != nil {
		return nil, err
	}
	return available, nil
}

type flakeInputAddition struct {
	name  string
	lines []string
}

func localRepoFlakeInputs(workspacePath string, manifest Manifest, state State) []flakeInputAddition {
	needed := mergedRepoFlakeInputs(manifest)
	names := make([]string, 0, len(needed))
	for name := range needed {
		names = append(names, name)
	}
	sort.Strings(names)

	var additions []flakeInputAddition
	for _, inputName := range names {
		repoState, ok := state.Repos[inputName]
		if !ok {
			continue
		}
		if _, err := os.Stat(filepath.Join(repoState.Path, "flake.nix")); err != nil {
			continue
		}
		inputPath := filepath.ToSlash(repoState.Path)
		additions = append(additions, flakeInputAddition{
			name: inputName,
			lines: []string{
				fmt.Sprintf("    %s.url = \"%s\";", inputName, nixFlakePathURL(inputPath)),
				fmt.Sprintf("    %s.inputs.common.follows = \"common\";", inputName),
				"",
			},
		})
	}
	return additions
}

func nixFlakePathURL(path string) string {
	return "path:" + strings.ReplaceAll(path, `"`, `\"`)
}

func findInputsBlock(lines []string) (int, int) {
	start := -1
	depth := 0
	for i, line := range lines {
		if start == -1 && strings.Contains(line, "inputs = {") {
			start = i
		}
		if start >= 0 {
			depth += strings.Count(line, "{")
			depth -= strings.Count(line, "}")
			if i > start && depth == 0 {
				return start, i
			}
		}
	}
	return -1, -1
}

func findDeclaredFlakeInputs(lines []string) map[string]bool {
	declared := map[string]bool{}
	blockPattern := regexp.MustCompile(`^\s*([A-Za-z0-9-]+)\s*=\s*\{\s*$`)
	urlPattern := regexp.MustCompile(`^\s*([A-Za-z0-9-]+)\.url\s*=`)
	for _, line := range lines {
		if match := blockPattern.FindStringSubmatch(line); match != nil {
			declared[match[1]] = true
			continue
		}
		if match := urlPattern.FindStringSubmatch(line); match != nil {
			declared[match[1]] = true
		}
	}
	return declared
}

func findFlakeInputBlock(state State, inputName string) ([]string, bool, error) {
	for _, repoState := range state.Repos {
		flakePath := filepath.Join(repoState.Path, "flake.nix")
		data, err := os.ReadFile(flakePath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		block, found := extractFlakeInputBlock(strings.Split(string(data), "\n"), inputName)
		if found {
			return block, true, nil
		}
	}
	return nil, false, nil
}

func extractFlakeInputBlock(lines []string, inputName string) ([]string, bool) {
	blockPattern := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(inputName) + `\s*=\s*\{\s*$`)
	for i, line := range lines {
		if !blockPattern.MatchString(line) {
			continue
		}
		depth := 0
		for j := i; j < len(lines); j++ {
			depth += strings.Count(lines[j], "{")
			depth -= strings.Count(lines[j], "}")
			if j > i && depth == 0 {
				block := append([]string{}, lines[i:j+1]...)
				block = append(block, "")
				return block, true
			}
		}
	}
	return nil, false
}

func writeWorkspaceCabalConfig(workspacePath string) error {
	cabalDir := filepath.Join(workspacePath, ".cabal-dir")
	if err := os.MkdirAll(cabalDir, 0o755); err != nil {
		return err
	}

	content := fmt.Sprintf(`-- Generated by gitplex for the merged workspace.
repository hackage.haskell.org
  url: https://hackage.haskell.org/
  secure: False

remote-repo-cache: %s
logs-dir: %s
store-dir: %s
extra-prog-path: %s
`, filepath.Join(cabalDir, "packages"), filepath.Join(cabalDir, "logs"), filepath.Join(cabalDir, "store"), filepath.Join(cabalDir, "bin"))

	return os.WriteFile(filepath.Join(cabalDir, "config"), []byte(content), 0o644)
}

func writeWorkspaceGitIgnore(workspacePath string, manifest Manifest, state State) error {
	var b strings.Builder
	b.WriteString(`dist-newstyle/
.cabal-dir/*
!.cabal-dir/
!.cabal-dir/config
.pre-commit-config.yaml
cachix.log
`)

	repoNames := make([]string, 0, len(manifest.Repos))
	for name := range manifest.Repos {
		repoNames = append(repoNames, name)
	}
	sort.Strings(repoNames)

	for _, name := range repoNames {
		repoState, ok := state.Repos[name]
		if !ok {
			return fmt.Errorf("repo %q is missing from state", name)
		}
		data, err := os.ReadFile(filepath.Join(repoState.Path, ".gitignore"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}

		modules := append([]ModuleMapping{}, manifest.Repos[name].Modules...)
		sort.Slice(modules, func(i, j int) bool {
			return modules[i].To < modules[j].To
		})
		for _, module := range modules {
			b.WriteString("\n")
			fmt.Fprintf(&b, "# From %s/.gitignore for %s\n", name, filepath.ToSlash(module.To))
			for _, line := range strings.Split(string(data), "\n") {
				for _, translated := range translateGitIgnoreLine(line, module.To) {
					b.WriteString(translated)
					b.WriteString("\n")
				}
			}
		}
	}

	return os.WriteFile(filepath.Join(workspacePath, ".gitignore"), []byte(b.String()), 0o644)
}

func translateGitIgnoreLine(line, moduleTo string) []string {
	if line == "" || strings.HasPrefix(strings.TrimLeft(line, " \t"), "#") {
		return []string{line}
	}

	negated := strings.HasPrefix(line, "!")
	pattern := line
	if negated {
		pattern = strings.TrimPrefix(pattern, "!")
	}
	prefix := ""
	if negated {
		prefix = "!"
	}
	modulePrefix := filepath.ToSlash(filepath.Clean(moduleTo))
	pattern = filepath.ToSlash(pattern)
	anchoredPattern := strings.TrimLeft(pattern, "/")
	if anchoredPattern == "" {
		return []string{line}
	}
	scopedPattern := anchoredPattern
	if modulePrefix != "." {
		scopedPattern = modulePrefix + "/" + anchoredPattern
	}

	hasPathSeparator := strings.Contains(strings.TrimSuffix(pattern, "/"), "/")
	if strings.HasPrefix(pattern, "/") || hasPathSeparator {
		return []string{prefix + scopedPattern}
	}
	if modulePrefix == "." {
		return []string{prefix + anchoredPattern}
	}
	return []string{prefix + modulePrefix + "/**/" + anchoredPattern}
}

func stageWorkspaceForFlakeLock(workspacePath string) error {
	if _, err := os.Stat(filepath.Join(workspacePath, "flake.nix")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	if err := ensureWorkspaceGit(workspacePath); err != nil {
		return err
	}
	return gitAddWorkspace(workspacePath)
}

func prepareWorkspaceGit(workspacePath string, commitBaseline bool) error {
	if err := ensureWorkspaceGit(workspacePath); err != nil {
		return err
	}
	if commitBaseline {
		if err := gitAddWorkspace(workspacePath); err != nil {
			return err
		}
		dirty, err := gitHasChanges(workspacePath)
		if err != nil {
			return err
		}
		if !dirty {
			return nil
		}
		_, err = git(workspacePath, "commit", "-m", "chore: gitplex generated workspace baseline")
		return err
	}
	_, _ = git(workspacePath, "reset")
	if _, err := git(workspacePath, "rev-parse", "--verify", "HEAD"); err == nil {
		return nil
	}
	if err := gitAddWorkspace(workspacePath); err != nil {
		return err
	}
	dirty, err := gitHasChanges(workspacePath)
	if err != nil {
		return err
	}
	if !dirty {
		return nil
	}
	_, err = git(workspacePath, "commit", "-m", "chore: gitplex generated workspace baseline")
	return err
}

func ensureWorkspaceGit(workspacePath string) error {
	if _, err := os.Stat(filepath.Join(workspacePath, ".git")); os.IsNotExist(err) {
		if _, err := git(workspacePath, "init"); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	_, _ = git(workspacePath, "reset")
	if _, err := git(workspacePath, "config", "user.name", "Gitplex"); err != nil {
		return err
	}
	if _, err := git(workspacePath, "config", "user.email", "gitplex@example.invalid"); err != nil {
		return err
	}
	return nil
}

func gitAddWorkspace(workspacePath string) error {
	_, err := git(workspacePath, "add", "-A")
	return err
}
