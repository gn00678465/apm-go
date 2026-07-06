package deploy

import (
	"path/filepath"

	"github.com/apm-go/apm/internal/manifest"
)

func (a *opencodeAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes opencode.json (apm-cli parity: top-level key "mcp", not
// mcpServers -- see design.md and research/opencode-mcp-parity.md). Remote
// transports (http/sse/streamable-http) are unified into a single "remote"
// shape, unlike codex (which skips SSE) or antigravity (which switches the
// URL field name per transport).
func (a *opencodeAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	// Headers keep ${VAR} verbatim (envMode bakes env dict as before); the
	// entry builder rewrites header placeholders to OpenCode's {env:VAR}
	// runtime-substitution syntax (M8).
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, manifest.ResolveTranslate, opencodeMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	relPath := "opencode.json"
	if err := writeMergedMCPJSON(filepath.Join(projectDir, relPath), "mcp", entries, consideredNames(prims), 0600); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func opencodeMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	e := map[string]any{"enabled": true}
	if r.Transport == "stdio" {
		e["type"] = "local"
		e["command"] = append([]string{r.Command}, r.Args...)
		if len(r.Env) > 0 {
			e["environment"] = r.Env
		}
		return e, true, ""
	}
	e["type"] = "remote"
	e["url"] = r.URL
	if len(r.Headers) > 0 {
		e["headers"] = translateOpencodeHeaders(r.Headers)
	}
	return e, true, ""
}

// translateOpencodeHeaders rewrites ${VAR} / ${env:VAR} placeholders to
// OpenCode's own {env:VAR} runtime-substitution syntax (opencode.json), so a
// credential stays a runtime reference instead of being baked to disk.
// Non-placeholder (static) header values pass through unchanged.
func translateOpencodeHeaders(headers map[string]string) map[string]string {
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		out[k] = manifest.EnvVarRe.ReplaceAllString(v, "{env:$1}")
	}
	return out
}
