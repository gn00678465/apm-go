package deploy

import (
	"path/filepath"

	"github.com/apm-go/apm/internal/manifest"
)

func (a *antigravityAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes .agents/mcp_config.json per the oracle descriptor
// (targets/expected/antigravity.yaml): key mcpServers, HTTP field serverUrl.
func (a *antigravityAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, manifest.ResolveBake, antigravityMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	relPath := ".agents/mcp_config.json"
	if err := writeMergedMCPJSON(filepath.Join(projectDir, filepath.FromSlash(relPath)), "mcpServers", entries, consideredNames(prims), 0600); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func antigravityMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	if r.Transport == "stdio" {
		e := map[string]any{"command": r.Command}
		if len(r.Args) > 0 {
			e["args"] = r.Args
		}
		if len(r.Env) > 0 {
			e["env"] = r.Env
		}
		return e, true, ""
	}
	// All remote transports (sse, http, streamable-http) use serverUrl: the
	// official docs reject legacy `url`/`httpUrl`, and the agy 1.0.16 binary
	// validator only accepts command|serverUrl (research/cli-mcp.md).
	e := map[string]any{"serverUrl": r.URL}
	if len(r.Headers) > 0 {
		e["headers"] = r.Headers
	}
	return e, true, ""
}
