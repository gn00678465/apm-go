package deploy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/pelletier/go-toml/v2"
)

func mcpPrim(source string, s *manifest.MCPDependency) Primitive {
	return Primitive{Name: s.Name, Type: TypeMCP, Source: source, MCP: s}
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// ── antigravity: bake mode, oracle-driven field names (AC7) ──

func TestWriteMCP_Antigravity_MatchesOracleDescriptor(t *testing.T) {
	exp := loadOracle(t, "antigravity")
	if exp.MCP == nil {
		t.Skip("oracle has no mcp descriptor for antigravity")
	}

	dir := t.TempDir()
	t.Setenv("MCP_TEST_TOKEN", "secret123")
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "stdio-server", Registry: false, Transport: "stdio",
			Command: "my-server", Args: &[]string{"--flag"},
			Env: map[string]string{"TOKEN": "${MCP_TEST_TOKEN}"},
		}),
		mcpPrim("local", &manifest.MCPDependency{
			Name: "http-server", Registry: false, Transport: "http",
			URL: "https://api.example.com/mcp",
		}),
	}

	files, _, diags, err := (&antigravityAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	if len(files) != 1 || files[0] != exp.MCP.File {
		t.Fatalf("files = %v, want [%s]", files, exp.MCP.File)
	}

	root := readJSON(t, filepath.Join(dir, filepath.FromSlash(exp.MCP.File)))
	servers, ok := root[exp.MCP.Key].(map[string]any)
	if !ok {
		t.Fatalf("root[%q] missing or not a map: %v", exp.MCP.Key, root)
	}

	stdio, ok := servers["stdio-server"].(map[string]any)
	if !ok {
		t.Fatalf("stdio-server entry missing: %v", servers)
	}
	if stdio["command"] != "my-server" {
		t.Errorf("command = %v", stdio["command"])
	}
	if env, ok := stdio["env"].(map[string]any); !ok || env["TOKEN"] != "secret123" {
		t.Errorf("env.TOKEN not resolved: %v", stdio["env"])
	}

	http, ok := servers["http-server"].(map[string]any)
	if !ok {
		t.Fatalf("http-server entry missing: %v", servers)
	}
	if http[exp.MCP.HTTPField] != "https://api.example.com/mcp" {
		t.Errorf("%s = %v, want the resolved URL (oracle field name)", exp.MCP.HTTPField, http[exp.MCP.HTTPField])
	}
}

func TestWriteMCP_Antigravity_SSEUsesURLField(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "sse-server", Registry: false, Transport: "sse", URL: "https://api.example.com/sse"}),
	}
	if _, _, diags, err := (&antigravityAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	root := readJSON(t, filepath.Join(dir, ".agents", "mcp_config.json"))
	servers := root["mcpServers"].(map[string]any)
	entry := servers["sse-server"].(map[string]any)
	if entry["url"] != "https://api.example.com/sse" {
		t.Errorf("sse entry should use 'url' not 'serverUrl': %v", entry)
	}
	if _, hasServerURL := entry["serverUrl"]; hasServerURL {
		t.Errorf("sse entry must not set serverUrl: %v", entry)
	}
}

// ── claude: bake mode ──

func TestWriteMCP_Claude_StdioAndHTTP(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "s1", Registry: false, Transport: "stdio", Command: "cmd"}),
		mcpPrim("local", &manifest.MCPDependency{Name: "s2", Registry: false, Transport: "http", URL: "https://x/mcp"}),
	}
	files, _, diags, err := (&claudeAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	if len(files) != 1 || files[0] != ".mcp.json" {
		t.Fatalf("files = %v", files)
	}
	root := readJSON(t, filepath.Join(dir, ".mcp.json"))
	servers := root["mcpServers"].(map[string]any)
	s1 := servers["s1"].(map[string]any)
	if s1["type"] != "stdio" || s1["command"] != "cmd" {
		t.Errorf("s1 = %v", s1)
	}
	s2 := servers["s2"].(map[string]any)
	if s2["type"] != "http" || s2["url"] != "https://x/mcp" {
		t.Errorf("s2 = %v", s2)
	}
}

// ── codex: TOML, SSE unsupported ──

func TestWriteMCP_Codex_TOMLShapeAndSSESkipped(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "stdio1", Registry: false, Transport: "stdio", Command: "cmd", Args: &[]string{"-y"}}),
		mcpPrim("local", &manifest.MCPDependency{Name: "http1", Registry: false, Transport: "http", URL: "https://x/mcp", Headers: map[string]string{"Authorization": "Bearer tok"}}),
		mcpPrim("local", &manifest.MCPDependency{Name: "sse1", Registry: false, Transport: "sse", URL: "https://x/sse"}),
	}
	files, _, diags, err := (&codexAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	if len(files) != 1 || files[0] != ".codex/config.toml" {
		t.Fatalf("files = %v", files)
	}
	foundSkip := false
	for _, d := range diags {
		if strings.Contains(d, "sse1") && strings.Contains(d, "SSE") {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Errorf("expected SSE-skip diagnostic for sse1, got: %v", diags)
	}

	data, err := os.ReadFile(filepath.Join(dir, ".codex", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := toml.Unmarshal(data, &root); err != nil {
		t.Fatalf("invalid TOML written: %v\n%s", err, data)
	}
	servers := root["mcp_servers"].(map[string]any)
	if _, ok := servers["sse1"]; ok {
		t.Errorf("sse1 should not be written: %v", servers)
	}
	http1 := servers["http1"].(map[string]any)
	if http1["url"] != "https://x/mcp" || http1["id"] != "http1" {
		t.Errorf("http1 = %v", http1)
	}
	headers, ok := http1["http_headers"].(map[string]any)
	if !ok || headers["Authorization"] != "Bearer tok" {
		t.Errorf("http1.http_headers = %v", http1["http_headers"])
	}
}

// ── copilot: translate mode ──

func TestWriteMCP_Copilot_TranslateVerbatimAndLiteralRewrite(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "s1", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{
				"ALREADY_PLACEHOLDER": "${env:SOME_TOKEN}",
				"AUTHORED_LITERAL":    "sk-literal-secret",
			},
		}),
	}
	files, _, diags, err := (&copilotAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	if len(files) != 1 || files[0] != ".github/mcp-config.json" {
		t.Fatalf("files = %v", files)
	}
	root := readJSON(t, filepath.Join(dir, ".github", "mcp-config.json"))
	servers := root["mcpServers"].(map[string]any)
	s1 := servers["s1"].(map[string]any)
	env := s1["env"].(map[string]any)
	if env["ALREADY_PLACEHOLDER"] != "${env:SOME_TOKEN}" {
		t.Errorf("existing placeholder should pass through verbatim, got %v", env["ALREADY_PLACEHOLDER"])
	}
	if env["AUTHORED_LITERAL"] != "${AUTHORED_LITERAL}" {
		t.Errorf("authored literal should be rewritten to ${AUTHORED_LITERAL}, got %v (secret must not bake into translate-mode config)", env["AUTHORED_LITERAL"])
	}
}

func TestWriteMCP_Copilot_InputRefusedInBakeButPreservedInTranslate(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "s1", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{"KEY": "${input:api-key}"},
		}),
	}
	_, _, _, err := (&copilotAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	root := readJSON(t, filepath.Join(dir, ".github", "mcp-config.json"))
	servers := root["mcpServers"].(map[string]any)
	s1, ok := servers["s1"].(map[string]any)
	if !ok {
		t.Fatal("translate mode must still write the server (${input:} preserved for runtime)")
	}
	if s1["env"].(map[string]any)["KEY"] != "${input:api-key}" {
		t.Errorf("KEY = %v, want verbatim ${input:api-key}", s1["env"])
	}
}

func TestWriteMCP_Claude_InputRefusesServerInBakeMode(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "s1", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{"KEY": "${input:api-key}"},
		}),
		mcpPrim("local", &manifest.MCPDependency{Name: "s2", Registry: false, Transport: "stdio", Command: "cmd2"}),
	}
	files, _, diags, err := (&claudeAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	root := readJSON(t, filepath.Join(dir, ".mcp.json"))
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["s1"]; ok {
		t.Errorf("s1 (unresolved input:) must be refused, got: %v", servers)
	}
	if _, ok := servers["s2"]; !ok {
		t.Errorf("s2 should still be written, got: %v", servers)
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d, "s1") && strings.Contains(d, "refused") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected refuse diagnostic for s1, got: %v", diags)
	}
}

// ── shared: non-https skip ──

func TestWriteMCP_NonHTTPSRemoteSkipped(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "insecure", Registry: false, Transport: "http", URL: "http://plain.example.com/mcp"}),
	}
	files, _, diags, err := (&claudeAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	// The file is still written (with an empty mcpServers) even though the
	// only declared server was skipped: a redeploy must not leave a stale
	// entry from a PRIOR successful run sitting in the config untouched.
	if len(files) != 1 {
		t.Fatalf("files = %v, want [.mcp.json]", files)
	}
	root := readJSON(t, filepath.Join(dir, ".mcp.json"))
	if servers, ok := root["mcpServers"].(map[string]any); !ok || len(servers) != 0 {
		t.Errorf("mcpServers = %v, want empty map", root["mcpServers"])
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d, "insecure") && strings.Contains(d, "non-https") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected non-https diagnostic, got: %v", diags)
	}
}

// ── merge: preserves existing entries and foreign keys ──

func TestWriteMCP_MergePreservesForeignKeysAndOtherServers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	preexisting := `{"mcpServers":{"untouched":{"type":"stdio","command":"kept"}},"otherTopLevelKey":"kept-too"}`
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(preexisting), 0644); err != nil {
		t.Fatal(err)
	}

	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "new-server", Registry: false, Transport: "stdio", Command: "new"}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}

	root := readJSON(t, path)
	if root["otherTopLevelKey"] != "kept-too" {
		t.Errorf("foreign top-level key lost: %v", root)
	}
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["untouched"]; !ok {
		t.Errorf("preexisting server lost: %v", servers)
	}
	if _, ok := servers["new-server"]; !ok {
		t.Errorf("new server not written: %v", servers)
	}
}

// ── AC8: multi-source provenance + single hash for a merged file ──

func TestRun_MCP_MultiSourceProvenanceAndSingleHash(t *testing.T) {
	dir := t.TempDir()
	depA, depB := "acme/a", "acme/b"
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", depA), "    - name: from-a\n      registry: false\n      transport: stdio\n      command: a-cmd\n")
	writeDepManifestWithMCP(t, filepath.Join(dir, "apm_modules", depB), "    - name: from-b\n      registry: false\n      transport: stdio\n      command: b-cmd\n")

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

	result, err := Run([]string{"claude"}, dir, m, resolved)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(result.MCPFiles) != 1 {
		t.Fatalf("MCPFiles = %v, want exactly 1 hashed file", result.MCPFiles)
	}
	hash, ok := result.MCPFiles[".mcp.json"]
	if !ok || hash == "" {
		t.Fatalf("expected .mcp.json hash, got %v", result.MCPFiles)
	}

	if len(result.MCPProvenance) != 2 {
		t.Fatalf("MCPProvenance = %+v, want 2 entries", result.MCPProvenance)
	}
	bySource := map[string]string{}
	for _, p := range result.MCPProvenance {
		if p.File != ".mcp.json" {
			t.Errorf("provenance file = %q, want .mcp.json: %+v", p.File, p)
		}
		bySource[p.Server] = p.Source
	}
	if bySource["from-a"] != "dependency:"+depA {
		t.Errorf("from-a source = %q, want dependency:%s", bySource["from-a"], depA)
	}
	if bySource["from-b"] != "dependency:"+depB {
		t.Errorf("from-b source = %q, want dependency:%s", bySource["from-b"], depB)
	}
}

// ── redeploy correctness: a server that WAS written on a prior run must not
// silently survive once it's refused/skipped or one of its managed fields is
// omitted -- shallow-merge foreign-key preservation must not resurrect stale
// managed state (codex Review Gate B, HIGH + MEDIUM).

func TestWriteMCP_Redeploy_RefusedServerIsRemoved(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REDEPLOY_TOKEN", "secret")

	first := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "flaky", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{"TOKEN": "${REDEPLOY_TOKEN}"},
		}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(first, dir); err != nil {
		t.Fatalf("first WriteMCP: %v (diags=%v)", err, diags)
	}
	root := readJSON(t, filepath.Join(dir, ".mcp.json"))
	if _, ok := root["mcpServers"].(map[string]any)["flaky"]; !ok {
		t.Fatal("setup: server should be written on first run")
	}

	// Second run: same server name, now refused (unresolved ${input:}).
	second := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "flaky", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{"TOKEN": "${input:now-required}"},
		}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(second, dir); err != nil {
		t.Fatalf("second WriteMCP: %v (diags=%v)", err, diags)
	}
	root = readJSON(t, filepath.Join(dir, ".mcp.json"))
	if _, ok := root["mcpServers"].(map[string]any)["flaky"]; ok {
		t.Errorf("refused server from a prior successful run must be removed, got: %v", root["mcpServers"])
	}
}

func TestWriteMCP_Redeploy_OmittedEnvKeyIsDropped(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("REDEPLOY_TOKEN2", "secret")

	first := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "s1", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{"TOKEN": "${REDEPLOY_TOKEN2}"},
		}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(first, dir); err != nil {
		t.Fatalf("first WriteMCP: %v (diags=%v)", err, diags)
	}
	root := readJSON(t, filepath.Join(dir, ".mcp.json"))
	env := root["mcpServers"].(map[string]any)["s1"].(map[string]any)["env"].(map[string]any)
	if env["TOKEN"] != "secret" {
		t.Fatalf("setup: TOKEN should resolve on first run, got %v", env["TOKEN"])
	}

	// Second run: TOKEN's var is now undefined -> env-dict undefined = omit,
	// not refuse, so the server is still written but the key is dropped.
	os.Unsetenv("REDEPLOY_TOKEN2")
	second := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "s1", Registry: false, Transport: "stdio", Command: "cmd",
			Env: map[string]string{"TOKEN": "${REDEPLOY_TOKEN2}"},
		}),
	}
	if _, _, diags, err := (&claudeAdapter{}).WriteMCP(second, dir); err != nil {
		t.Fatalf("second WriteMCP: %v (diags=%v)", err, diags)
	}
	root = readJSON(t, filepath.Join(dir, ".mcp.json"))
	s1 := root["mcpServers"].(map[string]any)["s1"].(map[string]any)
	if env, ok := s1["env"].(map[string]any); ok {
		if _, stillPresent := env["TOKEN"]; stillPresent {
			t.Errorf("stale TOKEN from prior run must not survive an omit, got env=%v", env)
		}
	}
}

func TestWriteMCP_MalformedExistingFileErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".mcp.json")
	if err := os.WriteFile(path, []byte("{not valid json"), 0644); err != nil {
		t.Fatal(err)
	}

	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{Name: "s1", Registry: false, Transport: "stdio", Command: "cmd"}),
	}
	_, _, _, err := (&claudeAdapter{}).WriteMCP(prims, dir)
	if err == nil {
		t.Fatal("expected an error, not a silent overwrite of a malformed existing file")
	}

	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if string(after) != "{not valid json" {
		t.Errorf("malformed file must be left untouched on error, got: %s", after)
	}
}

// ── registry-backed servers never reach a writer (belt-and-suspenders on
// the collection-layer filter from Step 3; writers assume self-defined-only) ──

// ── end-to-end through Run(): pass --target antigravity explicitly so this
// test doesn't depend on the tempdir's auto-detection state.

func TestRun_MCP_AntigravityExplicitTargetEndToEnd(t *testing.T) {
	dir := t.TempDir()
	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		MCPServers: []*manifest.MCPDependency{
			{Name: "e2e-server", Registry: false, Transport: "stdio", Command: "e2e-cmd"},
		},
	}

	targets, _ := ResolveTargets("antigravity", nil, dir)
	if len(targets) != 1 || targets[0] != "antigravity" {
		t.Fatalf("ResolveTargets with explicit flag = %v", targets)
	}

	result, err := Run(targets, dir, m, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.MCPFiles[".agents/mcp_config.json"] == "" {
		t.Fatalf("expected .agents/mcp_config.json hash in MCPFiles, got %v", result.MCPFiles)
	}
	root := readJSON(t, filepath.Join(dir, ".agents", "mcp_config.json"))
	servers := root["mcpServers"].(map[string]any)
	if _, ok := servers["e2e-server"]; !ok {
		t.Errorf("e2e-server not written via full Run() pipeline: %v", servers)
	}
}

func TestWriteMCP_NoServersProducesNoFiles(t *testing.T) {
	dir := t.TempDir()
	files, _, diags, err := (&claudeAdapter{}).WriteMCP(nil, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	if len(files) != 0 || len(diags) != 0 {
		t.Errorf("files=%v diags=%v, want both empty", files, diags)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); !os.IsNotExist(err) {
		t.Error("no file should be created when there are no servers")
	}
}

// ── permission enforcement: os.WriteFile only applies perm on file creation,
// not on rewrite of an existing file (POSIX open() semantics) -- a bake-mode
// config can embed resolved secret values verbatim, so 0600 must be enforced
// on every redeploy, not just the first. Skipped on Windows: os.Chmod there
// only toggles the read-only attribute and cannot distinguish 0600 from 0644.

func TestWriteMCP_RedeployTightensPermissionOnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits are not meaningfully testable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, ".mcp.json")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Simulate a pre-existing file left at a looser mode (e.g. git checkout).
	if err := os.WriteFile(path, []byte(`{"mcpServers":{}}`), 0644); err != nil {
		t.Fatal(err)
	}

	prims := []Primitive{mcpPrim("local", &manifest.MCPDependency{
		Name: "s1", Registry: false, Transport: "stdio", Command: "cmd",
	})}
	if _, _, _, err := (&claudeAdapter{}).WriteMCP(prims, dir); err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Errorf(".mcp.json permission = %o, want 0600 (existing 0644 file must be tightened on redeploy)", got)
	}
}

// ── translate-mode remote URL: an unresolved placeholder is preserved
// verbatim by mf-013 (design.md D4), so the shared non-https guard must defer
// to copilot's own runtime resolution rather than dropping the server ──

func TestWriteMCP_Copilot_TranslatePlaceholderURLNotSkippedByHTTPSGuard(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{
		mcpPrim("local", &manifest.MCPDependency{
			Name: "remote-http", Registry: false, Transport: "http", URL: "${input:mcp-url}",
		}),
		mcpPrim("local", &manifest.MCPDependency{
			Name: "remote-sse", Registry: false, Transport: "sse", URL: "${input:mcp-url}",
		}),
		mcpPrim("local", &manifest.MCPDependency{
			Name: "remote-stream", Registry: false, Transport: "streamable-http", URL: "${input:mcp-url}",
		}),
	}
	_, _, diags, err := (&copilotAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v (diags=%v)", err, diags)
	}
	root := readJSON(t, filepath.Join(dir, ".github", "mcp-config.json"))
	servers, _ := root["mcpServers"].(map[string]any)
	for _, name := range []string{"remote-http", "remote-sse", "remote-stream"} {
		s, ok := servers[name].(map[string]any)
		if !ok {
			t.Fatalf("%s was dropped by the https guard instead of deferring to runtime: diags=%v, servers=%v", name, diags, servers)
		}
		if s["url"] != "${input:mcp-url}" {
			t.Errorf("%s url = %v, want verbatim ${input:mcp-url}", name, s["url"])
		}
	}
}

func TestWriteMCP_Copilot_TranslateLiteralHTTPStillSkipped(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{mcpPrim("local", &manifest.MCPDependency{
		Name: "insecure", Registry: false, Transport: "http", URL: "http://plain.example.com/mcp",
	})}
	_, _, diags, err := (&copilotAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	root := readJSON(t, filepath.Join(dir, ".github", "mcp-config.json"))
	servers, _ := root["mcpServers"].(map[string]any)
	if _, ok := servers["insecure"]; ok {
		t.Errorf("a literal (non-placeholder) http:// URL must still be skipped in translate mode: servers=%v", servers)
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d, "insecure") && strings.Contains(d, "non-https") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected non-https diagnostic, got: %v", diags)
	}
}

func TestWriteMCP_Copilot_TranslateLiteralHTTPWithPlaceholderPathStillSkipped(t *testing.T) {
	dir := t.TempDir()
	prims := []Primitive{mcpPrim("local", &manifest.MCPDependency{
		Name: "insecure", Registry: false, Transport: "http", URL: "http://plain.example.com/${input:path}",
	})}
	_, _, diags, err := (&copilotAdapter{}).WriteMCP(prims, dir)
	if err != nil {
		t.Fatalf("WriteMCP: %v", err)
	}
	root := readJSON(t, filepath.Join(dir, ".github", "mcp-config.json"))
	servers, _ := root["mcpServers"].(map[string]any)
	if _, ok := servers["insecure"]; ok {
		t.Errorf("literal http:// scheme must still be skipped even when the path has a runtime placeholder: servers=%v", servers)
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d, "insecure") && strings.Contains(d, "non-https") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected non-https diagnostic, got: %v", diags)
	}
}
