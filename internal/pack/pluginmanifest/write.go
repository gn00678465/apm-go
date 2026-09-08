package pluginmanifest

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/ux"
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
//   - on success -> "Generated plugin manifest: <path>", wrote=true
//
// Containment is enforced via internal/marketplace/build.EnsureWithinRoot
// (mirrors ensure_path_within), reused rather than reimplemented per
// design.md's Surgical Changes note. m.ToJSONValue()/bundle.MarshalIndent
// preserve field order and never HTML-escape (Python's json.dumps parity).
func Write(w io.Writer, projectRoot, ecosystem string, m *bundle.PluginManifest, force, dryRun bool) (wrote bool, err error) {
	relPath, ok := PluginEcosystemPaths[ecosystem]
	if !ok {
		ux.Warn(w, "unknown plugin ecosystem %q; skipping plugin.json generation.", ecosystem)
		return false, nil
	}

	absPath, err := build.EnsureWithinRoot(projectRoot, relPath)
	if err != nil {
		return false, err
	}

	if dryRun {
		ux.Info(w, "Would write plugin manifest to %s", absPath)
		return false, nil
	}

	// Every write below goes through the RootWriter, whose directory handle
	// is taken once here. The path string absPath is used only for messages
	// from this point on: a check that returns a string cannot stop an
	// ancestor from being swapped for a junction before the write reaches it
	// (external audit 2026-08-13, reproduced locally), and the second
	// EnsureWithinRoot this code used to make after MkdirAll only narrowed
	// that window rather than closing it.
	rw, err := build.OpenRootWriter(projectRoot)
	if err != nil {
		return false, err
	}
	defer rw.Close()

	if _, statErr := rw.Stat(relPath); statErr == nil {
		if !force {
			ux.Warn(w, "%s already exists; skipping plugin.json generation. Re-run with --force to overwrite it.", absPath)
			return false, nil
		}
		ux.Warn(w, "Overwriting %s with generated manifest from apm.yml (--force).", absPath)
	}

	if strings.HasPrefix(filepath.ToSlash(relPath), ".github/") {
		ux.Info(w, "Writing generated plugin manifest under .github/: %s", absPath)
	}

	data := append(bundle.MarshalIndent(m.ToJSONValue()), '\n')
	if err := rw.WriteFileAtomic(relPath, data); err != nil {
		return false, err
	}

	// Oracle core/plugin_manifest.py:483-484 emits this through
	// _emit(..., "check"), so the stream glyph is "[+]".
	ux.Check(w, "Generated plugin manifest: %s", absPath)
	return true, nil
}
