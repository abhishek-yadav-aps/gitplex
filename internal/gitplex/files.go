package gitplex

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

var ignoredDirs = map[string]bool{
	".git":          true,
	".direnv":       true,
	"dist-newstyle": true,
}

func copyTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return copyFile(src, dst, info.Mode())
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		name := entry.Name()
		if entry.IsDir() && ignoredDirs[name] {
			return filepath.SkipDir
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		target := filepath.Join(dst, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFile(path, target, info.Mode())
	})
}

func copyFile(src, dst string, mode fs.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func mirrorTree(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		if err := os.RemoveAll(dst); err != nil {
			return err
		}
		return copyFile(src, dst, info.Mode())
	}
	if err := pruneExtraFiles(src, dst); err != nil {
		return err
	}
	return copyTree(src, dst)
}

func pruneExtraFiles(src, dst string) error {
	if _, err := os.Stat(dst); os.IsNotExist(err) {
		return nil
	}
	return filepath.WalkDir(dst, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && ignoredDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if path == dst {
			return nil
		}
		rel, err := filepath.Rel(dst, path)
		if err != nil {
			return err
		}
		if _, err := os.Stat(filepath.Join(src, rel)); os.IsNotExist(err) {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		return err
	})
}

func dirsDiffer(a, b string) (bool, error) {
	aInfo, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	bInfo, err := os.Stat(b)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !aInfo.IsDir() || !bInfo.IsDir() {
		if aInfo.IsDir() != bInfo.IsDir() {
			return true, nil
		}
		same, err := filesEqual(a, b)
		return !same, err
	}

	seen := map[string]bool{}
	if err := filepath.WalkDir(a, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && ignoredDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(a, path)
		if err != nil {
			return err
		}
		seen[rel] = true
		other := filepath.Join(b, rel)
		same, err := filesEqual(path, other)
		if err != nil || !same {
			return errDifferent
		}
		return nil
	}); err != nil {
		if errors.Is(err, errDifferent) {
			return true, nil
		}
		return false, err
	}

	if err := filepath.WalkDir(b, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() && ignoredDirs[entry.Name()] {
			return filepath.SkipDir
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(b, path)
		if err != nil {
			return err
		}
		if !seen[rel] {
			return errDifferent
		}
		return nil
	}); err != nil {
		if errors.Is(err, errDifferent) {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

var errDifferent = errors.New("different")

func filesEqual(a, b string) (bool, error) {
	left, err := os.ReadFile(a)
	if err != nil {
		return false, err
	}
	right, err := os.ReadFile(b)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return bytes.Equal(left, right), nil
}

func removeGeneratedPath(path string) error {
	if strings.TrimSpace(path) == "" || path == "." || path == string(filepath.Separator) {
		return nil
	}
	return os.RemoveAll(path)
}
