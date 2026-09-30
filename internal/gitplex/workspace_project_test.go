package gitplex

import (
	"reflect"
	"testing"
)

func TestMergedHaskellProjectImportsOmitEveryLocalManifestRepo(t *testing.T) {
	manifest := Manifest{Repos: map[string]RepoConfig{
		"euler-credit-db":       {},
		"euler-lsp":             {},
		"euler-lsp-api-gateway": {},
		"repo-with-input-alias": {Dependencies: map[string]DependencyConfig{
			"euler-credit-db": {FlakeInput: "credit-db-alias"},
		}},
	}}
	fragments := []haskellProjectFragment{{repo: "euler-lsp", data: []byte(`{
  perSystem.haskellProjects.default.imports = [
    inputs.euler-credit-db.haskellFlakeProjectModules.output
    inputs.euler-lsp-api-gateway.haskellFlakeProjectModules.output
    inputs.credit-db-alias.haskellFlakeProjectModules.output
    inputs.euler-db.haskellFlakeProjectModules.output
  ];
}`)}}

	got := mergedHaskellProjectImports(fragments, manifest)
	want := []string{"inputs.euler-db.haskellFlakeProjectModules.output"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged imports = %v, want %v", got, want)
	}
}

func TestMergedRepoFlakeInputsIncludesRepoNamesAndAliases(t *testing.T) {
	manifest := Manifest{Repos: map[string]RepoConfig{
		"app": {Dependencies: map[string]DependencyConfig{
			"db": {FlakeInput: "database"},
		}},
		"db": {},
	}}

	got := mergedRepoFlakeInputs(manifest)
	want := map[string]bool{"app": true, "db": true, "database": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("merged repo inputs = %v, want %v", got, want)
	}
}
