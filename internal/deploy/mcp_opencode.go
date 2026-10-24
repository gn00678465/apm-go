package deploy

import (
	"encoding/json"
	"math"

	"github.com/apm-go/apm/internal/manifest"
	"go.yaml.in/yaml/v4"
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
	if err := writeMergedMCPJSON(projectDir, relPath, "mcp", entries, consideredNames(prims), 0600); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func opencodeMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	e := map[string]any{"enabled": true}
	if r.Enabled != nil {
		// Forwarded unchanged, whatever its type, as the Oracle does
		// (adapters/client/opencode.py:124-127).
		e["enabled"] = jsonValue(r.Enabled)
	}
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

// jsonValue is the JSON value of a node manifest.ParseMCPEntry tagged. A
// number JSON cannot spell (.inf, 0x1F) is written as its YAML text.
func jsonValue(n *yaml.Node) any {
	switch n.Kind {
	case yaml.SequenceNode:
		out := make([]any, 0, len(n.Content))
		for _, c := range n.Content {
			out = append(out, jsonValue(c))
		}
		return out
	case yaml.MappingNode:
		out := make(map[string]any, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			out[n.Content[i].Value] = jsonValue(n.Content[i+1])
		}
		return out
	}
	switch n.Tag {
	case "!!null":
		return nil
	case "!!bool":
		return n.Value == "true"
	case "!!float":
		// The node holds PyYAML's spelling (1.0e+20); json.dumps writes
		// the float's repr (1e+20).
		if f := manifest.PyYAMLFloat(n.Value); !math.IsInf(f, 0) && !math.IsNaN(f) {
			return json.Number(manifest.PyFloatRepr(f))
		}
	case "!!int":
		if json.Valid([]byte(n.Value)) {
			return json.Number(n.Value)
		}
	}
	return n.Value
}
