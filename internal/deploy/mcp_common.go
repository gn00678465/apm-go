package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/manifest"

	"github.com/apm-go/apm/internal/rootfs"
)

// ResolvedMCPServer is a self-defined MCP server after mf-013 resolution for
// one target's dispatch mode. Refused==true means the caller must not write
// this server at all (an unresolved ${input:} or ${VAR} in its URL).
type ResolvedMCPServer struct {
	Name      string
	Transport string
	Command   string
	Args      []string
	URL       string
	Env       map[string]string
	Headers   map[string]string
	Refused   bool
	Diags     []string
}

func envLookup(name string) (string, bool) {
	return os.LookupEnv(name)
}

// resolveMCPServer runs mf-013 resolution over one server's args/env/headers/
// url. args/env/url use envMode; headers use headerMode -- decoupled so a
// target that bakes env can still keep header placeholders verbatim (M8: the
// Python original always preserves ${VAR} in headers, issue #1152, even when
// it bakes env). Command and Transport are not placeholder-bearing fields in
// practice and pass through unresolved. In translate mode, an authored env
// value with no placeholder is rewritten to ${<key>} so secrets never bake
// into a translate-mode target's config (design.md §3).
func resolveMCPServer(s *manifest.MCPDependency, mode, headerMode manifest.ResolveMode) *ResolvedMCPServer {
	r := &ResolvedMCPServer{Name: s.Name, Transport: s.Transport, Command: s.Command}

	if s.Args != nil {
		for _, a := range *s.Args {
			out, diags, refuse, _ := manifest.ResolvePlaceholders(a, mode, manifest.PosArgs, envLookup)
			r.Diags = append(r.Diags, diags...)
			if refuse {
				r.Refused = true
			}
			r.Args = append(r.Args, out)
		}
	}

	if len(s.Env) > 0 {
		r.Env = map[string]string{}
		for _, k := range sortedKeys(s.Env) {
			v := s.Env[k]
			out, diags, refuse, omit := manifest.ResolvePlaceholders(v, mode, manifest.PosEnvDict, envLookup)
			r.Diags = append(r.Diags, diags...)
			if refuse {
				r.Refused = true
				continue
			}
			if omit {
				continue
			}
			if mode == manifest.ResolveTranslate && !manifest.HasPlaceholder(v) {
				out = fmt.Sprintf("${%s}", k)
			}
			r.Env[k] = out
		}
	}

	if len(s.Headers) > 0 {
		r.Headers = map[string]string{}
		for _, k := range sortedKeys(s.Headers) {
			v := s.Headers[k]
			out, diags, refuse, omit := manifest.ResolvePlaceholders(v, headerMode, manifest.PosHeader, envLookup)
			r.Diags = append(r.Diags, diags...)
			if refuse {
				r.Refused = true
				continue
			}
			if omit {
				continue
			}
			r.Headers[k] = out
		}
	}

	if s.URL != "" {
		out, diags, refuse, _ := manifest.ResolvePlaceholders(s.URL, mode, manifest.PosURL, envLookup)
		r.Diags = append(r.Diags, diags...)
		if refuse {
			r.Refused = true
		}
		// manifest.ValidateMCP's embedded-credential guard only ever sees
		// the AUTHORED value (e.g. "${MCP_URL}", which has no literal "@"),
		// not what a placeholder resolves to at deploy time. If the actual
		// environment has MCP_URL=https://user:pass@host/mcp, bake-mode
		// resolution just substituted that credential in here -- refuse to
		// deploy it rather than let it reach a target's config file in
		// plaintext (found by codex review: this is the same
		// embedded-credential policy as ValidateMCP, applied at the point
		// a placeholder actually becomes a literal value).
		if strings.Contains(out, "@") {
			r.Refused = true
			r.Diags = append(r.Diags, fmt.Sprintf("mcp %q: resolved url contains embedded credentials; refusing to deploy", s.Name))
		}
		r.URL = out
	}

	return r
}

// consideredNames returns the set of MCP server names evaluated in this
// deploy run, for mergeMCPServers's redeploy-cleanup logic.
func consideredNames(prims []Primitive) map[string]bool {
	names := make(map[string]bool, len(prims))
	for _, p := range prims {
		names[p.Name] = true
	}
	return names
}

// entryNames returns the server names that buildMCPEntries actually
// produced an entry for, i.e. the servers this target is about to write
// (excludes refused/skipped names), for MCPTarget.WriteMCP's written return.
func entryNames(entries map[string]map[string]any) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mcpEntryBuilder converts a resolved server into a target-specific JSON/TOML
// entry. ok=false with a non-empty skipReason means the server is diagnosed
// and skipped (e.g. codex SSE); ok=false with an empty skipReason means the
// caller already diagnosed it (used for the shared refuse/https checks).
type mcpEntryBuilder func(r *ResolvedMCPServer) (entry map[string]any, ok bool, skipReason string)

// buildMCPEntries resolves every self-defined MCP primitive via mf-013 for
// mode, drops refused and non-https-remote servers with diagnostics, and
// delegates the remaining per-server shape to build.
func buildMCPEntries(prims []Primitive, mode, headerMode manifest.ResolveMode, build mcpEntryBuilder) (entries map[string]map[string]any, diags []string) {
	entries = map[string]map[string]any{}
	for _, p := range prims {
		s := p.MCP
		r := resolveMCPServer(s, mode, headerMode)
		diags = append(diags, r.Diags...)
		if r.Refused {
			diags = append(diags, fmt.Sprintf("mcp %q: refused (unresolved placeholder)", s.Name))
			continue
		}
		// A translate-mode URL that starts with an unresolved placeholder was
		// passed through verbatim by resolveMCPServer (design.md D4: runtime
		// resolves it, e.g. via GitHub Actions/Copilot's own secret/input
		// substitution) -- its eventual scheme is unknowable at deploy time.
		// Placeholders later in the URL do not hide a literal scheme such as
		// http://, so those URLs still go through the https guard.
		deferredToRuntime := remoteURLDeferredToRuntime(mode, r.URL)
		if r.Transport != "stdio" && !deferredToRuntime && !strings.HasPrefix(r.URL, "https://") {
			diags = append(diags, fmt.Sprintf("mcp %q: non-https remote URL, skipped", s.Name))
			continue
		}
		entry, ok, skipReason := build(r)
		if !ok {
			if skipReason != "" {
				diags = append(diags, fmt.Sprintf("mcp %q: %s", s.Name, skipReason))
			}
			continue
		}
		entries[s.Name] = entry
	}
	return entries, diags
}

func remoteURLDeferredToRuntime(mode manifest.ResolveMode, rawURL string) bool {
	if mode != manifest.ResolveTranslate {
		return false
	}
	return placeholderAtStart(rawURL)
}

func placeholderAtStart(s string) bool {
	if loc := manifest.ActionsRe.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return true
	}
	if loc := manifest.InputVarRe.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return true
	}
	if loc := manifest.EnvVarRe.FindStringIndex(s); loc != nil && loc[0] == 0 {
		return true
	}
	return false
}

// managedMCPKeys are the per-server keys any writer in this package might
// set. A key on an existing entry that is NOT in this set is treated as
// user/foreign and always preserved across redeploys; a managed key that is
// no longer produced this run (e.g. an env var became undefined and was
// omitted) is dropped rather than left stale from a prior successful run.
var managedMCPKeys = map[string]bool{
	"command": true, "args": true, "env": true, "headers": true,
	"url": true, "serverUrl": true, "type": true, "id": true, "http_headers": true,
	"environment": true, "enabled": true,
}

// mergeMCPServers folds this run's entries into the existing servers map.
// considered is every server name evaluated this run, whether it ended up in
// entries or was refused/skipped -- those names are fully replaced (dropped
// entirely if refused/skipped, since a stale copy of a now-invalid server
// must not survive a redeploy). Names outside considered are left completely
// untouched: they are no longer declared at all, and stale-server cleanup is
// explicitly out of scope for this task (design.md §5).
func mergeMCPServers(existing map[string]any, entries map[string]map[string]any, considered map[string]bool) map[string]any {
	merged := map[string]any{}
	for name, v := range existing {
		if !considered[name] {
			merged[name] = v
		}
	}
	for name := range considered {
		entry, written := entries[name]
		if !written {
			continue
		}
		out := map[string]any{}
		if prev, ok := existing[name].(map[string]any); ok {
			for k, v := range prev {
				if !managedMCPKeys[k] {
					out[k] = v
				}
			}
		}
		for k, v := range entry {
			out[k] = v
		}
		merged[name] = out
	}
	return merged
}

// readExistingMCPRoot loads an existing merged-MCP-config root via unmarshal.
// A missing file starts fresh (root=empty map). Any other read failure (e.g.
// permission denied) or a file that exists but fails to parse is an error --
// silently discarding either would destroy whatever the user had, possibly
// including hand-authored foreign keys.
// It reads through a directory handle on projectDir rather than a joined path
// string: an MCP config can embed a resolved secret, and both the read that
// decides what to preserve and the write that follows must land on the file
// the project actually contains, not on whatever an ancestor swapped for a
// link between them (external audit 2026-09-08).
func readExistingMCPRoot(projectDir, rel string, unmarshal func([]byte, any) error) (map[string]any, error) {
	rw, err := rootfs.OpenRootWriter(projectDir)
	if err != nil {
		return nil, err
	}
	defer rw.Close()

	data, err := rw.ReadFile(rel)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read existing %s: %w", rw.Path(rel), err)
	}
	root := map[string]any{}
	if err := unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("existing %s is not valid, refusing to overwrite: %w", rw.Path(rel), err)
	}
	return root, nil
}

// writeMergedMCPJSON reads an existing JSON file at path (if any), merges
// entries (keyed by server name) into the map at topKey per mergeMCPServers,
// and writes the result with perm.
func writeMergedMCPJSON(projectDir, rel, topKey string, entries map[string]map[string]any, considered map[string]bool, perm os.FileMode) error {
	root, err := readExistingMCPRoot(projectDir, rel, json.Unmarshal)
	if err != nil {
		return err
	}
	existing, _ := root[topKey].(map[string]any)
	root[topKey] = mergeMCPServers(existing, entries, considered)

	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	return writeMCPFile(projectDir, rel, data, perm)
}

// writeMCPFile writes data at rel under projectDir with perm enforced on
// every write, not just on create.
//
// The perm enforcement is the point: os.WriteFile's mode argument is only
// honored on create (POSIX open() semantics), so an existing file left at
// 0644 by a git checkout, or by a prior config another tool authored, would
// keep its old mode after a plain rewrite. A bake-mode MCP config can embed
// resolved secret values verbatim, so 0600 has to be re-asserted each time.
//
// rootfs.WriteFileAtomicMode supplies both halves of what this needs: the
// mode is forced (WriteFileAtomic's own mode-preserving behaviour would be
// exactly wrong here), and the file arrives by rename rather than by
// truncation, so a hard link planted at rel keeps its own contents. The
// previous chmod/WriteFile/chmod sequence had neither property.
func writeMCPFile(projectDir, rel string, data []byte, perm os.FileMode) error {
	rw, err := rootfs.OpenRootWriter(projectDir)
	if err != nil {
		return err
	}
	defer rw.Close()
	return rw.WriteFileAtomicMode(rel, data, perm)
}
