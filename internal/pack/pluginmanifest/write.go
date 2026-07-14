package pluginmanifest

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/pack/bundle"
)

// PluginEcosystemPaths mirrors core/plugin_manifest.py's
// PLUGIN_ECOSYSTEM_PATHS: the output path (relative to the project root)
// for each supported ecosystem's plugin.json.
var PluginEcosystemPaths = map[string]string{
	"claude":  ".claude-plugin/plugin.json",
	"copilot": ".github/plugin/plugin.json",
}

// Write writes m as ecosystem's plugin.json under projectRoot, mirroring
// write_plugin_manifest's full overwrite/dry-run/logging contract
// (core/plugin_manifest.py:388-467):
//
//   - unknown ecosystem -> warns to msgs, writes nothing, wrote=false
//   - dry-run -> info "Would write plugin manifest to ..." only, wrote=false
//   - existing file + no force -> warns "already exists; skipping ...",
//     wrote=false (the pre-existing file is left byte-for-byte untouched)
//   - existing file + force -> warns "Overwriting ... (--force).", writes,
//     wrote=true
//   - a .github/-rooted path (copilot) gets an extra info line, since
//     GitHub Actions grants elevated trust to generated content there
//   - on success -> "[+] Generated plugin manifest: <path>", wrote=true
//
// Containment is enforced via internal/marketplace/build.EnsureWithinRoot
// (mirrors ensure_path_within), reused rather than reimplemented per
// design.md's Surgical Changes note. m.ToJSONValue()/bundle.MarshalIndent
// preserve field order and never HTML-escape (Python's json.dumps parity).
func Write(w io.Writer, projectRoot, ecosystem string, m *bundle.PluginManifest, force, dryRun bool) (wrote bool, err error) {
	relPath, ok := PluginEcosystemPaths[ecosystem]
	if !ok {
		fmt.Fprintf(w, "[warn] unknown plugin ecosystem %q; skipping plugin.json generation.\n", ecosystem)
		return false, nil
	}

	absPath, err := build.EnsureWithinRoot(projectRoot, relPath)
	if err != nil {
		return false, err
	}

	if dryRun {
		fmt.Fprintf(w, "[i] Would write plugin manifest to %s\n", absPath)
		return false, nil
	}

	if _, statErr := os.Stat(absPath); statErr == nil {
		if !force {
			fmt.Fprintf(w, "[warn] %s already exists; skipping plugin.json generation. Re-run with --force to overwrite it.\n", absPath)
			return false, nil
		}
		fmt.Fprintf(w, "[warn] Overwriting %s with generated manifest from apm.yml (--force).\n", absPath)
	}

	if strings.HasPrefix(filepath.ToSlash(relPath), ".github/") {
		fmt.Fprintf(w, "[i] Writing generated plugin manifest under .github/: %s\n", absPath)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return false, fmt.Errorf("create plugin manifest directory: %w", err)
	}
	// Re-check containment after mkdir to shrink the TOCTOU window, mirroring
	// write_plugin_manifest's second ensure_path_within call.
	if _, err := build.EnsureWithinRoot(projectRoot, relPath); err != nil {
		return false, err
	}

	data := append(bundle.MarshalIndent(m.ToJSONValue()), '\n')
	if err := os.WriteFile(absPath, data, 0o644); err != nil {
		return false, fmt.Errorf("write plugin manifest %s: %w", absPath, err)
	}

	fmt.Fprintf(w, "[+] Generated plugin manifest: %s\n", absPath)
	return true, nil
}
