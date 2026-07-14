package deploy

import (
	"path/filepath"

	"github.com/apm-go/apm/internal/manifest"
)

func (a *claudeAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes .mcp.json (apm-cli parity: key mcpServers, type+url for
// remote transports).
func (a *claudeAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	// env bakes (parity with the Python original), but headers keep ${VAR}
	// verbatim: Claude Code .mcp.json natively expands ${VAR}/${VAR:-default}
	// in headers at runtime, so a credential must not be baked to disk (M8).
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, manifest.ResolveTranslate, claudeMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	relPath := ".mcp.json"
	if err := writeMergedMCPJSON(filepath.Join(projectDir, relPath), "mcpServers", entries, consideredNames(prims), 0600); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func claudeMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	if r.Transport == "stdio" {
		e := map[string]any{"type": "stdio", "command": r.Command}
		if len(r.Args) > 0 {
			e["args"] = r.Args
		}
		if len(r.Env) > 0 {
			e["env"] = r.Env
		}
		return e, true, ""
	}
	e := map[string]any{"type": r.Transport, "url": r.URL}
	if len(r.Headers) > 0 {
		e["headers"] = r.Headers
	}
	return e, true, ""
}
