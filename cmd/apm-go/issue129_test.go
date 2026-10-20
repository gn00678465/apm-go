package main

import (
	"os"
	"path/filepath"
	"testing"
)

const mcpServer129 = "      registry: false\n      transport: stdio\n      command: echo\n"

// installedMCP129 writes manifest, installs it, and returns the absolute
// path of apm.yml.
func installedMCP129(t *testing.T, manifest string) string {
	t.Helper()
	dir := chdirTemp(t)
	path := filepath.Join(dir, "apm.yml")
	if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunCLI(t, "install")
	return mcpCoverManifestPath(t)
}

func TestUninstall_DevMCPServer_NamesDevSection(t *testing.T) {
	manifestPath := installedMCP129(t, "name: p\nversion: 1.0.0\ntargets: [claude]\n"+
		"dependencies:\n  apm: []\n  mcp:\n    - name: prod-srv\n"+mcpServer129+
		"devDependencies:\n  mcp:\n    - name: dev-srv\n"+mcpServer129)

	want := " > Uninstalling 1 package(s)...\n" +
		" + dev-srv - found in apm.yml\n" +
		" i Removed dev-srv from devDependencies.mcp in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, "uninstall", "dev-srv"); got != want {
		t.Errorf("uninstall dev-srv:\n%s\nwant:\n%s", got, want)
	}

	wantManifest := "name: p\nversion: 1.0.0\ntargets: [claude]\n" +
		"dependencies:\n  apm: []\n  mcp:\n    - name: prod-srv\n" + mcpServer129
	if got, err := os.ReadFile(manifestPath); err != nil || string(got) != wantManifest {
		t.Errorf("apm.yml (err %v):\n%s\nwant:\n%s", err, got, wantManifest)
	}
}

func TestUninstall_MCPServerInBothSections_OneLinePerSection(t *testing.T) {
	manifestPath := installedMCP129(t, "name: p\nversion: 1.0.0\ntargets: [claude]\n"+
		"dependencies:\n  apm: []\n  mcp:\n    - name: both\n"+mcpServer129+
		"devDependencies:\n  mcp:\n    - name: both\n"+mcpServer129)

	want := " > Uninstalling 1 package(s)...\n" +
		" + both - found in apm.yml\n" +
		" i Removed both from dependencies.mcp in apm.yml\n" +
		" i Removed both from devDependencies.mcp in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml\n"
	if got := mustRunCLI(t, "uninstall", "both"); got != want {
		t.Errorf("uninstall both:\n%s\nwant:\n%s", got, want)
	}
}
