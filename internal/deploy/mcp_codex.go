package deploy

import (
	"os"
	"path/filepath"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/pelletier/go-toml/v2"
)

func (a *codexAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes .codex/config.toml table mcp_servers (apm-cli parity: url+
// id+http_headers for remote transports; SSE is not supported and skipped
// with a diagnostic).
func (a *codexAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, codexMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	relPath := ".codex/config.toml"
	if err := writeMergedMCPTOML(filepath.Join(projectDir, filepath.FromSlash(relPath)), "mcp_servers", entries, consideredNames(prims), 0600); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func codexMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	if r.Transport == "sse" {
		return nil, false, "SSE transport is not supported by codex; skipped"
	}
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
	e := map[string]any{"url": r.URL, "id": r.Name}
	if len(r.Headers) > 0 {
		e["http_headers"] = r.Headers
	}
	return e, true, ""
}

// writeMergedMCPTOML mirrors writeMergedMCPJSON for codex's TOML config: read
// the existing table at topKey (if any), merge entries per mergeMCPServers.
func writeMergedMCPTOML(path, topKey string, entries map[string]map[string]any, considered map[string]bool, perm os.FileMode) error {
	root, err := readExistingMCPRoot(path, toml.Unmarshal)
	if err != nil {
		return err
	}
	existing, _ := root[topKey].(map[string]any)
	root[topKey] = mergeMCPServers(existing, entries, considered)

	data, err := toml.Marshal(root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return writeFileWithPerm(path, data, perm)
}
