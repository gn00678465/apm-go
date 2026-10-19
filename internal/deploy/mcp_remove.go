package deploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// mcpRemoveTarget describes one MCP config file's removal shape: the target
// that writes it, the name the Oracle's stale cleanup gives it in its output
// (integration/mcp_integrator.py:705-965), the path (relative to the deploy
// root, forward-slash form), the top-level key servers live under, whether
// the file is TOML (else JSON), and the permission writeMergedMCP*/
// writeFileWithPerm enforces on rewrite. Every field here mirrors the
// corresponding target's WriteMCP exactly (mcp_claude.go/mcp_codex.go/
// mcp_copilot.go/mcp_antigravity.go/mcp_opencode.go), so removal and install
// never diverge on where a server lives.
type mcpRemoveTarget struct {
	target  string
	label   string
	relPath string
	topKey  string
	isTOML  bool
	perm    os.FileMode
	// keyOrder is set for a file written through writeOrderedMCPJSON.
	keyOrder []string
}

var mcpRemoveTargets = []mcpRemoveTarget{
	{target: "claude", label: ".mcp.json", relPath: ".mcp.json", topKey: "mcpServers", perm: 0600, keyOrder: claudeMCPKeyOrder},
	{target: "codex", label: "Codex CLI config", relPath: ".codex/config.toml", topKey: "mcp_servers", isTOML: true, perm: 0600},
	{target: "copilot", label: "Copilot CLI config", relPath: ".github/mcp-config.json", topKey: "mcpServers", perm: 0644},
	{target: "antigravity", label: "Antigravity MCP config", relPath: ".agents/mcp_config.json", topKey: "mcpServers", perm: 0600},
	{target: "opencode", label: "OpenCode MCP config", relPath: "opencode.json", topKey: "mcp", perm: 0600},
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
	for _, t := range mcpRemoveTargets {
		if _, err := removeMCPServers(projectDir, t, serverNames); err != nil {
			diags = append(diags, fmt.Sprintf("mcp uninstall %s: %v", t.relPath, err))
		}
	}
	return diags
}

// MCPConfigLabel is the name the Oracle's stale cleanup gives target's MCP
// config file, or "" when target writes none.
func MCPConfigLabel(target string) string {
	if t, ok := mcpRemoveTargetFor(target); ok {
		return t.label
	}
	return ""
}

// RemoveStaleMCPServers deletes names from the MCP config file target wrote
// under root and returns the names it removed, sorted. A target that writes
// no MCP config, a file that does not exist and a file that has none of the
// names are left alone. The install that calls it must not record a state
// the files do not have, so every failure is an error, not a diagnostic.
//
// The file and each directory between root and it are refused when one is
// a symlink, before anything is read, as the Oracle refuses them
// (integration/mcp_integrator.py:58-95); root itself is not checked, so a
// HOME reached through a link still works for --global.
func RemoveStaleMCPServers(root, target string, names []string) ([]string, error) {
	t, ok := mcpRemoveTargetFor(target)
	if !ok || len(names) == 0 {
		return nil, nil
	}
	if err := refuseSymlinkedMCPConfig(root, t); err != nil {
		return nil, err
	}
	removed, err := removeMCPServers(root, t, names)
	if err != nil {
		return nil, fmt.Errorf("MCP cleanup failed for %s: %v. Check the config path and permissions, then retry.", t.label, err)
	}
	return removed, nil
}

func mcpRemoveTargetFor(target string) (mcpRemoveTarget, bool) {
	for _, t := range mcpRemoveTargets {
		if t.target == target {
			return t, true
		}
	}
	return mcpRemoveTarget{}, false
}

func refuseSymlinkedMCPConfig(root string, t mcpRemoveTarget) error {
	parts := strings.Split(t.relPath, "/")
	for i := range parts {
		info, err := os.Lstat(filepath.Join(root, filepath.Join(parts[:i+1]...)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		// The Oracle counts a path it cannot inspect as a symlink.
		if err == nil && info.Mode()&fs.ModeSymlink == 0 {
			continue
		}
		abs, absErr := filepath.Abs(filepath.Join(root, filepath.FromSlash(t.relPath)))
		if absErr != nil {
			abs = filepath.Join(root, filepath.FromSlash(t.relPath))
		}
		return fmt.Errorf("Refusing to clean symlinked MCP config: %s (%s). Replace the symlink with a regular file or directory, then retry.", t.label, abs)
	}
	return nil
}

// removeMCPServers writes t's config file without names and returns the
// ones it had, sorted; a file with none of them is not written.
func removeMCPServers(root string, t mcpRemoveTarget, names []string) ([]string, error) {
	unmarshal := json.Unmarshal
	if t.isTOML {
		unmarshal = toml.Unmarshal
	}
	doc, err := readExistingMCPRoot(root, t.relPath, unmarshal)
	if err != nil {
		return nil, err
	}
	existing, _ := doc[t.topKey].(map[string]any)
	considered := make(map[string]bool, len(names))
	var present []string
	for _, name := range names {
		if _, ok := existing[name]; ok && !considered[name] {
			present = append(present, name)
		}
		considered[name] = true
	}
	if len(present) == 0 {
		return nil, nil
	}
	sort.Strings(present)

	entries := map[string]map[string]any{}
	switch {
	case t.isTOML:
		err = writeMergedMCPTOML(root, t.relPath, t.topKey, entries, considered, t.perm)
	case t.keyOrder != nil:
		err = writeOrderedMCPJSON(root, t.relPath, t.topKey, entries, considered, nil, t.keyOrder, t.perm)
	default:
		err = writeMergedMCPJSON(root, t.relPath, t.topKey, entries, considered, t.perm)
	}
	if err != nil {
		return nil, err
	}
	return present, nil
}
