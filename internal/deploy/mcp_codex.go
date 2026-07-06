package deploy

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/pelletier/go-toml/v2"
)

func (a *codexAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes .codex/config.toml table mcp_servers (apm-cli parity: url+
// id+http_headers for remote transports; SSE is not supported and skipped
// with a diagnostic).
func (a *codexAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	// Headers keep ${VAR} verbatim (envMode bakes env dict as before); the
	// entry builder re-encodes header placeholders into Codex's own
	// bearer_token_env_var / env_http_headers fields (M8).
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, manifest.ResolveTranslate, codexMCPEntry)
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
		encodeCodexHeaders(e, r.Headers)
	}
	return e, true, ""
}

// encodeCodexHeaders splits resolved headers into Codex's config.toml fields
// (developers.openai.com/codex/mcp): Codex does NOT expand ${VAR} inline, so a
// header carrying a placeholder must be re-encoded to reference the env var by
// name. "Authorization: Bearer ${VAR}" -> bearer_token_env_var = "VAR"; a
// header whose whole value is ${VAR} -> env_http_headers[name] = "VAR"; any
// other (static, or a value with surrounding text) stays in http_headers.
func encodeCodexHeaders(e map[string]any, headers map[string]string) {
	var bearerEnv string
	envHeaders := map[string]string{}
	staticHeaders := map[string]string{}
	for _, k := range sortedKeys(headers) {
		v := headers[k]
		if strings.EqualFold(k, "Authorization") {
			if name, ok := bearerEnvVar(v); ok {
				bearerEnv = name
				continue
			}
		}
		if name, ok := soleEnvVar(v); ok {
			envHeaders[k] = name
			continue
		}
		staticHeaders[k] = v
	}
	if bearerEnv != "" {
		e["bearer_token_env_var"] = bearerEnv
	}
	if len(envHeaders) > 0 {
		e["env_http_headers"] = envHeaders
	}
	if len(staticHeaders) > 0 {
		e["http_headers"] = staticHeaders
	}
}

// soleEnvVar returns the var name when v is exactly a single ${VAR} / ${env:VAR}
// placeholder (the whole string), else ("", false).
func soleEnvVar(v string) (string, bool) {
	if m := manifest.EnvVarRe.FindStringSubmatch(v); m != nil && m[0] == v {
		return m[1], true
	}
	return "", false
}

// bearerEnvVar returns the var name when v is "Bearer ${VAR}" (the standard
// bearer scheme wrapping a single env-var placeholder), else ("", false).
func bearerEnvVar(v string) (string, bool) {
	const prefix = "Bearer "
	if !strings.HasPrefix(v, prefix) {
		return "", false
	}
	return soleEnvVar(strings.TrimSpace(v[len(prefix):]))
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
