package gitplex

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReleaseArchiveURLMatchesREADMEInstaller(t *testing.T) {
	url, err := releaseArchiveURL("linux", "amd64")
	if err != nil {
		t.Fatalf("releaseArchiveURL: %v", err)
	}
	want := "https://github.com/abhishek-yadav-aps/gitplex/releases/latest/download/gitplex_linux_amd64.tar.gz"
	if url != want {
		t.Fatalf("release URL = %q, want %q", url, want)
	}
}

func TestUpdateFromReleaseReplacesExecutable(t *testing.T) {
	archive := releaseArchive(t, "new binary")
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Write(archive)
	}))
	defer server.Close()

	target := filepath.Join(t.TempDir(), "gitplex")
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := updateFromRelease(server.Client(), server.URL, target, func(string, ...string) error {
		t.Fatal("unexpected elevated installer")
		return nil
	}); err != nil {
		t.Fatalf("updateFromRelease: %v", err)
	}
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "new binary" {
		t.Fatalf("installed content = %q", content)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("installed mode = %o, want 755", info.Mode().Perm())
	}
}

func TestInstallUpdateBinaryUsesSudoForProtectedTarget(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "gitplex")
	if err := os.WriteFile(binary, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "missing", "gitplex")
	var gotName string
	var gotArgs []string
	err := installUpdateBinary(binary, target, func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string{}, args...)
		return nil
	})
	if err != nil {
		t.Fatalf("installUpdateBinary: %v", err)
	}
	if gotName != "sudo" {
		t.Fatalf("installer = %q, want sudo", gotName)
	}
	wantArgs := []string{"install", "-m", "0755", binary, target}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("installer args = %q, want %q", gotArgs, wantArgs)
	}
}

func releaseArchive(t *testing.T, content string) []byte {
	t.Helper()
	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	tarWriter := tar.NewWriter(gzipWriter)
	data := []byte(content)
	if err := tarWriter.WriteHeader(&tar.Header{Name: "gitplex", Mode: 0o755, Size: int64(len(data))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tarWriter.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	return output.Bytes()
}
