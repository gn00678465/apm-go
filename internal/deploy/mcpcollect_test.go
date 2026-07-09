package deploy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
)

// mockMCPRegistryServer is a minimal MCP Registry v0.1 stand-in, mirroring
// cmd/apm's mcpRegistryServer test helper (kept as a separate copy since Go
// doesn't share unexported test helpers across packages).
func mockMCPRegistryServer(t *testing.T, name string, remotes []map[string]any) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"servers": []map[string]any{
			{"server": map[string]any{"name": name, "remotes": remotes}},
		}})
	})
	mux.HandleFunc("/v0.1/servers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"server": map[string]any{"name": name, "remotes": remotes}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

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

	result, err := Run(nil, dir, m, resolved, nil)
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

	result, err := Run(nil, dir, m, resolved, nil)
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

	result, err := Run(nil, dir, m, resolved, nil)
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

// TestRun_MCPCollection_DevDependencySelfDefinedAutoTrusted is the F3
// deploy-parity test: a devDependencies.apm entry is a DIRECT (depth-1)
// dependency exactly like a dependencies.apm entry, so its own self-defined
// MCP server must be auto-trusted the same way -- not routed through the
// "transitive, never auto-trusted" bucket just because deploy.Run's direct-
// dep loop used to only scan m.ParsedDeps.
func TestRun_MCPCollection_DevDependencySelfDefinedAutoTrusted(t *testing.T) {
	dir := t.TempDir()
	devKey := "acme/devtool"
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", devKey), "    - name: dev-server\n      registry: false\n      transport: stdio\n      command: dev-cmd\n")

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDevDeps: []*manifest.DependencyReference{
			{RepoURL: devKey, Owner: "acme", Repo: "devtool", Source: "git"},
		},
	}
	resolved := &resolver.ResolutionResult{
		Deps: []resolver.ResolvedDep{
			{Key: devKey, RepoURL: devKey, Kind: resolver.KindGitSemver, Depth: 1},
		},
	}

	result, err := Run(nil, dir, m, resolved, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, d := range result.Diags {
		if strings.Contains(d, `mcp "dev-server"`) && strings.Contains(d, "not auto-trusted") {
			t.Fatalf("dev dependency's self-defined MCP server was routed through the transitive (not-auto-trusted) path, got diag: %q (all diags: %v)", d, result.Diags)
		}
	}
}

func TestCollectMCPPrimitives_RegistryBackedResolvedLive(t *testing.T) {
	srv := mockMCPRegistryServer(t, "from-registry", []map[string]any{
		{"type": "http", "url": "https://resolved.example.com/mcp"},
	})
	servers := []*manifest.MCPDependency{
		{Name: "from-registry", Registry: srv.URL, Transport: "http"},
	}

	prims, diags := collectMCPPrimitives(servers, "local", "")
	if len(diags) != 0 {
		t.Errorf("unexpected diagnostics: %v", diags)
	}
	if len(prims) != 1 || prims[0].Name != "from-registry" || prims[0].Type != TypeMCP {
		t.Fatalf("expected one resolved TypeMCP primitive, got %+v", prims)
	}
	if prims[0].MCP == nil || prims[0].MCP.URL != "https://resolved.example.com/mcp" {
		t.Errorf("expected resolved dep URL from registry, got %+v", prims[0].MCP)
	}
}

func TestCollectMCPPrimitives_RegistryBackedNotFound_DiagnosedNotFatal(t *testing.T) {
	srv := mockMCPRegistryServer(t, "other-server", nil)
	servers := []*manifest.MCPDependency{
		{Name: "missing-server", Registry: srv.URL},
	}

	prims, diags := collectMCPPrimitives(servers, "local", "")
	if len(prims) != 0 {
		t.Errorf("expected no primitives for an unresolvable entry, got %+v", prims)
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d, `mcp "missing-server"`) && strings.Contains(d, "not found in registry") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a not-found diagnostic, got diags: %v", diags)
	}
}

func TestCollectMCPPrimitives_RegistryBackedRequiredHeaders_Diagnosed(t *testing.T) {
	srv := mockMCPRegistryServer(t, "needs-auth", []map[string]any{
		{"type": "http", "url": "https://auth.example.com/mcp", "headers": []map[string]any{{"name": "Authorization"}}},
	})
	servers := []*manifest.MCPDependency{
		{Name: "needs-auth", Registry: srv.URL},
	}

	prims, diags := collectMCPPrimitives(servers, "local", "")
	if len(prims) != 1 {
		t.Fatalf("expected the server to still resolve and deploy, got %+v", prims)
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d, `mcp "needs-auth"`) && strings.Contains(d, "Authorization") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a required-headers diagnostic, got diags: %v", diags)
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

// TestLoadDependencyDeps_MissingFileIsSilent mirrors
// TestLoadDependencyMCP_MissingFileIsSilent's lenience contract for
// LoadDependencyDeps: no apm.yml means "no dependencies", not an error.
func TestLoadDependencyDeps_MissingFileIsSilent(t *testing.T) {
	dir := t.TempDir()
	keys, diags := LoadDependencyDeps("acme/none", filepath.Join(dir, "apm_modules", "acme/none"))
	if keys != nil || diags != nil {
		t.Errorf("missing apm.yml should be silent, got keys=%v diags=%v", keys, diags)
	}
}

// TestLoadDependencyDeps_MalformedFileDiagnosed mirrors
// TestLoadDependencyMCP_MalformedFileDiagnosed.
func TestLoadDependencyDeps_MalformedFileDiagnosed(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/broken")
	mkFile(t, modDir, "apm.yml", "not: [valid, yaml, manifest\n")
	keys, diags := LoadDependencyDeps("acme/broken", modDir)
	if keys != nil {
		t.Errorf("expected nil keys for malformed apm.yml, got %v", keys)
	}
	if len(diags) != 1 || !strings.Contains(diags[0], "acme/broken") {
		t.Errorf("expected one diagnostic naming the dep, got %v", diags)
	}
}

// TestLoadDependencyDeps_ReturnsProdIdentityKeysIgnoringRefAndDev locks down
// CRITICAL #1's fix prerequisite: LoadDependencyDeps returns identity keys
// (ignoring git ref, matching DependencyReference.IdentityKey()) for a
// dependency's own PROD dependencies.apm entries only -- devDependencies.apm
// is never followed, matching deploy.Run's own transitive depth split.
func TestLoadDependencyDeps_ReturnsProdIdentityKeysIgnoringRefAndDev(t *testing.T) {
	dir := t.TempDir()
	modDir := filepath.Join(dir, "apm_modules", "acme/parent")
	mkFile(t, modDir, "apm.yml", "name: parent\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/child#v1.2.3\ndevDependencies:\n  apm:\n    - acme/dev-only\n")

	keys, diags := LoadDependencyDeps("acme/parent", modDir)
	if len(diags) != 0 {
		t.Errorf("expected no diagnostics, got %v", diags)
	}
	if len(keys) != 1 || keys[0] != "acme/child" {
		t.Errorf("expected exactly [acme/child] (ref stripped, dev dep excluded), got %v", keys)
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

	result, err := Run(nil, dir, m, resolved, nil)
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
