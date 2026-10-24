package deploy

import (
	"github.com/apm-go/apm/internal/manifest"
)

func (a *antigravityAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes .agents/mcp_config.json per the oracle descriptor
// (targets/expected/antigravity.yaml): key mcpServers, HTTP field serverUrl.
// The servers, their keys and env and headers keep the order the Oracle's
// Gemini-family adapter writes them in (adapters/client/gemini.py:93-111,
// 125-205): a server already in the file keeps its place.
func (a *antigravityAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, manifest.ResolveBake, antigravityMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	keepPrimOrder(prims, entries, "env", "headers")
	relPath := ".agents/mcp_config.json"
	if err := writeOrderedMCP(projectDir, relPath, "mcpServers", entries, consideredNames(prims), primNames(prims), antigravityMCPFile); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

// antigravityMCPKeyOrder: stdio is command, env, args; remote is the URL,
// then headers (adapters/client/gemini.py:153-205).
var antigravityMCPKeyOrder = []string{"command", "env", "args", "serverUrl", "headers"}

func antigravityMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	if r.Transport == "stdio" {
		// args is always written, [] when there is none (gemini.py:166).
		args := r.Args
		if args == nil {
			args = []string{}
		}
		e := map[string]any{"command": r.Command, "args": args}
		if len(r.Env) > 0 {
			e["env"] = r.Env
		}
		return e, true, ""
	}
	// All remote transports (sse, http, streamable-http) use serverUrl: the
	// official docs reject legacy `url`/`httpUrl`, and the agy 1.0.16 binary
	// validator only accepts command|serverUrl (research/cli-mcp.md).
	// Deviation: Oracle 0.33.0 writes httpUrl, or url for sse
	// (gemini.py:190-194).
	e := map[string]any{"serverUrl": r.URL}
	if len(r.Headers) > 0 {
		e["headers"] = r.Headers
	}
	return e, true, ""
}
