package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// ── validateMCPConflicts ──

func TestValidateMCPConflicts(t *testing.T) {
	base := func() mcpInstallOpts { return mcpInstallOpts{Name: "my-server"} }

	tests := []struct {
		name    string
		mutate  func(o *mcpInstallOpts)
		wantErr string
	}{
		{"empty name", func(o *mcpInstallOpts) { o.Name = "" }, "requires a server name"},
		{"name starts with dash", func(o *mcpInstallOpts) { o.Name = "-oops" }, "cannot start with '-'"},
		{"positional packages", func(o *mcpInstallOpts) { o.PrePackages = []string{"acme/foo"} }, "positional packages"},
		{"skill combined", func(o *mcpInstallOpts) { o.SkillSubset = []string{"x"} }, "cannot be combined with --mcp"},
		{"header without url", func(o *mcpInstallOpts) { o.HeaderPairs = []string{"K=V"} }, "--header requires --url"},
		{"env with url", func(o *mcpInstallOpts) { o.EnvPairs = []string{"K=V"}; o.URL = "https://x" }, "--env applies to stdio"},
		{"env without command or url (registry lookup)", func(o *mcpInstallOpts) { o.EnvPairs = []string{"K=V"} }, "--env applies to stdio"},
		{"url and stdio command", func(o *mcpInstallOpts) { o.URL = "https://x"; o.Command = []string{"npx"} }, "both --url and a stdio command"},
		{"stdio transport with url", func(o *mcpInstallOpts) { o.Transport = "stdio"; o.URL = "https://x" }, "stdio transport doesn't accept --url"},
		{"stdio transport with neither url nor command", func(o *mcpInstallOpts) { o.Transport = "stdio" }, "requires a stdio command"},
		{"remote transport with stdio", func(o *mcpInstallOpts) { o.Transport = "http"; o.Command = []string{"npx"} }, "don't accept a stdio command"},
		{"registry with url", func(o *mcpInstallOpts) { o.Registry = "https://reg"; o.URL = "https://x" }, "only applies to registry-resolved"},
		{"registry with stdio", func(o *mcpInstallOpts) { o.Registry = "https://reg"; o.Command = []string{"npx"} }, "only applies to registry-resolved"},
		{"mcp-version with url", func(o *mcpInstallOpts) { o.Version = "1.0.0"; o.URL = "https://x" }, "--mcp-version only applies to registry-resolved"},
		{"mcp-version with stdio", func(o *mcpInstallOpts) { o.Version = "1.0.0"; o.Command = []string{"npx"} }, "--mcp-version only applies to registry-resolved"},
		{"unknown transport", func(o *mcpInstallOpts) { o.Transport = "grpc" }, "unknown MCP transport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := base()
			tt.mutate(&o)
			err := validateMCPConflicts(o)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("got %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestValidateMCPConflicts_ValidCombinations(t *testing.T) {
	valid := []mcpInstallOpts{
		{Name: "bare-registry-lookup"},
		{Name: "with-transport", Transport: "http"},
		{Name: "self-defined-url", URL: "https://example.com/mcp", Transport: "http", HeaderPairs: []string{"Authorization=Bearer x"}},
		{Name: "self-defined-stdio", Command: []string{"npx", "-y", "pkg"}, EnvPairs: []string{"TOKEN=abc"}},
		{Name: "registry-pinned", Version: "1.2.3"},
		{Name: "custom-registry", Registry: "https://reg.example.com"},
	}
	for _, o := range valid {
		if err := validateMCPConflicts(o); err != nil {
			t.Errorf("%+v: unexpected error: %v", o, err)
		}
	}
}

// buildMCPEntryForTest mirrors runMCPInstall's persist-then-deploy sequence
// for tests that want both results together in one call. Production splits
// these into buildPersistEntry (cheap, no network) and buildDeployDep
// (network-bound for a registry lookup) so runMCPInstall can skip the
// network call entirely when the entry turns out unchanged.
func buildMCPEntryForTest(opts mcpInstallOpts) (entryNode *yamllib.Node, dep *manifest.MCPDependency, diags []string, err error) {
	entryNode, err = buildPersistEntry(opts)
	if err != nil {
		return nil, nil, nil, err
	}
	dep, diags, err = buildDeployDep(opts)
	if err != nil {
		return nil, nil, nil, err
	}
	return entryNode, dep, diags, nil
}

// ── buildMCPEntry: self-defined branches ──

// TestParseKVPairs_MalformedPair_NeverEchoesValue is a regression test
// (eighth codex review round): parseKVPairs backs both --env and --header,
// so a mistyped separator (e.g. --header "Authorization: Bearer secret"
// using ":" instead of "=") must not leak the value into the error message.
func TestParseKVPairs_MalformedPair_NeverEchoesValue(t *testing.T) {
	secret := "Authorization: Bearer super-secret-token"
	_, err := parseKVPairs([]string{secret})
	if err == nil {
		t.Fatal("expected an error for a pair with no '='")
	}
	if strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), "super-secret-token") {
		t.Errorf("error message leaked the value: %v", err)
	}
}

func TestBuildMCPEntry_SelfDefinedStdio(t *testing.T) {
	opts := mcpInstallOpts{Name: "fetch", Command: []string{"npx", "-y", "@modelcontextprotocol/server-fetch"}, EnvPairs: []string{"TOKEN=abc"}}
	entryNode, dep, diags, err := buildMCPEntryForTest(opts)
	if err != nil {
		t.Fatalf("buildMCPEntry: %v", err)
	}
	if len(diags) != 0 {
		t.Errorf("unexpected diags: %v", diags)
	}
	if dep.Transport != "stdio" || dep.Command != "npx" {
		t.Errorf("dep = %+v", dep)
	}
	if dep.Args == nil || len(*dep.Args) != 2 || (*dep.Args)[0] != "-y" {
		t.Errorf("Args = %v", dep.Args)
	}
	if dep.Env["TOKEN"] != "abc" {
		t.Errorf("Env = %v", dep.Env)
	}
	if got := nodeToValue(entryNode).(map[string]any)["command"]; got != "npx" {
		t.Errorf("persisted command = %v", got)
	}
}

func TestBuildMCPEntry_SelfDefinedURL(t *testing.T) {
	opts := mcpInstallOpts{Name: "api", URL: "https://example.com/mcp", HeaderPairs: []string{"Authorization=Bearer x"}}
	entryNode, dep, _, err := buildMCPEntryForTest(opts)
	if err != nil {
		t.Fatalf("buildMCPEntry: %v", err)
	}
	if dep.Transport != "http" || dep.URL != "https://example.com/mcp" {
		t.Errorf("dep = %+v", dep)
	}
	if dep.Headers["Authorization"] != "Bearer x" {
		t.Errorf("Headers = %v", dep.Headers)
	}
	v := nodeToValue(entryNode).(map[string]any)
	if v["url"] != "https://example.com/mcp" || v["registry"] != false {
		t.Errorf("persisted = %v", v)
	}
}

func TestBuildMCPEntry_SelfDefinedURL_InvalidTransport(t *testing.T) {
	// stdio transport requires 'command', so URL+stdio (which validateMCPConflicts
	// already forbids at the CLI layer) never reaches buildMCPEntry, but a bogus
	// transport for a url-based entry should fail ValidateMCP.
	opts := mcpInstallOpts{Name: "api", URL: "https://example.com/mcp", Transport: "carrier-pigeon"}
	if _, _, _, err := buildMCPEntryForTest(opts); err == nil {
		t.Fatal("expected ValidateMCP to reject an unknown transport")
	}
}

// ── buildMCPEntry: registry branch (httptest-mocked MCP Registry v0.1) ──

type mockMCPServer struct {
	Name    string
	Remotes []map[string]any
}

func mcpRegistryServer(t *testing.T, servers map[string]mockMCPServer) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("search")
		var entries []map[string]any
		for name, s := range servers {
			if strings.Contains(strings.ToLower(name), strings.ToLower(q)) {
				entries = append(entries, map[string]any{"server": map[string]any{"name": s.Name, "remotes": s.Remotes}})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"servers": entries})
	})
	mux.HandleFunc("/v0.1/servers/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/v0.1/servers/")
		parts := strings.SplitN(rest, "/versions/", 2)
		if len(parts) != 2 {
			http.NotFound(w, r)
			return
		}
		s, ok := servers[parts[0]]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"server": map[string]any{"name": s.Name, "remotes": s.Remotes}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestBuildMCPEntry_RegistryLookup_Success(t *testing.T) {
	// Non-interactive: the registry remote declares a required Authorization
	// header, so an interactive run would prompt for a token. Pin
	// non-interactive so this test deterministically exercises the fallback
	// guidance diagnostic (the interactive inject path is covered by the
	// collectHeaderValues unit tests).
	withNonInteractiveStdin(t)
	srv := mcpRegistryServer(t, map[string]mockMCPServer{
		"io.github.github/github-mcp-server": {
			Name: "io.github.github/github-mcp-server",
			Remotes: []map[string]any{
				{"type": "streamable-http", "url": "https://api.githubcopilot.com/mcp/",
					"headers": []map[string]any{{"name": "Authorization"}}},
			},
		},
	})

	opts := mcpInstallOpts{Name: "io.github.github/github-mcp-server", Transport: "http", Registry: srv.URL}
	entryNode, dep, diags, err := buildMCPEntryForTest(opts)
	if err != nil {
		t.Fatalf("buildMCPEntry: %v", err)
	}
	// dep.Name must equal opts.Name verbatim (not a shortened slug of the
	// registry's canonical name) -- it is the SAME identity upsertMCPEntry
	// just checked apm.yml for; a derived short name would deploy under a
	// different key than what was conflict-checked, silently bypassing
	// --force against any unrelated entry that happens to slug-collide.
	if dep.Name != opts.Name {
		t.Errorf("deployed dep.Name = %q, want opts.Name verbatim %q", dep.Name, opts.Name)
	}
	if dep.URL != "https://api.githubcopilot.com/mcp/" {
		t.Errorf("dep.URL = %q", dep.URL)
	}
	if dep.Transport != "http" {
		t.Errorf("dep.Transport = %q, want http (opts.Transport overrides registry's streamable-http)", dep.Transport)
	}
	if len(diags) != 1 || !strings.Contains(diags[0], "Authorization") {
		t.Errorf("expected a header diagnostic naming Authorization, got %v", diags)
	}

	// Persisted entry must NOT carry the resolved url -- only what the user typed.
	v := nodeToValue(entryNode).(map[string]any)
	if v["name"] != opts.Name {
		t.Errorf("persisted name = %v, want %v", v["name"], opts.Name)
	}
	if _, hasURL := v["url"]; hasURL {
		t.Errorf("persisted entry must not carry the resolved url, got %v", v)
	}
	if v["transport"] != "http" {
		t.Errorf("persisted transport = %v", v["transport"])
	}
}

// TestBuildMCPEntry_RegistryLookup_RejectsCredentialedRemoteURL is a
// regression test (seventh codex review round): a registry response is not
// trusted any more than a self-defined --url -- a compromised or malicious
// registry entry returning a URL with embedded credentials must be
// rejected, not deployed verbatim into the target's MCP config file. This
// path previously bypassed the credential guard entirely (only
// buildSelfDefinedURLDep called ValidateMCP; resolveFromRegistry built its
// dep without it).
func TestBuildMCPEntry_RegistryLookup_RejectsCredentialedRemoteURL(t *testing.T) {
	srv := mcpRegistryServer(t, map[string]mockMCPServer{
		"acme/leaky": {
			Name:    "acme/leaky",
			Remotes: []map[string]any{{"type": "http", "url": "https://user:pass@evil.example.com/mcp"}},
		},
	})
	opts := mcpInstallOpts{Name: "acme/leaky", Registry: srv.URL}
	_, _, _, err := buildMCPEntryForTest(opts)
	if err == nil || !strings.Contains(err.Error(), "embedded credentials") {
		t.Errorf("expected a credentialed-remote-url rejection, got %v", err)
	}
}

// TestBuildMCPEntry_RegistryLookup_UsesMCPRegistryURLEnv is a regression
// test (codex review): resolveFromRegistry originally only looked at
// opts.Registry, silently ignoring an MCP_REGISTRY_URL env override when
// --registry was omitted -- so a configured private/enterprise registry was
// never consulted. Precedence must be --registry flag > MCP_REGISTRY_URL env.
func TestBuildMCPEntry_RegistryLookup_UsesMCPRegistryURLEnv(t *testing.T) {
	srv := mcpRegistryServer(t, map[string]mockMCPServer{
		"acme/server": {Name: "acme/server", Remotes: []map[string]any{{"type": "http", "url": "https://acme.example/mcp"}}},
	})
	t.Setenv("MCP_REGISTRY_URL", srv.URL)

	opts := mcpInstallOpts{Name: "acme/server"} // no --registry flag
	_, dep, _, err := buildMCPEntryForTest(opts)
	if err != nil {
		t.Fatalf("buildMCPEntry: %v", err)
	}
	if dep.URL != "https://acme.example/mcp" {
		t.Errorf("dep.URL = %q; MCP_REGISTRY_URL env override was not used", dep.URL)
	}
}

// TestBuildMCPEntry_RegistryLookup_RejectsStdioTransportOverride is a
// regression test (second codex review round): validateMCPConflicts is the
// only production caller that guards Transport=="stdio" from reaching a
// registry lookup, but resolveFromRegistry itself must also refuse a
// non-remote --transport override -- otherwise it would silently build a
// stdio-transport MCPDependency carrying a URL and no command, which
// writers would deploy as broken (empty command).
func TestBuildMCPEntry_RegistryLookup_RejectsStdioTransportOverride(t *testing.T) {
	srv := mcpRegistryServer(t, map[string]mockMCPServer{
		"acme/server": {Name: "acme/server", Remotes: []map[string]any{{"type": "http", "url": "https://acme.example/mcp"}}},
	})
	opts := mcpInstallOpts{Name: "acme/server", Transport: "stdio", Registry: srv.URL}
	_, _, _, err := buildMCPEntryForTest(opts)
	if err == nil || !strings.Contains(err.Error(), "not valid for a registry-resolved server") {
		t.Errorf("expected a stdio-transport-override rejection, got %v", err)
	}
}

func TestBuildMCPEntry_RegistryLookup_NotFound(t *testing.T) {
	srv := mcpRegistryServer(t, map[string]mockMCPServer{})
	opts := mcpInstallOpts{Name: "nonexistent/server", Registry: srv.URL}
	_, _, _, err := buildMCPEntryForTest(opts)
	if err == nil || !strings.Contains(err.Error(), "not found in registry") {
		t.Errorf("expected not-found error, got %v", err)
	}
	// The registry URL (srv.URL, an httptest address) must not appear in
	// the error -- a path-embedded token in a real --registry URL would
	// otherwise leak here (found in a follow-up codex review round).
	if strings.Contains(err.Error(), srv.URL) {
		t.Errorf("not-found error leaked the registry URL: %v", err)
	}
}

func TestBuildMCPEntry_RegistryLookup_PackagesOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"servers": []map[string]any{
			{"server": map[string]any{"name": "npm-only/server", "packages": []map[string]any{{"registry_name": "npm"}}}},
		}})
	})
	mux.HandleFunc("/v0.1/servers/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"server": map[string]any{"name": "npm-only/server", "packages": []map[string]any{{"registry_name": "npm"}}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	opts := mcpInstallOpts{Name: "npm-only/server", Registry: srv.URL}
	_, _, _, err := buildMCPEntryForTest(opts)
	if err == nil || !strings.Contains(err.Error(), "package-based (stdio)") {
		t.Errorf("expected package-based-only error, got %v", err)
	}
}

// TestBuildMCPEntry_RegistryLookup_NoRemotesNoPackages covers the third
// branch of resolveFromRegistry's remotes==0 check: a registry hit with
// neither remotes[] nor packages[] (distinct from the "package-based-only"
// case above) must still report a clear, non-nil error rather than build a
// dep with an empty URL.
func TestBuildMCPEntry_RegistryLookup_NoRemotesNoPackages(t *testing.T) {
	srv := mcpRegistryServer(t, map[string]mockMCPServer{
		"acme/empty": {Name: "acme/empty"},
	})
	opts := mcpInstallOpts{Name: "acme/empty", Registry: srv.URL}
	_, _, _, err := buildMCPEntryForTest(opts)
	if err == nil || !strings.Contains(err.Error(), "no deployable remote endpoint") {
		t.Errorf("expected a no-deployable-endpoint error, got %v", err)
	}
}

// TestBuildMCPEntry_RegistryLookup_UsesPinnedVersion is a coverage gap fix:
// --mcp-version's actual end-to-end effect (the version reaches the registry
// HTTP request, and the persisted apm.yml node records {name, version}) had
// no test beyond validateMCPConflicts' conflict-matrix checks.
func TestBuildMCPEntry_RegistryLookup_UsesPinnedVersion(t *testing.T) {
	var gotVersion string
	mux := http.NewServeMux()
	mux.HandleFunc("/v0.1/servers", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"servers": []map[string]any{
			{"server": map[string]any{"name": "acme/server", "remotes": []map[string]any{{"type": "http", "url": "https://acme.example/mcp"}}}},
		}})
	})
	mux.HandleFunc("/v0.1/servers/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/v0.1/servers/")
		if parts := strings.SplitN(rest, "/versions/", 2); len(parts) == 2 {
			gotVersion = parts[1]
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"server": map[string]any{"name": "acme/server", "remotes": []map[string]any{{"type": "http", "url": "https://acme.example/mcp"}}}})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	opts := mcpInstallOpts{Name: "acme/server", Version: "1.2.3", Registry: srv.URL}
	entryNode, dep, _, err := buildMCPEntryForTest(opts)
	if err != nil {
		t.Fatalf("buildMCPEntry: %v", err)
	}
	if gotVersion != "1.2.3" {
		t.Errorf("registry request used version %q, want %q", gotVersion, "1.2.3")
	}
	if dep.URL != "https://acme.example/mcp" {
		t.Errorf("dep.URL = %q", dep.URL)
	}

	v := nodeToValue(entryNode).(map[string]any)
	if v["name"] != "acme/server" || v["version"] != "1.2.3" {
		t.Errorf("persisted = %v", v)
	}
	if _, hasURL := v["url"]; hasURL {
		t.Errorf("persisted entry must not carry the resolved url, got %v", v)
	}
	if v["registry"] != srv.URL {
		t.Errorf("persisted registry = %v, want %v (the --registry flag value)", v["registry"], srv.URL)
	}
}

// ── buildRegistryPersistEntryNode: apm.yml shape per opts combination ──

func TestBuildRegistryPersistEntryNode_BareNameWhenNothingElseSet(t *testing.T) {
	node := buildRegistryPersistEntryNode(mcpInstallOpts{Name: "acme/server"}, "")
	if node.Kind != yamllib.ScalarNode || node.Value != "acme/server" {
		t.Errorf("expected a bare scalar name node, got kind=%v value=%q", node.Kind, node.Value)
	}
}

func TestBuildRegistryPersistEntryNode_VersionOnly(t *testing.T) {
	node := buildRegistryPersistEntryNode(mcpInstallOpts{Name: "acme/server", Version: "1.2.3"}, "")
	v := nodeToValue(node).(map[string]any)
	if v["name"] != "acme/server" || v["version"] != "1.2.3" {
		t.Errorf("persisted = %v", v)
	}
	if _, hasTransport := v["transport"]; hasTransport {
		t.Errorf("must not carry transport when --transport wasn't given, got %v", v)
	}
	if _, hasRegistry := v["registry"]; hasRegistry {
		t.Errorf("must not carry registry when none was effective, got %v", v)
	}
}

func TestBuildRegistryPersistEntryNode_VersionTransportAndRegistry(t *testing.T) {
	node := buildRegistryPersistEntryNode(mcpInstallOpts{Name: "acme/server", Version: "1.2.3", Transport: "http"}, "https://reg.example.com")
	v := nodeToValue(node).(map[string]any)
	if v["name"] != "acme/server" || v["version"] != "1.2.3" || v["transport"] != "http" || v["registry"] != "https://reg.example.com" {
		t.Errorf("persisted = %v", v)
	}
}

// ── upsertMCPEntry ──

func parseYAML(t *testing.T, src string) *yamllib.Node {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return node
}

func TestUpsertMCPEntry_Added(t *testing.T) {
	doc := parseYAML(t, "name: test\nversion: \"1.0.0\"\n")
	status, err := upsertMCPEntry(doc, "fetch", strNode("fetch"), false, nil)
	if err != nil {
		t.Fatalf("upsertMCPEntry: %v", err)
	}
	if status != "added" {
		t.Errorf("status = %q, want added", status)
	}
}

func TestUpsertMCPEntry_UnchangedDoesNotError(t *testing.T) {
	doc := parseYAML(t, "name: test\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - fetch\n")
	status, err := upsertMCPEntry(doc, "fetch", strNode("fetch"), false, nil)
	if err != nil {
		t.Fatalf("upsertMCPEntry: %v", err)
	}
	if status != "unchanged" {
		t.Errorf("status = %q, want unchanged", status)
	}
}

func TestUpsertMCPEntry_DifferentWithoutForce_Errors(t *testing.T) {
	doc := parseYAML(t, "name: test\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: fetch\n      transport: stdio\n")
	newEntry := mapNode([][2]*yamllib.Node{{strNode("name"), strNode("fetch")}, {strNode("transport"), strNode("http")}})
	_, err := upsertMCPEntry(doc, "fetch", newEntry, false, nil)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected a --force-required error, got %v", err)
	}
}

func TestUpsertMCPEntry_DifferentWithForce_Replaces(t *testing.T) {
	doc := parseYAML(t, "name: test\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: fetch\n      transport: stdio\n")
	newEntry := mapNode([][2]*yamllib.Node{{strNode("name"), strNode("fetch")}, {strNode("transport"), strNode("http")}})
	status, err := upsertMCPEntry(doc, "fetch", newEntry, true, nil)
	if err != nil {
		t.Fatalf("upsertMCPEntry: %v", err)
	}
	if status != "replaced" {
		t.Errorf("status = %q, want replaced", status)
	}
}

// TestUpsertMCPEntry_WritesBlockStyleNotInheritedFlow covers #1: `apm-go
// init` writes `mcp: []`, which go-yaml renders in flow style; the parsed
// SequenceNode carries FlowStyle, so a naive append + re-dump emits
// `mcp: [foo]`. upsertMCPEntry must clear that flow style on mutation so the
// serialized output matches the Python original's block style, while leaving
// the sibling `apm: []` (also flow) untouched.
func TestUpsertMCPEntry_WritesBlockStyleNotInheritedFlow(t *testing.T) {
	cases := []struct {
		name  string
		entry *yamllib.Node
		want  string // block-form substring that must appear
	}{
		{"bare string", strNode("io.github.github/github-mcp-server"), "- io.github.github/github-mcp-server"},
		{"mapping", mapNode([][2]*yamllib.Node{{strNode("name"), strNode("api")}, {strNode("transport"), strNode("http")}}), "- name: api"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := []byte("name: test\ndependencies:\n  apm: []\n  mcp: []\n")
			node := parseYAML(t, string(src))
			status, err := upsertMCPEntry(node, mcpEntryName(tc.entry), tc.entry, false, nil)
			if err != nil || status != "added" {
				t.Fatalf("upsertMCPEntry status=%q err=%v", status, err)
			}
			out, patched, err := yamlcore.PatchMappingPath(src, node, []string{"dependencies", "mcp"})
			if err != nil || !patched {
				t.Fatalf("PatchMappingPath patched=%v err=%v", patched, err)
			}
			got := string(out)
			if strings.Contains(got, "mcp: [") {
				t.Errorf("mcp written in flow style, want block:\n%s", got)
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("missing block entry %q in:\n%s", tc.want, got)
			}
			if !strings.Contains(got, "apm: []") {
				t.Errorf("sibling apm list must be left untouched, got:\n%s", got)
			}
		})
	}
}

// TestUpsertMCPEntry_ConflictConfirm covers D2: a differing existing entry
// without --force is resolved by the injected confirm callback (interactive
// TTY), mirroring the Python original's writer.py three-way. nil confirm
// (non-interactive) is a hard error; accept -> replaced; decline -> skipped
// (doc untouched); --force -> replaced without consulting confirm.
func TestUpsertMCPEntry_ConflictConfirm(t *testing.T) {
	const src = "name: test\ndependencies:\n  mcp:\n    - name: fetch\n      transport: stdio\n"
	base := func() *yamllib.Node { return parseYAML(t, src) }
	newEntry := func() *yamllib.Node {
		return mapNode([][2]*yamllib.Node{{strNode("name"), strNode("fetch")}, {strNode("transport"), strNode("http")}})
	}

	t.Run("accept -> replaced", func(t *testing.T) {
		doc := base()
		var gotDiff []string
		confirm := func(name string, diff []string) (bool, error) { gotDiff = diff; return true, nil }
		status, err := upsertMCPEntry(doc, "fetch", newEntry(), false, confirm)
		if err != nil || status != "replaced" {
			t.Fatalf("status=%q err=%v", status, err)
		}
		if len(gotDiff) == 0 {
			t.Errorf("confirm must receive a non-empty diff")
		}
		out, _ := yamlcore.SafeDump(doc)
		if !strings.Contains(string(out), "http") {
			t.Errorf("accepted replace must apply the new entry:\n%s", out)
		}
	})

	t.Run("decline -> skipped, doc untouched", func(t *testing.T) {
		doc := base()
		confirm := func(name string, diff []string) (bool, error) { return false, nil }
		status, err := upsertMCPEntry(doc, "fetch", newEntry(), false, confirm)
		if err != nil || status != "skipped" {
			t.Fatalf("status=%q err=%v", status, err)
		}
		out, _ := yamlcore.SafeDump(doc)
		if !strings.Contains(string(out), "stdio") || strings.Contains(string(out), "http") {
			t.Errorf("declined replace must leave the entry unchanged:\n%s", out)
		}
	})

	t.Run("nil confirm (non-interactive) -> error", func(t *testing.T) {
		doc := base()
		_, err := upsertMCPEntry(doc, "fetch", newEntry(), false, nil)
		if err == nil || !strings.Contains(err.Error(), "non-interactive") {
			t.Errorf("expected a non-interactive --force error, got %v", err)
		}
	})

	t.Run("force -> replaced without consulting confirm", func(t *testing.T) {
		doc := base()
		called := false
		confirm := func(name string, diff []string) (bool, error) { called = true; return false, nil }
		status, err := upsertMCPEntry(doc, "fetch", newEntry(), true, confirm)
		if err != nil || status != "replaced" {
			t.Fatalf("status=%q err=%v", status, err)
		}
		if called {
			t.Errorf("--force must not consult confirm")
		}
	})
}

// TestDiffEntry covers the diffEntry port of the Python original's
// _diff_entry: bare-string vs bare-string, mapping key changes, and an added
// key shown as "<absent> -> value".
func TestDiffEntry(t *testing.T) {
	if got := diffEntry(strNode("a"), strNode("b")); len(got) != 1 || got[0] != "  a -> b" {
		t.Errorf("bare diff = %v, want [  a -> b]", got)
	}
	if got := diffEntry(strNode("a"), strNode("a")); got != nil {
		t.Errorf("identical bare strings should have no diff, got %v", got)
	}

	old := mapNode([][2]*yamllib.Node{{strNode("name"), strNode("fetch")}, {strNode("transport"), strNode("stdio")}})
	newN := mapNode([][2]*yamllib.Node{{strNode("name"), strNode("fetch")}, {strNode("transport"), strNode("http")}, {strNode("url"), strNode("https://x")}})
	joined := strings.Join(diffEntry(old, newN), "\n")
	if !strings.Contains(joined, "transport: stdio -> http") {
		t.Errorf("missing transport change in %q", joined)
	}
	if !strings.Contains(joined, "url: <absent> -> https://x") {
		t.Errorf("missing added-url line in %q", joined)
	}
	if strings.Contains(joined, "name:") {
		t.Errorf("unchanged name must not appear in %q", joined)
	}
}

// ── deployMCPEntry ──

func TestDeployMCPEntry_ClaudeWritesConfig(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	dep := &manifest.MCPDependency{Name: "fetch", Transport: "http", URL: "https://example.com/mcp"}
	deployed, skipped, err := deployMCPEntry(&manifest.Manifest{}, "claude", dep)
	if err != nil {
		t.Fatalf("deployMCPEntry: %v", err)
	}
	if len(deployed) != 1 || deployed[0] != "claude" {
		t.Errorf("deployed = %v", deployed)
	}
	if len(skipped) != 0 {
		t.Errorf("skipped = %v", skipped)
	}
	if _, err := os.Stat(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Errorf(".mcp.json not written: %v", err)
	}
}

func TestDeployMCPEntry_NonMCPTargetIsSkippedNotErrored(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	dep := &manifest.MCPDependency{Name: "fetch", Transport: "http", URL: "https://example.com/mcp"}
	_, skipped, err := deployMCPEntry(&manifest.Manifest{}, "agent-skills", dep)
	if err != nil {
		t.Fatalf("deployMCPEntry: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != "agent-skills" {
		t.Errorf("skipped = %v, want [agent-skills]", skipped)
	}
}

// ── runMCPInstall: end-to-end ──

func TestRunMCPInstall_SelfDefinedURL_E2E(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(".claude", 0755)

	err := runMCPInstall(mcpInstallOpts{Name: "api", URL: "https://example.com/mcp", Transport: "http"})
	if err != nil {
		t.Fatalf("runMCPInstall: %v", err)
	}

	yml, _ := os.ReadFile("apm.yml")
	if !strings.Contains(string(yml), "api") || !strings.Contains(string(yml), "https://example.com/mcp") {
		t.Errorf("apm.yml missing entry: %s", yml)
	}
	if _, err := os.Stat(".mcp.json"); err != nil {
		t.Errorf(".mcp.json not written: %v", err)
	}
}

// TestRunMCPInstall_PrintsTargetSource is a regression test (fourth codex
// review round): design.md §8 / PRD R7 require the resolved target source
// (--target > apm.yml targets: > auto-detect) to be verifiable in stdout,
// matching runInstall's existing "Targets: ...  (source: ...)"
// convention -- this line was missing entirely from --mcp's output.
func TestRunMCPInstall_PrintsTargetSource(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(".claude", 0755)

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := runMCPInstall(mcpInstallOpts{Name: "api", URL: "https://example.com/mcp", Transport: "http"})
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("runMCPInstall: %v", err)
	}
	if !strings.Contains(stdout, "i Targets: claude  (source: auto-detect)") {
		t.Errorf("expected a target-source line, got:\n%s", stdout)
	}
}

// TestRunMCPInstall_SummaryShowsTargetsAndAbsolutePath is the R11 regression
// (prd.md/design.md §3): runMCPInstall already computes deployMCPEntry's
// `deployed` target list, but the success summary only ever surfaced it
// inside the "Skipped MCP config for X" branch (nothing-skipped is the
// common case), and hardcoded a literal "apm.yml: apm.yml" instead of
// resolving the real path. Both must now appear in the closing bullet list.
func TestRunMCPInstall_SummaryShowsTargetsAndAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(".claude", 0755)

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := runMCPInstall(mcpInstallOpts{Name: "api", URL: "https://example.com/mcp", Transport: "http"})
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("runMCPInstall: %v", err)
	}
	if !strings.Contains(stdout, "targets: claude") {
		t.Errorf("expected the deployed target list in the summary, got:\n%s", stdout)
	}
	wantPath, pathErr := filepath.Abs("apm.yml")
	if pathErr != nil {
		t.Fatal(pathErr)
	}
	if !strings.Contains(stdout, "apm.yml: "+wantPath) {
		t.Errorf("expected the absolute apm.yml path %q in the summary, got:\n%s", wantPath, stdout)
	}
}

// TestRunMCPInstall_FilteredByWriter_DoesNotClaimSuccess is a regression
// test (codex review): a non-https remote URL passes manifest.ValidateMCP
// (only self-defined stdio/URL structural checks) but is silently dropped
// by the existing per-target writer's own https guard at deploy time.
// runMCPInstall must not print "+ Added" when nothing actually deployed.
func TestRunMCPInstall_FilteredByWriter_DoesNotClaimSuccess(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)
	os.MkdirAll(".claude", 0755)

	r, w, _ := os.Pipe()
	origStdout := os.Stdout
	os.Stdout = w
	err := runMCPInstall(mcpInstallOpts{Name: "insecure", URL: "http://insecure.example.com/mcp", Transport: "http"})
	os.Stdout = origStdout
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	stdout := buf.String()

	if err != nil {
		t.Fatalf("runMCPInstall: %v", err)
	}
	if strings.Contains(stdout, "+ Added") {
		t.Errorf("must not claim success when the writer filtered the entry out, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "not deployed to any target") {
		t.Errorf("expected a not-deployed diagnostic, got:\n%s", stdout)
	}
}

func TestRunMCPInstall_SkillCombined_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	err := runMCPInstall(mcpInstallOpts{Name: "api", URL: "https://example.com/mcp", SkillSubset: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "--skill") {
		t.Errorf("expected --skill conflict error, got %v", err)
	}
	if _, statErr := os.Stat("apm.yml"); statErr != nil {
		t.Fatal("apm.yml should still exist")
	}
	yml, _ := os.ReadFile("apm.yml")
	if strings.Contains(string(yml), "mcp") {
		t.Errorf("apm.yml must be untouched on conflict error: %s", yml)
	}
}

func TestRunMCPInstall_ExistingConflictWithoutForce_ApmYmlUntouched(t *testing.T) {
	// This pins the NON-interactive conflict path: without --force and with
	// no way to prompt, a differing existing entry is a hard error and
	// apm.yml is left untouched. withNonInteractiveStdin forces
	// canPromptCreds() false so the result is deterministic regardless of
	// whether the test harness's stdin happens to look like a TTY (the
	// interactive decline->skipped path is covered by the upsertMCPEntry
	// confirm tests).
	withNonInteractiveStdin(t)
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	original := "name: test\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: api\n      registry: false\n      transport: http\n      url: https://old.example.com/mcp\n"
	os.WriteFile("apm.yml", []byte(original), 0644)
	os.MkdirAll(".claude", 0755)

	err := runMCPInstall(mcpInstallOpts{Name: "api", URL: "https://new.example.com/mcp", Transport: "http"})
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("expected a --force-required error, got %v", err)
	}
	after, _ := os.ReadFile("apm.yml")
	if string(after) != original {
		t.Errorf("apm.yml changed on rejected conflict:\nbefore: %s\nafter:  %s", original, after)
	}
}

func TestRunMCPInstall_RegistryLookupFailure_ApmYmlUntouched(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	original := "name: test\nversion: \"1.0.0\"\n"
	os.WriteFile("apm.yml", []byte(original), 0644)

	srv := mcpRegistryServer(t, map[string]mockMCPServer{})
	err := runMCPInstall(mcpInstallOpts{Name: "nonexistent/server", Registry: srv.URL})
	if err == nil {
		t.Fatal("expected an error for a registry miss")
	}
	after, _ := os.ReadFile("apm.yml")
	if string(after) != original {
		t.Errorf("apm.yml must not be written when registry resolution fails:\nbefore: %s\nafter:  %s", original, after)
	}
}

// TestRunMCPInstall_UnchangedRegistryEntry_NeverContactsRegistry is a
// regression test (third codex review round): an unchanged apm.yml entry
// must be a pure local no-op, never making a registry HTTP call. Round 2's
// original build-then-upsert order still resolved the registry BEFORE
// checking whether anything had changed, so an unreachable/outaged registry
// could fail an install that should have been a silent no-op. opts.Registry
// here points at an address nothing listens on -- if buildDeployDep were
// ever reached, resolveFromRegistry would fail immediately.
func TestRunMCPInstall_UnchangedRegistryEntry_NeverContactsRegistry(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	seeded := "name: test\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: acme/server\n      transport: http\n      registry: http://127.0.0.1:1\n"
	os.WriteFile("apm.yml", []byte(seeded), 0644)

	err := runMCPInstall(mcpInstallOpts{Name: "acme/server", Transport: "http", Registry: "http://127.0.0.1:1"})
	if err != nil {
		t.Fatalf("runMCPInstall should short-circuit on unchanged before ever contacting the registry, got: %v", err)
	}
	after, _ := os.ReadFile("apm.yml")
	if string(after) != seeded {
		t.Errorf("apm.yml should be untouched for an unchanged entry:\nbefore: %s\nafter:  %s", seeded, after)
	}
}

// TestBuildRegistryPersistEntryNode_PersistsEnvDerivedRegistry is a
// regression test (third codex review round): when MCP_REGISTRY_URL (not
// --registry) selected the registry actually queried, the persisted
// apm.yml entry must still record it -- otherwise a later run in an
// environment without that env var set would silently resolve the same
// declared name against the default public registry instead.
func TestBuildRegistryPersistEntryNode_PersistsEnvDerivedRegistry(t *testing.T) {
	opts := mcpInstallOpts{Name: "acme/server"} // no --registry flag, no --transport, no --mcp-version
	node := buildRegistryPersistEntryNode(opts, "https://private.example.com/registry")
	v := nodeToValue(node).(map[string]any)
	if v["registry"] != "https://private.example.com/registry" {
		t.Errorf("persisted entry must record the env-derived registry, got %v", v)
	}
}

// TestEffectiveRegistryURL_NormalizesTrailingSlash is a regression test
// (seventh codex review round): the persisted registry: value must match
// what mcpregistry.NewClient treats as the same registry, or
// "--registry https://reg/" then a later "--registry https://reg" (the
// same registry to NewClient, which trims the trailing slash) would compare
// as different persisted values and force a spurious --force conflict.
func TestEffectiveRegistryURL_NormalizesTrailingSlash(t *testing.T) {
	got := effectiveRegistryURL(mcpInstallOpts{Registry: "https://reg.example.com/"})
	if got != "https://reg.example.com" {
		t.Errorf("effectiveRegistryURL = %q, want trailing slash trimmed", got)
	}
}

func TestRunMCPInstall_NoApmYml_Errors(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	err := runMCPInstall(mcpInstallOpts{Name: "api", URL: "https://example.com/mcp"})
	if err == nil || !strings.Contains(err.Error(), "apm-go init") {
		t.Errorf("expected a no-apm.yml error, got %v", err)
	}
}

// TestInstallCmd_ExplicitEmptyMCPName_DispatchesToMCPPath is a regression
// test (third codex review round): the cobra RunE originally dispatched on
// mcpName != "", so an explicit `--mcp ""` (empty string, but the flag WAS
// given) looked identical to "the flag was never passed" and silently fell
// through to a normal package install instead of reporting the empty-name
// error. Dispatch must use cmd.Flags().Changed("mcp").
func TestInstallCmd_ExplicitEmptyMCPName_DispatchesToMCPPath(t *testing.T) {
	dir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)
	os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

	cmd := installCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"--mcp", ""})

	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "--mcp requires a server name") {
		t.Errorf("expected the --mcp path's empty-name error, got %v", err)
	}
}

// TestInstallCmd_ExplicitEmptyMCPOnlyFlag_RequiresMCP is a regression test
// (fifth codex review round): round 3 fixed --mcp "" dispatching correctly,
// but the requires-mcp gate for the OTHER MCP-only flags was still
// value-based (mcpTransport != "" etc.), so an explicit empty value like
// `--registry ""` (without --mcp) was not detected as "the flag was
// passed" and silently fell through to a normal package install instead of
// erroring. --force was missing from the gate check entirely, so `apm
// install --force` (no --mcp) was silently a no-op (runInstall never reads
// it). All MCP-only flags must use cmd.Flags().Changed(...).
func TestInstallCmd_ExplicitEmptyMCPOnlyFlag_RequiresMCP(t *testing.T) {
	for _, args := range [][]string{
		{"--registry", ""},
		{"--transport", ""},
		{"--mcp-version", ""},
		{"--url", ""},
		{"--force"},
	} {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)
			os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

			cmd := installCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(args)

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), "require --mcp") {
				t.Errorf("args %v: expected a require---mcp error, got %v", args, err)
			}
		})
	}
}

// TestInstallCmd_MCPWithExplicitEmptyFlag_Rejected is a regression test
// (sixth codex review round): round 5 fixed the OUTER requires---mcp gate,
// but once inside the --mcp path an explicitly-empty value (e.g. --url "")
// was still indistinguishable from "not given" and silently fell through to
// a different branch -- e.g. `--mcp foo --url ""` silently became a
// registry lookup for "foo" instead of erroring. Must reject at dispatch,
// where Changed() is still available.
func TestInstallCmd_MCPWithExplicitEmptyFlag_Rejected(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"empty url", []string{"--mcp", "foo", "--url", ""}, "--url cannot be empty"},
		{"empty transport", []string{"--mcp", "foo", "--transport", ""}, "--transport cannot be empty"},
		{"empty registry", []string{"--mcp", "foo", "--registry", ""}, "--registry cannot be empty"},
		{"empty mcp-version", []string{"--mcp", "foo", "--mcp-version", ""}, "--mcp-version cannot be empty"},
		{"bare dash-dash with no command", []string{"--mcp", "foo", "--"}, "must be followed by a stdio command"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			origDir, _ := os.Getwd()
			os.Chdir(dir)
			defer os.Chdir(origDir)
			os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\n"), 0644)

			cmd := installCmd()
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			cmd.SetArgs(tc.args)

			err := cmd.Execute()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("args %v: expected error containing %q, got %v", tc.args, tc.want, err)
			}
		})
	}
}
