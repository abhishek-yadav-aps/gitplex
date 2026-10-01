package gitplex

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"

	vscodeextension "github.com/abhishek-yadav-aps/gitplex/editors/vscode"
)

func TestBuildVSCodeExtension(t *testing.T) {
	for _, setting := range []string{`"files.exclude"`, `"search.exclude"`, `"files.watcherExclude"`, `"**/.gitplex"`} {
		if !bytes.Contains(vscodeextension.PackageJSON, []byte(setting)) {
			t.Fatalf("extension package is missing default %s", setting)
		}
	}
	data, manifest, err := buildVSCodeExtension()
	if err != nil {
		t.Fatalf("buildVSCodeExtension: %v", err)
	}
	if manifest.Publisher != "gitplex" || manifest.Name != "gitplex-ownership" || manifest.Version == "" {
		t.Fatalf("unexpected extension identity: %+v", manifest)
	}

	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open generated VSIX: %v", err)
	}
	want := []string{
		"[Content_Types].xml",
		"extension.vsixmanifest",
		"extension/package.json",
		"extension/extension.js",
		"extension/readme.md",
	}
	var got []string
	for _, file := range archive.File {
		got = append(got, file.Name)
		if file.Name != "extension.vsixmanifest" {
			continue
		}
		reader, err := file.Open()
		if err != nil {
			t.Fatalf("open extension.vsixmanifest: %v", err)
		}
		contents, err := io.ReadAll(reader)
		reader.Close()
		if err != nil {
			t.Fatalf("read extension.vsixmanifest: %v", err)
		}
		for _, value := range []string{`Id="gitplex-ownership"`, `Version="` + manifest.Version + `"`, `Publisher="gitplex"`} {
			if !strings.Contains(string(contents), value) {
				t.Fatalf("extension.vsixmanifest missing %q:\n%s", value, contents)
			}
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("VSIX files = %v, want %v", got, want)
	}
}

func TestInstallVSCodeExtensionInvokesCodeWithTemporaryVSIX(t *testing.T) {
	var gotName string
	var gotArgs []string
	var installed []byte
	run := func(name string, args ...string) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		var err error
		installed, err = os.ReadFile(args[1])
		return err
	}

	if err := installVSCodeExtension("/test/bin/code", run); err != nil {
		t.Fatalf("installVSCodeExtension: %v", err)
	}
	if gotName != "/test/bin/code" {
		t.Fatalf("command = %q, want /test/bin/code", gotName)
	}
	if len(gotArgs) != 3 || gotArgs[0] != "--install-extension" || gotArgs[2] != "--force" {
		t.Fatalf("arguments = %v", gotArgs)
	}
	if _, err := os.Stat(gotArgs[1]); !os.IsNotExist(err) {
		t.Fatalf("temporary VSIX still exists after installation: %v", err)
	}
	if _, err := zip.NewReader(bytes.NewReader(installed), int64(len(installed))); err != nil {
		t.Fatalf("installer received invalid VSIX: %v", err)
	}
}

func TestParseInstallExtensionsArgs(t *testing.T) {
	if err := parseInstallExtensionsArgs(nil); err != nil {
		t.Fatalf("parseInstallExtensionsArgs: %v", err)
	}
	if err := parseInstallExtensionsArgs([]string{"unexpected"}); err == nil {
		t.Fatal("parseInstallExtensionsArgs accepted an argument")
	}
}
