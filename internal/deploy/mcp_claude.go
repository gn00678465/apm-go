package deploy

import (
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
	for _, p := range prims {
		entry := entries[p.Name]
		if p.MCP == nil || entry == nil {
			continue
		}
		keepDeclaredOrder(entry, "env", p.MCP.EnvOrder)
		keepDeclaredOrder(entry, "headers", p.MCP.HeaderOrder)
	}
	relPath := ".mcp.json"
	names := make([]string, 0, len(prims))
	for _, p := range prims {
		names = append(names, p.Name)
	}
	if err := writeOrderedMCPJSON(projectDir, relPath, "mcpServers", entries, consideredNames(prims), names, claudeMCPKeyOrder, 0600); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

// claudeMCPKeyOrder is the key order of one server in .mcp.json as the
// Oracle's Claude adapter writes it (adapters/client/claude.py), observed
// from its output: stdio is type, command, env, args; remote is type, url,
// headers.
var claudeMCPKeyOrder = []string{"type", "command", "env", "args", "cwd", "url", "headers"}

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

// keepDeclaredOrder turns entry[key], a string map, into an object whose keys
// are in the order apm.yml or the command line declared them: the Oracle
// writes env and headers to .mcp.json in that order, not sorted.
func keepDeclaredOrder(entry map[string]any, key string, order []string) {
	m, ok := entry[key].(map[string]string)
	if !ok {
		return
	}
	obj := newOrderedObject()
	for _, k := range manifest.OrderedKeys(m, order) {
		obj.set(k, m[k])
	}
	entry[key] = obj
}
