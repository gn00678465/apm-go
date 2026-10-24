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
	keepPrimOrder(prims, entries, "env", "headers")
	relPath := ".mcp.json"
	if err := writeOrderedMCP(projectDir, relPath, "mcpServers", entries, consideredNames(prims), primNames(prims), claudeMCPFile); err != nil {
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
		// args is always written, [] when there is none, as the Oracle's
		// copilot formatter that the Claude adapter inherits builds it
		// (adapters/client/copilot.py, claude.py).
		args := r.Args
		if args == nil {
			args = []string{}
		}
		e := map[string]any{"type": "stdio", "command": r.Command, "args": args}
		if len(r.Env) > 0 {
			e["env"] = r.Env
		}
		return e, true, ""
	}
	// The Oracle writes "type": "http" for every remote
	// (adapters/client/copilot.py:485-490), which Claude Code reads as
	// streamable HTTP. sse stays sse: Claude Code connects to an "http"
	// entry with streamable HTTP, which an SSE server does not speak.
	typ := r.Transport
	if typ == "streamable-http" {
		typ = "http"
	}
	e := map[string]any{"type": typ, "url": r.URL}
	if len(r.Headers) > 0 {
		e["headers"] = r.Headers
	}
	return e, true, ""
}

// keepPrimOrder keeps, in each written entry, the declared order of the
// env (envKey) and header (headerKeys) maps of its primitive.
func keepPrimOrder(prims []Primitive, entries map[string]map[string]any, envKey string, headerKeys ...string) {
	for _, p := range prims {
		entry := entries[p.Name]
		if p.MCP == nil || entry == nil {
			continue
		}
		keepDeclaredOrder(entry, envKey, p.MCP.EnvOrder)
		for _, k := range headerKeys {
			keepDeclaredOrder(entry, k, p.MCP.HeaderOrder)
		}
	}
}

// primNames is the names of prims, in their order.
func primNames(prims []Primitive) []string {
	names := make([]string, 0, len(prims))
	for _, p := range prims {
		names = append(names, p.Name)
	}
	return names
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
