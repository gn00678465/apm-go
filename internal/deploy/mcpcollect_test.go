package deploy

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
)

func writeDepManifestWithMCP(t *testing.T, modDir string, mcpYAML string) {
	t.Helper()
	mkFile(t, modDir, "apm.yml", "name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n"+mcpYAML)
}

func TestRun_MCPCollection_LocalOverridesDependency(t *testing.T) {
	dir := t.TempDir()
	depKey := "acme/foo"
	modDir := filepath.Join(dir, "apm_modules", depKey)
	writeDepManifestWithMCP(t, modDir, "    - name: shared\n      registry: false\n      transport: stdio\n      command: dep-cmd\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		MCPServers: []*manifest.MCPDependency{
			{Name: "shared", Registry: false, Transport: "stdio", Command: "local-cmd"},
		},
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depKey, Owner: "acme", Repo: "foo", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{{Key: depKey, RepoURL: depKey, Kind: resolver.KindGitSemver, Depth: 1}},
	}

	result, err := Run(nil, dir, m, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// No target adapter supports TypeMCP yet (Step 4), so nothing is written;
	// this test only exercises collection + pr-002 override via diagnostics.
	// Locals are collected before deps, so the dependency entry is the one
	// reported as shadowed (conflict.go's "shadowed by local" branch).
	found := false
	for _, d := range result.Diags {
		if strings.Contains(d, `mcp "shared" from dependency:`+depKey+` shadowed by local`) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected dependency-shadowed-by-local diagnostic, got diags: %v", result.Diags)
	}
}

func TestRun_MCPCollection_FirstDeclaredDepWins(t *testing.T) {
	dir := t.TempDir()
	depA, depB := "acme/a", "acme/b"
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", depA), "    - name: shared\n      registry: false\n      transport: stdio\n      command: a-cmd\n")
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", depB), "    - name: shared\n      registry: false\n      transport: stdio\n      command: b-cmd\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depA, Owner: "acme", Repo: "a", Source: "git"},
			{RepoURL: depB, Owner: "acme", Repo: "b", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: depA, RepoURL: depA, Kind: resolver.KindGitSemver, Depth: 1},
			{Key: depB, RepoURL: depB, Kind: resolver.KindGitSemver, Depth: 1},
		},
	}

	result, err := Run(nil, dir, m, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, d := range result.Diags {
		if strings.Contains(d, "shadowed by dependency:"+depA) && strings.Contains(d, "first-declared wins") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected first-declared-wins diagnostic favoring %s, got diags: %v", depA, result.Diags)
	}
}

func TestRun_MCPCollection_TransitiveSelfDefinedSkippedWithWarning(t *testing.T) {
	dir := t.TempDir()
	transitiveKey := "acme/transitive"
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", transitiveKey), "    - name: deep\n      registry: false\n      transport: stdio\n      command: deep-cmd\n")

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: transitiveKey, RepoURL: transitiveKey, Kind: resolver.KindGitSemver, Depth: 2},
		},
	}

	result, err := Run(nil, dir, m, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, d := range result.Diags {
		if strings.Contains(d, `mcp "deep"`) && strings.Contains(d, "not auto-trusted") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected transitive-not-auto-trusted diagnostic, got diags: %v", result.Diags)
	}
}

func TestRun_MCPCollection_RegistryBackedDiagnosedAndSkipped(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		MCPServers: []*manifest.MCPDependency{
			{Name: "from-registry", Registry: nil}, // default registry -- not self-defined
		},
	}

	result, err := Run(nil, dir, m, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	found := false
	for _, d := range result.Diags {
		if strings.Contains(d, `mcp "from-registry"`) && strings.Contains(d, "registry-backed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected registry-backed diagnostic, got diags: %v", result.Diags)
	}
}

func TestLoadDependencyMCP_MissingFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	servers, diags := loadDependencyMCP("acme/none", filepath.Join(dir, "apm_modules", "acme/none"))
	if servers != nil || diags != nil {
		t.Errorf("missing apm.yml should be silent, got servers=%v diags=%v", servers, diags)
	}
}

func TestLoadDependencyMCP_MalformedFileDiagnosed(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/broken")
	mkFile(t, modDir, "apm.yml", "not: [valid, yaml, manifest\n")
	servers, diags := loadDependencyMCP("acme/broken", modDir)
	if servers != nil {
		t.Errorf("expected nil servers for malformed apm.yml, got %v", servers)
	}
	if len(diags) != 1 || !strings.Contains(diags[0], "acme/broken") {
		t.Errorf("expected one diagnostic naming the dep, got %v", diags)
	}
}

func TestRun_MCPCollection_DirectDepAutoTrusted(t *testing.T) {
	dir := t.TempDir()
	depKey := "acme/direct"
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", depKey), "    - name: trusted\n      registry: false\n      transport: stdio\n      command: trusted-cmd\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: depKey, Owner: "acme", Repo: "direct", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{{Key: depKey, RepoURL: depKey, Kind: resolver.KindGitSemver, Depth: 1}},
	}

	result, err := Run(nil, dir, m, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, d := range result.Diags {
		if strings.Contains(d, "trusted") && strings.Contains(d, "not auto-trusted") {
			t.Errorf("direct dep MCP server should be auto-trusted, got diag: %s", d)
		}
	}

	// Positive assertion: the direct dep's self-defined server actually
	// becomes a TypeMCP Primitive (not just "no negative diagnostic").
	modulePath := filepath.Join(dir, "apm_modules", depKey)
	servers, loadDiags := loadDependencyMCP(depKey, modulePath)
	if len(loadDiags) != 0 {
		t.Fatalf("unexpected load diagnostics: %v", loadDiags)
	}
	prims, collectDiags := collectMCPPrimitives(servers, "dependency:"+depKey, depKey)
	if len(collectDiags) != 0 {
		t.Errorf("unexpected collect diagnostics: %v", collectDiags)
	}
	if len(prims) != 1 || prims[0].Name != "trusted" || prims[0].Type != TypeMCP || prims[0].MCP == nil {
		t.Fatalf("expected one TypeMCP primitive named %q, got %+v", "trusted", prims)
	}
}
