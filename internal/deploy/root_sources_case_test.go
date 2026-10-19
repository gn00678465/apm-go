package deploy

import (
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

// casePackage is a package whose root agents and hooks differ in the case of
// their extension (impl/H3-case: case.ps1 on Windows, the same files on
// Linux).
func casePackage(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mod := filepath.Join(dir, "apm_modules", "acme", "dep")
	mkFile(t, mod, "apm.yml", "name: dep\nversion: 1.0.0\n")
	for _, n := range []string{"X.AGENT.MD", "Y.Agent.md", "z.agent.md"} {
		mkFile(t, mod, n, "---\ndescription: d\n---\nB\n")
	}
	mkFile(t, mod, "hooks/UPPER.JSON", `{"hooks":{"preToolUse":[{"type":"command","bash":"echo upper"}]}}`)
	mkFile(t, mod, ".apm/hooks/Mixed.Json", `{"hooks":{"preToolUse":[{"type":"command","bash":"echo mixed"}]}}`)
	mkFile(t, mod, "hooks/mixed.json", `{"hooks":{"preToolUse":[{"type":"command","bash":"echo dup"}]}}`)
	m, resolved := depManifest("acme/dep")
	if _, err := Run([]string{"copilot", "claude"}, dir, m, resolved, nil, "", false, nil); err != nil {
		t.Fatal(err)
	}
	return dir
}

func assertDeployedNames(t *testing.T, dir, rel string, want []string) {
	t.Helper()
	if got := deployedFiles(t, dir, rel); !slices.Equal(got, want) {
		t.Errorf("%s = %v, want %v", rel, got, want)
	}
}

// On Windows Path.glob matches *.agent.md and *.json without regard to case.
// Oracle 0.33.0 then skips X.AGENT.MD, whose name does not end in .md as
// written, deploys Y.Agent.md as the agent Y.Agent, deploys hooks/UPPER.JSON
// as dep-UPPER.json, and keeps .apm/hooks/Mixed.Json over hooks/mixed.json
// (measured in the Windows sandbox, impl/H3-case/case-win.out).
func TestRun_RootSourcesExtensionCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Path.glob ignores case on Windows only")
	}
	dir := casePackage(t)
	assertDeployedNames(t, dir, ".github/agents", []string{"Y.Agent.agent.md", "z.agent.md"})
	assertDeployedNames(t, dir, ".claude/agents", []string{"Y.Agent.md", "z.md"})
	assertDeployedNames(t, dir, ".github/hooks", []string{"dep-Mixed.json", "dep-UPPER.json"})
}

// On Linux Path.glob matches the case as written: Oracle 0.33.0 deploys
// z.agent.md and hooks/mixed.json alone (measured, impl/H3-case/linux/or).
func TestRun_RootSourcesExtensionCaseElsewhere(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Path.glob matches case on Linux and macOS")
	}
	dir := casePackage(t)
	assertDeployedNames(t, dir, ".github/agents", []string{"z.agent.md"})
	assertDeployedNames(t, dir, ".claude/agents", []string{"z.md"})
	assertDeployedNames(t, dir, ".github/hooks", []string{"dep-mixed.json"})
}
