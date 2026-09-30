package gitplex

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"

	vscodeextension "github.com/abhishek-yadav-aps/gitplex/editors/vscode"
)

type vscodePackage struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Publisher   string   `json:"publisher"`
	Categories  []string `json:"categories"`
	Engines     struct {
		VSCode string `json:"vscode"`
	} `json:"engines"`
}

type extensionInstallerCommand func(name string, args ...string) error

// InstallExtensions installs the bundled Gitplex extension into VS Code.
func InstallExtensions() error {
	code, err := exec.LookPath("code")
	if err != nil {
		return fmt.Errorf("VS Code CLI not found in PATH; install the 'code' command and retry")
	}
	return installVSCodeExtension(code, runExtensionInstaller)
}

func runExtensionInstaller(name string, args ...string) error {
	return commandStreaming("", name, args...)
}

func installVSCodeExtension(code string, run extensionInstallerCommand) error {
	vsix, manifest, err := buildVSCodeExtension()
	if err != nil {
		return err
	}

	file, err := os.CreateTemp("", "gitplex-ownership-*.vsix")
	if err != nil {
		return fmt.Errorf("create temporary VSIX: %w", err)
	}
	path := file.Name()
	defer os.Remove(path)

	if _, err := file.Write(vsix); err != nil {
		file.Close()
		return fmt.Errorf("write temporary VSIX: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("close temporary VSIX: %w", err)
	}

	fmt.Printf("Installing %s %s with %s\n", manifest.DisplayName, manifest.Version, code)
	if err := run(code, "--install-extension", path, "--force"); err != nil {
		return fmt.Errorf("install VS Code extension: %w", err)
	}
	fmt.Printf("Installed %s %s\n", manifest.DisplayName, manifest.Version)
	return nil
}

func buildVSCodeExtension() ([]byte, vscodePackage, error) {
	var manifest vscodePackage
	if err := json.Unmarshal(vscodeextension.PackageJSON, &manifest); err != nil {
		return nil, manifest, fmt.Errorf("read bundled VS Code extension manifest: %w", err)
	}
	if manifest.Name == "" || manifest.DisplayName == "" || manifest.Description == "" || manifest.Version == "" || manifest.Publisher == "" || manifest.Engines.VSCode == "" {
		return nil, manifest, fmt.Errorf("bundled VS Code extension manifest is incomplete")
	}

	vsixManifest, err := makeVSIXManifest(manifest)
	if err != nil {
		return nil, manifest, err
	}

	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	files := []struct {
		name string
		data []byte
	}{
		{"[Content_Types].xml", []byte(vsixContentTypes)},
		{"extension.vsixmanifest", vsixManifest},
		{"extension/package.json", vscodeextension.PackageJSON},
		{"extension/extension.js", vscodeextension.ExtensionJS},
		{"extension/readme.md", vscodeextension.README},
	}
	for _, file := range files {
		entry, err := archive.Create(file.name)
		if err != nil {
			return nil, manifest, fmt.Errorf("create %s in VSIX: %w", file.name, err)
		}
		if _, err := entry.Write(file.data); err != nil {
			return nil, manifest, fmt.Errorf("write %s in VSIX: %w", file.name, err)
		}
	}
	if err := archive.Close(); err != nil {
		return nil, manifest, fmt.Errorf("finish VSIX: %w", err)
	}
	return buffer.Bytes(), manifest, nil
}

func makeVSIXManifest(manifest vscodePackage) ([]byte, error) {
	var categories bytes.Buffer
	for index, category := range manifest.Categories {
		if index > 0 {
			categories.WriteByte(',')
		}
		if err := xml.EscapeText(&categories, []byte(category)); err != nil {
			return nil, fmt.Errorf("encode VSIX categories: %w", err)
		}
	}

	escape := func(value string) (string, error) {
		var escaped bytes.Buffer
		if err := xml.EscapeText(&escaped, []byte(value)); err != nil {
			return "", err
		}
		return escaped.String(), nil
	}
	name, err := escape(manifest.Name)
	if err != nil {
		return nil, fmt.Errorf("encode VSIX name: %w", err)
	}
	version, err := escape(manifest.Version)
	if err != nil {
		return nil, fmt.Errorf("encode VSIX version: %w", err)
	}
	publisher, err := escape(manifest.Publisher)
	if err != nil {
		return nil, fmt.Errorf("encode VSIX publisher: %w", err)
	}
	displayName, err := escape(manifest.DisplayName)
	if err != nil {
		return nil, fmt.Errorf("encode VSIX display name: %w", err)
	}
	description, err := escape(manifest.Description)
	if err != nil {
		return nil, fmt.Errorf("encode VSIX description: %w", err)
	}
	engine, err := escape(manifest.Engines.VSCode)
	if err != nil {
		return nil, fmt.Errorf("encode VSIX engine: %w", err)
	}

	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<PackageManifest Version="2.0.0" xmlns="http://schemas.microsoft.com/developer/vsx-schema/2011">
  <Metadata>
    <Identity Language="en-US" Id="%s" Version="%s" Publisher="%s" />
    <DisplayName>%s</DisplayName>
    <Description xml:space="preserve">%s</Description>
    <Tags></Tags>
    <Categories>%s</Categories>
    <GalleryFlags>Public</GalleryFlags>
    <Properties>
      <Property Id="Microsoft.VisualStudio.Code.Engine" Value="%s" />
      <Property Id="Microsoft.VisualStudio.Code.ExtensionDependencies" Value="" />
      <Property Id="Microsoft.VisualStudio.Code.ExtensionPack" Value="" />
      <Property Id="Microsoft.VisualStudio.Code.ExtensionKind" Value="workspace" />
      <Property Id="Microsoft.VisualStudio.Code.LocalizedLanguages" Value="" />
      <Property Id="Microsoft.VisualStudio.Code.EnabledApiProposals" Value="" />
      <Property Id="Microsoft.VisualStudio.Code.ExecutesCode" Value="true" />
      <Property Id="Microsoft.VisualStudio.Services.GitHubFlavoredMarkdown" Value="true" />
      <Property Id="Microsoft.VisualStudio.Services.Content.Pricing" Value="Free" />
    </Properties>
  </Metadata>
  <Installation>
    <InstallationTarget Id="Microsoft.VisualStudio.Code" />
  </Installation>
  <Dependencies />
  <Assets>
    <Asset Type="Microsoft.VisualStudio.Code.Manifest" Path="extension/package.json" Addressable="true" />
    <Asset Type="Microsoft.VisualStudio.Services.Content.Details" Path="extension/readme.md" Addressable="true" />
  </Assets>
</PackageManifest>`, name, version, publisher, displayName, description, categories.String(), engine)), nil
}

const vsixContentTypes = `<?xml version="1.0" encoding="utf-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="js" ContentType="application/javascript"/><Default Extension="json" ContentType="application/json"/><Default Extension="md" ContentType="text/markdown"/><Default Extension="vsixmanifest" ContentType="text/xml"/></Types>`
