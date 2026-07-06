package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// mcpRemoveTarget describes one MCP config file's removal shape: the path
// (relative to projectDir, forward-slash form), the top-level key servers
// live under, whether the file is TOML (else JSON), and the permission
// writeMergedMCP*/writeFileWithPerm enforces on rewrite. Every field here
// mirrors the corresponding target's WriteMCP exactly (mcp_claude.go/
// mcp_codex.go/mcp_copilot.go/mcp_antigravity.go/mcp_opencode.go), so removal
// and install never diverge on where a server lives.
type mcpRemoveTarget struct {
	relPath string
	topKey  string
	isTOML  bool
	perm    os.FileMode
}

var mcpRemoveTargets = []mcpRemoveTarget{
	{relPath: ".mcp.json", topKey: "mcpServers", perm: 0600},
	{relPath: ".codex/config.toml", topKey: "mcp_servers", isTOML: true, perm: 0600},
	{relPath: ".github/mcp-config.json", topKey: "mcpServers", perm: 0644},
	{relPath: ".agents/mcp_config.json", topKey: "mcpServers", perm: 0600},
	{relPath: "opencode.json", topKey: "mcp", perm: 0600},
}

// RemoveMCPServersFromTargets deletes serverNames from every target's MCP
// config file (claude/codex/copilot/antigravity/opencode), reusing the exact
// same writeMergedMCPJSON/writeMergedMCPTOML + mergeMCPServers path install's
// WriteMCP writes through: considered=serverNames with entries={} makes
// mergeMCPServers drop those names entirely, while every other server --
// including anything the user hand-edited, and every foreign top-level key --
// is left byte-for-byte untouched (design.md N7 §7a, un-060~062/065).
//
// A target whose config file does not exist, or whose topKey currently has
// none of serverNames, is left alone entirely (no file is created, nothing is
// rewritten) -- this mirrors WriteMCP's own "nothing to do, don't touch the
// file" convention (empty-behavior parity with install: install never writes
// a target's config file when there is nothing to write, and removal must
// not either).
//
// Returns diagnostics for any target whose existing config file could not be
// read (permission error) or parsed (malformed) -- that target is skipped
// (left untouched) rather than aborting removal for every other target.
func RemoveMCPServersFromTargets(projectDir string, serverNames []string) (diags []string) {
	if len(serverNames) == 0 {
		return nil
	}
	considered := make(map[string]bool, len(serverNames))
	for _, name := range serverNames {
		considered[name] = true
	}
	entries := map[string]map[string]any{}

	for _, t := range mcpRemoveTargets {
		path := filepath.Join(projectDir, filepath.FromSlash(t.relPath))
		unmarshal := json.Unmarshal
		if t.isTOML {
			unmarshal = toml.Unmarshal
		}
		root, err := readExistingMCPRoot(path, unmarshal)
		if err != nil {
			diags = append(diags, fmt.Sprintf("mcp uninstall %s: %v", t.relPath, err))
			continue
		}
		existing, _ := root[t.topKey].(map[string]any)
		if !anyServerPresent(existing, considered) {
			continue
		}

		var writeErr error
		if t.isTOML {
			writeErr = writeMergedMCPTOML(path, t.topKey, entries, considered, t.perm)
		} else {
			writeErr = writeMergedMCPJSON(path, t.topKey, entries, considered, t.perm)
		}
		if writeErr != nil {
			diags = append(diags, fmt.Sprintf("mcp uninstall %s: %v", t.relPath, writeErr))
		}
	}
	return diags
}

// anyServerPresent reports whether existing (a target's current topKey map)
// has an entry for any name in considered -- i.e. whether this target
// actually has something to remove.
func anyServerPresent(existing map[string]any, considered map[string]bool) bool {
	for name := range considered {
		if _, ok := existing[name]; ok {
			return true
		}
	}
	return false
}
