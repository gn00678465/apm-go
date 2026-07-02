package deploy

import (
	"path/filepath"

	"github.com/apm-go/apm/internal/manifest"
)

func (a *copilotAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveTranslate }

// WriteMCP writes .github/mcp-config.json (apm-go project-scoped extension,
// NOT apm-cli parity -- apm-cli writes to the user's home directory; see
// design.md D2). Translate mode: placeholders pass through verbatim for the
// runtime to resolve.
func (a *copilotAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	entries, diags := buildMCPEntries(prims, manifest.ResolveTranslate, copilotMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	relPath := ".github/mcp-config.json"
	if err := writeMergedMCPJSON(filepath.Join(projectDir, filepath.FromSlash(relPath)), "mcpServers", entries, consideredNames(prims), 0644); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func copilotMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	if r.Transport == "stdio" {
		e := map[string]any{"type": "local", "command": r.Command}
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
