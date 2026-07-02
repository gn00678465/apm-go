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
