package manifest

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// ── MCP validation tests (mf-012) ──

func TestValidateMCP_SelfDefined(t *testing.T) {
	tests := []struct {
		name    string
		mcp     MCPDependency
		wantErr string
	}{
		{
			name:    "missing transport",
			mcp:     MCPDependency{Registry: false},
			wantErr: "requires 'transport'",
		},
		{
			name:    "stdio missing command",
			mcp:     MCPDependency{Registry: false, Transport: "stdio"},
			wantErr: "requires 'command'",
		},
		{
			name:    "stdio command with spaces no args",
			mcp:     MCPDependency{Registry: false, Transport: "stdio", Command: "npx -y @some/server"},
			wantErr: "whitespace",
		},
		{
			name:    "http missing url",
			mcp:     MCPDependency{Registry: false, Transport: "http"},
			wantErr: "requires 'url'",
		},
		{
			name:    "sse missing url",
			mcp:     MCPDependency{Registry: false, Transport: "sse"},
			wantErr: "requires 'url'",
		},
		{
			name:    "streamable-http missing url",
			mcp:     MCPDependency{Registry: false, Transport: "streamable-http"},
			wantErr: "requires 'url'",
		},
		{
			name:    "unknown transport",
			mcp:     MCPDependency{Registry: false, Transport: "grpc"},
			wantErr: "unknown MCP transport",
		},
		{
			name:    "http url with embedded credentials",
			mcp:     MCPDependency{Name: "leaky", Registry: false, Transport: "http", URL: "https://user:pass@example.com/mcp"},
			wantErr: "embedded credentials",
		},
		{
			// A malformed URL that still embeds credentials must not slip
			// through just because it fails to parse -- fail closed, not
			// silently skip the guard (found in a follow-up codex review
			// round after the first credential check only fired on a
			// successful url.Parse).
			name:    "malformed url with embedded credentials",
			mcp:     MCPDependency{Name: "leaky-malformed", Registry: false, Transport: "http", URL: "https://user:pass@%zz"},
			wantErr: "embedded credentials",
		},
		{
			// No "@" present, so the coarse credential pre-check doesn't
			// fire here -- this exercises url.Parse's own error path
			// (invalid percent-encoding), confirming it fails closed too.
			name:    "malformed url without credentials",
			mcp:     MCPDependency{Name: "malformed", Registry: false, Transport: "http", URL: "https://%zz"},
			wantErr: "not a valid URL",
		},
		{
			// url.Parse accepts a bare relative string without error --
			// this must still be rejected as not a usable remote endpoint,
			// not silently persisted only to fail later at deploy time
			// (found by codex review).
			name:    "relative url (missing scheme/host)",
			mcp:     MCPDependency{Name: "relative", Registry: false, Transport: "http", URL: "example.com/mcp"},
			wantErr: "must be absolute",
		},
		{
			// Mirrors Python's _ALLOWED_URL_SCHEMES =
			// frozenset({"http", "https"}) (models/dependency/mcp.py:40,
			// 249): a literal URL with any other scheme must be rejected,
			// not silently persisted into apm.yml only to fail later at
			// deploy time.
			name:    "ftp scheme rejected",
			mcp:     MCPDependency{Name: "x", Registry: false, Transport: "http", URL: "ftp://example.com/mcp"},
			wantErr: "scheme",
		},
		{
			// WebSocket schemes are explicitly unsupported for MCP
			// transports too.
			name:    "ws scheme rejected",
			mcp:     MCPDependency{Name: "x", Registry: false, Transport: "sse", URL: "ws://example.com/mcp"},
			wantErr: "scheme",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMCP(&tt.mcp)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
			}
		})
	}
}

// TestValidateMCP_RejectsNonHTTPSchemeNamingIt is a regression test (C9,
// live-CLI finding): `install --mcp --url ftp://...` used to be persisted
// verbatim into apm.yml with exit 0. Python restricts self-defined HTTP-like
// transports to _ALLOWED_URL_SCHEMES = frozenset({"http", "https"})
// (models/dependency/mcp.py:40,249); ValidateMCP must reject any other
// literal scheme and name it in the error, without echoing the rest of the
// URL (which could carry a query-embedded token, per the "never echoes
// secretish values" convention above).
func TestValidateMCP_RejectsNonHTTPSchemeNamingIt(t *testing.T) {
	m := &MCPDependency{Name: "api", Registry: false, Transport: "http", URL: "ftp://example.com/mcp?token=super-secret-value"}
	err := ValidateMCP(m)
	if err == nil {
		t.Fatal("expected an error for a non-http(s) literal URL scheme")
	}
	if !strings.Contains(err.Error(), "ftp") {
		t.Errorf("error = %v, want it to name the rejected scheme %q", err, "ftp")
	}
	if strings.Contains(err.Error(), "super-secret-value") {
		t.Errorf("error message leaked the raw value: %v", err)
	}
}

// TestValidateMCP_AcceptsHTTPAndHTTPSSchemes confirms the allowlist still
// accepts exactly the two schemes Python allows.
func TestValidateMCP_AcceptsHTTPAndHTTPSSchemes(t *testing.T) {
	for _, scheme := range []string{"http", "https"} {
		t.Run(scheme, func(t *testing.T) {
			m := &MCPDependency{Name: "x", Registry: false, Transport: "http", URL: scheme + "://example.com/mcp"}
			if err := ValidateMCP(m); err != nil {
				t.Errorf("ValidateMCP rejected an allowed scheme %q: %v", scheme, err)
			}
		})
	}
}

// TestValidateMCP_SchemeCheckSkippedForPlaceholderURL is a regression test
// (C9 design constraint): a placeholder-containing URL is not a literal
// value yet at declaration time, so it must skip scheme validation
// entirely -- same as it already skips the absolute-URL and malformed-parse
// checks above -- rather than being rejected because the placeholder
// substring happens to look like a non-http(s) scheme.
func TestValidateMCP_SchemeCheckSkippedForPlaceholderURL(t *testing.T) {
	m := &MCPDependency{Name: "x", Registry: false, Transport: "http", URL: "${MCP_URL}"}
	if err := ValidateMCP(m); err != nil {
		t.Errorf("ValidateMCP rejected a placeholder URL on scheme grounds: %v", err)
	}
}

// TestValidateMCP_NeverEchoesRawSecretishValues is a regression test
// (eighth codex review round): none of ValidateMCP's error messages may
// echo m.Command or m.URL verbatim -- a user could pass a shell command
// line or a query-embedded token there by mistake, and several error
// branches used to interpolate the raw value straight into the message.
func TestValidateMCP_NeverEchoesRawSecretishValues(t *testing.T) {
	cases := []struct {
		name string
		mcp  MCPDependency
	}{
		{
			name: "stdio command with whitespace",
			mcp:  MCPDependency{Name: "x", Registry: false, Transport: "stdio", Command: "run --token=super-secret-value"},
		},
		{
			name: "malformed url with query token",
			mcp:  MCPDependency{Name: "x", Registry: false, Transport: "http", URL: "https://example/%zz?token=super-secret-value"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateMCP(&tc.mcp)
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "super-secret-value") {
				t.Errorf("error message leaked the raw value: %v", err)
			}
		})
	}
}

// TestValidateMCP_AllowsPlaceholderURLs is a regression test (ninth codex
// review round): the round-8 absolute-URL requirement was too broad -- it
// rejected legitimate mf-013 placeholder URLs that are resolved later, per
// target, not literal URLs at declaration time. translate-mode targets
// (e.g. Copilot) intentionally preserve a bare "${input:...}" value
// verbatim for runtime resolution; bake-mode targets resolve "${VAR}"/
// "${env:VAR}" via ResolvePlaceholders before ever reaching a target
// writer. Neither should be rejected by ValidateMCP's own absolute-URL
// check, which only makes sense once a value is a literal URL.
func TestValidateMCP_AllowsPlaceholderURLs(t *testing.T) {
	cases := []string{
		"${input:mcp-url}",
		"${MCP_URL}",
		"${env:MCP_URL}",
		"https://${input:host}/mcp",
		// mf-013 placeholders are resolved by plain, position-agnostic
		// substring substitution (manifest.ResolvePlaceholders), so they
		// can legitimately appear in the port or an IPv6-bracket host too
		// -- a round-10 fix that substituted every placeholder with a
		// fixed "x" token before re-parsing wrongly rejected exactly these
		// positions ("x" is not a valid port or IPv6 literal), found in a
		// further follow-up round.
		"https://example.com:${MCP_PORT}/mcp",
		"https://[${MCP_HOST}]/mcp",
	}
	for _, u := range cases {
		t.Run(u, func(t *testing.T) {
			m := &MCPDependency{Name: "x", Registry: false, Transport: "http", URL: u}
			if err := ValidateMCP(m); err != nil {
				t.Errorf("ValidateMCP rejected a legitimate placeholder URL %q: %v", u, err)
			}
		})
	}
}

// TestValidateMCP_RejectsMalformedLiteralPortionEvenWithPlaceholder is a
// regression test (tenth codex review round): round 9's fix blanket-skipped
// ALL URL parsing whenever any placeholder was present, so a malformed
// LITERAL portion unrelated to the placeholder -- e.g. a bad percent-escape
// in the path -- could slip through too. The exemption must validate a
// placeholder-substituted skeleton, not skip parsing entirely.
func TestValidateMCP_RejectsMalformedLiteralPortionEvenWithPlaceholder(t *testing.T) {
	m := &MCPDependency{Name: "x", Registry: false, Transport: "http", URL: "https://example.com/%zz/${TOKEN}"}
	if err := ValidateMCP(m); err == nil {
		t.Fatal("expected ValidateMCP to reject a malformed literal portion even alongside a legitimate placeholder")
	}
}

func TestValidateMCP_SelfDefinedAccept(t *testing.T) {
	tests := []struct {
		name string
		mcp  MCPDependency
	}{
		{
			name: "stdio with command",
			mcp:  MCPDependency{Registry: false, Transport: "stdio", Command: "my-server"},
		},
		{
			name: "stdio command with spaces and explicit args",
			mcp:  MCPDependency{Registry: false, Transport: "stdio", Command: "npx -y @some/server", Args: &[]string{}},
		},
		{
			name: "http with url",
			mcp:  MCPDependency{Registry: false, Transport: "http", URL: "https://example.com/mcp"},
		},
		{
			name: "sse with url",
			mcp:  MCPDependency{Registry: false, Transport: "sse", URL: "https://example.com/sse"},
		},
		{
			name: "streamable-http with url",
			mcp:  MCPDependency{Registry: false, Transport: "streamable-http", URL: "https://example.com/api"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateMCP(&tt.mcp); err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestValidateMCP_RegistrySkipsValidation(t *testing.T) {
	m := &MCPDependency{Registry: nil}
	if err := ValidateMCP(m); err != nil {
		t.Errorf("registry=nil should skip validation: %v", err)
	}

	m2 := &MCPDependency{Registry: "https://registry.example.com"}
	if err := ValidateMCP(m2); err != nil {
		t.Errorf("registry=URL should skip validation: %v", err)
	}
}

func TestParseMCPEntry_FullObject(t *testing.T) {
	root := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	addScalar := func(k, v string) {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v},
		)
	}
	addScalar("name", "my-server")
	addScalar("transport", "http")
	addScalar("url", "https://example.com/mcp")

	// Add env mapping
	envKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "env"}
	envVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	envVal.Content = append(envVal.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "TOKEN"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "${env:MY_TOKEN}"},
	)
	root.Content = append(root.Content, envKey, envVal)

	// Add headers mapping
	hdrKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "headers"}
	hdrVal := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	hdrVal.Content = append(hdrVal.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "Authorization"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "Bearer ${TOKEN}"},
	)
	root.Content = append(root.Content, hdrKey, hdrVal)

	m, err := ParseMCPEntry(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "my-server" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Transport != "http" {
		t.Errorf("Transport = %q", m.Transport)
	}
	if m.Env["TOKEN"] != "${env:MY_TOKEN}" {
		t.Errorf("Env[TOKEN] = %q", m.Env["TOKEN"])
	}
	if m.Headers["Authorization"] != "Bearer ${TOKEN}" {
		t.Errorf("Headers[Authorization] = %q", m.Headers["Authorization"])
	}
}

func TestParseMCPEntry_RegistryURL(t *testing.T) {
	entry := buildMappingNode(map[string]string{
		"name":     "my-server",
		"registry": "https://registry.example.com",
	})
	m, err := ParseMCPEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if m.Registry != "https://registry.example.com" {
		t.Errorf("Registry = %v", m.Registry)
	}
}

func TestParseMCPEntry_Version(t *testing.T) {
	entry := buildMappingNode(map[string]string{
		"name":    "my-server",
		"version": "1.2.3",
	})
	m, err := ParseMCPEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if m.Version != "1.2.3" {
		t.Errorf("Version = %q, want %q", m.Version, "1.2.3")
	}
}

func TestParseMCPEntry_String(t *testing.T) {
	entry := &yaml.Node{Kind: yaml.ScalarNode, Value: "io.github.github/github-mcp-server"}
	m, err := ParseMCPEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "io.github.github/github-mcp-server" {
		t.Errorf("Name = %q", m.Name)
	}
}

func TestParseMCPEntry_SelfDefined(t *testing.T) {
	entry := buildMappingNode(map[string]string{
		"registry":  "false",
		"transport": "stdio",
		"command":   "my-mcp-server",
	})
	// Add args: [] explicitly
	argsKey := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "args"}
	argsVal := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	entry.Content = append(entry.Content, argsKey, argsVal)

	m, err := ParseMCPEntry(entry)
	if err != nil {
		t.Fatal(err)
	}
	if m.Registry != false {
		t.Errorf("Registry should be false")
	}
	if m.Transport != "stdio" {
		t.Errorf("Transport = %q", m.Transport)
	}
	if m.Args == nil {
		t.Error("Args should not be nil")
	}
	if err := ValidateMCP(m); err != nil {
		t.Errorf("valid self-defined MCP should pass: %v", err)
	}
}

// ── MCP retention on Manifest (AC1) ──

func TestParseManifest_RetainsMCPServersProdAndDev(t *testing.T) {
	data := []byte(`
name: my-project
version: 1.0.0
dependencies:
  mcp:
    - name: prod-server
      registry: false
      transport: stdio
      command: my-mcp-server
devDependencies:
  mcp:
    - name: dev-server
      registry: false
      transport: http
      url: https://example.com/mcp
`)
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(m.MCPServers) != 1 || m.MCPServers[0].Name != "prod-server" {
		t.Errorf("MCPServers = %+v, want 1 entry named prod-server", m.MCPServers)
	}
	if len(m.MCPDevServers) != 1 || m.MCPDevServers[0].Name != "dev-server" {
		t.Errorf("MCPDevServers = %+v, want 1 entry named dev-server", m.MCPDevServers)
	}
}

func TestParseManifest_NoMCPBlockLeavesNilServers(t *testing.T) {
	data := []byte("name: my-project\nversion: 1.0.0\n")
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := ParseManifest(node)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.MCPServers) != 0 {
		t.Errorf("MCPServers = %+v, want empty", m.MCPServers)
	}
	if len(m.MCPDevServers) != 0 {
		t.Errorf("MCPDevServers = %+v, want empty", m.MCPDevServers)
	}
}

// ── Placeholder recognition tests (mf-013) ──

func TestRecognizePlaceholders(t *testing.T) {
	tests := []struct {
		input   string
		wantEnv int
		wantIn  int
		wantAct int
	}{
		{"${TOKEN}", 1, 0, 0},
		{"${env:TOKEN}", 1, 0, 0},
		{"${input:api-key}", 0, 1, 0},
		{"${{ secrets.TOKEN }}", 0, 0, 1},
		{"Bearer ${env:API_KEY}", 1, 0, 0},
		{"no placeholders here", 0, 0, 0},
		{"${A} and ${input:b} and ${{ c }}", 1, 1, 1},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			ps := RecognizePlaceholders(tt.input)
			var envC, inC, actC int
			for _, p := range ps {
				switch p.Type {
				case PlaceholderEnv:
					envC++
				case PlaceholderInput:
					inC++
				case PlaceholderActions:
					actC++
				}
			}
			if envC != tt.wantEnv {
				t.Errorf("env placeholders = %d, want %d", envC, tt.wantEnv)
			}
			if inC != tt.wantIn {
				t.Errorf("input placeholders = %d, want %d", inC, tt.wantIn)
			}
			if actC != tt.wantAct {
				t.Errorf("actions placeholders = %d, want %d", actC, tt.wantAct)
			}
		})
	}
}

// ── Marketplace source validation tests (mf-017) ──

func TestValidateMarketplaceSource(t *testing.T) {
	tests := []struct {
		source  string
		wantErr string
	}{
		// valid
		{"./packages/foo", ""},
		{"https://example.com/owner/repo", ""},
		{"https://example.com/owner/repo.git", ""},
		{"owner/repo", ""},
		{"github.com/owner/repo", ""},

		// invalid
		{"", "empty"},
		{"../escape", ".."},
		{"./packages/../../../etc/passwd", ".."},
		{"http://example.com/repo", "https://"},
		{"ftp://example.com/repo", "https://"},
		{"https://user@example.com/repo", "userinfo"},
		{"https://example.com:8080/repo", "port"},
		{"https://example.com/repo?q=1", "query"},
		{".packages/foo", "start with './'"},
	}
	for _, tt := range tests {
		t.Run(tt.source, func(t *testing.T) {
			err := ValidateMarketplaceSource(tt.source)
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			} else {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}
