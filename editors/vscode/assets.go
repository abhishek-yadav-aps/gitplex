// Package vscodeextension exposes the files needed to install the Gitplex
// VS Code extension from the standalone Gitplex binary.
package vscodeextension

import _ "embed"

// PackageJSON is the extension manifest.
//
//go:embed package.json
var PackageJSON []byte

// ExtensionJS is the extension entry point.
//
//go:embed extension.js
var ExtensionJS []byte

// README is the extension's documentation.
//
//go:embed README.md
var README []byte
