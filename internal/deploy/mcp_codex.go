package deploy

import (
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

func (a *codexAdapter) MCPResolveMode() manifest.ResolveMode { return manifest.ResolveBake }

// WriteMCP writes .codex/config.toml table mcp_servers (url plus the header
// fields for remote transports; SSE is not supported and skipped with a
// diagnostic).
func (a *codexAdapter) WriteMCP(prims []Primitive, projectDir string) ([]string, []string, []string, error) {
	// Headers keep ${VAR} verbatim (envMode bakes env dict as before); the
	// entry builder re-encodes header placeholders into Codex's own
	// bearer_token_env_var / env_http_headers fields (M8).
	entries, diags := buildMCPEntries(prims, manifest.ResolveBake, manifest.ResolveTranslate, codexMCPEntry)
	if len(prims) == 0 {
		return nil, nil, diags, nil
	}
	keepPrimOrder(prims, entries, "env", "http_headers", "env_http_headers")
	relPath := ".codex/config.toml"
	if err := writeOrderedMCP(projectDir, relPath, "mcp_servers", entries, consideredNames(prims), primNames(prims), codexMCPFile); err != nil {
		return nil, nil, diags, err
	}
	return []string{relPath}, entryNames(entries), diags, nil
}

func codexMCPEntry(r *ResolvedMCPServer) (map[string]any, bool, string) {
	if r.Transport == "sse" {
		return nil, false, "SSE transport is not supported by codex; skipped"
	}
	if r.Transport == "stdio" {
		// args and env are always written, [] and an empty table when there
		// is none (codex.py:231-236).
		args := r.Args
		if args == nil {
			args = []string{}
		}
		env := r.Env
		if env == nil {
			env = map[string]string{}
		}
		return map[string]any{"command": r.Command, "args": args, "env": env}, true, ""
	}
	// Deviation: Oracle 0.33.0 writes id = "" on stdio and remote entries
	// (codex.py:235,318). Codex's config schema has no id field and
	// --strict-config rejects the whole file over it (microsoft/apm#3087).
	e := map[string]any{"url": r.URL}
	if len(r.Headers) > 0 {
		encodeCodexHeaders(e, r.Headers)
	}
	return e, true, ""
}

// codexMCPKeyOrder is the key order of one server in config.toml as the
// Oracle's Codex adapter builds it (codex.py:231-236, 316-369), less its id.
var codexMCPKeyOrder = []string{"command", "args", "env", "url", "bearer_token_env_var", "http_headers", "env_http_headers"}

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
