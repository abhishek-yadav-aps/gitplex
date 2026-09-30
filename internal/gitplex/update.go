package gitplex

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const latestReleaseURL = "https://github.com/abhishek-yadav-aps/gitplex/releases/latest/download/gitplex_%s_%s.tar.gz"

const maxReleaseBinarySize = 100 << 20

type updateInstallerCommand func(name string, args ...string) error

// Update downloads the latest GitHub release and replaces the executable that
// was used to invoke Gitplex.
func Update() error {
	url, err := releaseArchiveURL(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	target, err := os.Executable()
	if err != nil {
		return fmt.Errorf("find current executable: %w", err)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(target); resolveErr == nil {
		target = resolved
	}

	client := &http.Client{Timeout: 5 * time.Minute}
	return updateFromRelease(client, url, target, runUpdateInstaller)
}

func releaseArchiveURL(goos, goarch string) (string, error) {
	if goos != "darwin" && goos != "linux" {
		return "", fmt.Errorf("updates are not available for OS %q", goos)
	}
	if goarch != "amd64" && goarch != "arm64" {
		return "", fmt.Errorf("updates are not available for architecture %q", goarch)
	}
	return fmt.Sprintf(latestReleaseURL, goos, goarch), nil
}

func updateFromRelease(client *http.Client, url, target string, run updateInstallerCommand) error {
	fmt.Printf("Downloading latest Gitplex release from %s\n", url)
	response, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("download latest Gitplex release: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("download latest Gitplex release: %s", response.Status)
	}

	tempDir, err := os.MkdirTemp("", "gitplex-update-*")
	if err != nil {
		return fmt.Errorf("create update directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	binaryPath := filepath.Join(tempDir, "gitplex")
	if err := extractReleaseBinary(response.Body, binaryPath); err != nil {
		return err
	}
	if err := installUpdateBinary(binaryPath, target, run); err != nil {
		return err
	}
	fmt.Printf("Updated Gitplex at %s\n", target)
	return nil
}

func extractReleaseBinary(archive io.Reader, destination string) error {
	gzipReader, err := gzip.NewReader(archive)
	if err != nil {
		return fmt.Errorf("read release archive: %w", err)
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read release archive: %w", err)
		}
		name := strings.TrimPrefix(filepath.ToSlash(header.Name), "./")
		if name != "gitplex" || !header.FileInfo().Mode().IsRegular() {
			continue
		}
		if header.Size < 0 || header.Size > maxReleaseBinarySize {
			return fmt.Errorf("release binary has invalid size %d", header.Size)
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return fmt.Errorf("create release binary: %w", err)
		}
		_, copyErr := io.CopyN(file, tarReader, header.Size)
		closeErr := file.Close()
		if copyErr != nil {
			return fmt.Errorf("extract release binary: %w", copyErr)
		}
		if closeErr != nil {
			return fmt.Errorf("close release binary: %w", closeErr)
		}
		return nil
	}
	return fmt.Errorf("release archive does not contain gitplex")
}

func installUpdateBinary(binaryPath, target string, run updateInstallerCommand) error {
	targetDir := filepath.Dir(target)
	temp, err := os.CreateTemp(targetDir, ".gitplex-update-*")
	if err != nil {
		fmt.Printf("Installing into %s requires elevated permissions\n", targetDir)
		if runErr := run("sudo", "install", "-m", "0755", binaryPath, target); runErr != nil {
			return fmt.Errorf("install update at %s: %w", target, runErr)
		}
		return nil
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	source, err := os.Open(binaryPath)
	if err != nil {
		temp.Close()
		return fmt.Errorf("open release binary: %w", err)
	}
	_, copyErr := io.Copy(temp, source)
	closeSourceErr := source.Close()
	chmodErr := temp.Chmod(0o755)
	closeTempErr := temp.Close()
	if copyErr != nil {
		return fmt.Errorf("stage release binary: %w", copyErr)
	}
	if closeSourceErr != nil {
		return fmt.Errorf("close release binary: %w", closeSourceErr)
	}
	if chmodErr != nil {
		return fmt.Errorf("make release binary executable: %w", chmodErr)
	}
	if closeTempErr != nil {
		return fmt.Errorf("close staged release binary: %w", closeTempErr)
	}
	if err := os.Rename(tempPath, target); err != nil {
		return fmt.Errorf("replace executable at %s: %w", target, err)
	}
	return nil
}

func runUpdateInstaller(name string, args ...string) error {
	return commandStreaming("", name, args...)
}
