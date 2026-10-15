package pluginmanifest

import (
	"io"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/rootfs"
	"github.com/apm-go/apm/internal/ux"
)

// PluginEcosystemPaths mirrors core/plugin_manifest.py's
// PLUGIN_ECOSYSTEM_PATHS: the output path (relative to the project root)
// for each supported ecosystem's plugin.json.
var PluginEcosystemPaths = map[string]string{
	"claude":  ".claude-plugin/plugin.json",
	"copilot": ".github/plugin/plugin.json",
}

// Outcome is the decision Write made for one ecosystem's plugin.json: the
// Oracle's PluginManifestWriteResult.action (core/plugin_manifest.py:401-483).
type Outcome int

const (
	Skipped Outcome = iota
	DryRun
	Written
)

// Write writes m as ecosystem's plugin.json under projectRoot and returns the
// absolute path it reported with the decision it made, in the order of
// write_plugin_manifest_with_outcome (core/plugin_manifest.py:401-483):
//
//   - unknown ecosystem -> warns, writes nothing, Skipped
//   - existing file + no force -> warns "already exists; skipping ...",
//     Skipped, also under dry-run (the file is left byte-for-byte untouched)
//   - dry-run -> info "Would overwrite ... (--force)." for an existing file,
//     "Would write plugin manifest to ..." otherwise, DryRun; nothing is
//     created
//   - existing file + force -> warns "Overwriting ... (--force).", writes
//   - a .github/-rooted path (copilot) gets an extra info line, since
//     GitHub Actions grants elevated trust to generated content there
//   - on success -> "Generated plugin manifest: <path>", Written
//
// The existence check comes before the dry-run branch and is the same Stat
// for both, so --dry-run previews the decision the real run makes.
//
// Containment is enforced via internal/marketplace/build.EnsureWithinRoot
// (mirrors ensure_path_within), reused rather than reimplemented per
// design.md's Surgical Changes note. m.ToJSONValue()/bundle.MarshalIndent
// preserve field order and never HTML-escape (Python's json.dumps parity).
func Write(w io.Writer, projectRoot, ecosystem string, m *bundle.PluginManifest, force, dryRun bool) (absPath string, outcome Outcome, err error) {
	relPath, ok := PluginEcosystemPaths[ecosystem]
	if !ok {
		ux.Warn(w, "unknown plugin ecosystem %q; skipping plugin.json generation.", ecosystem)
		return "", Skipped, nil
	}

	if _, err := build.EnsureWithinRoot(projectRoot, relPath); err != nil {
		return "", Skipped, err
	}
	// Oracle reports project_root / rel_path with only the root resolved
	// (commands/pack.py:363, core/plugin_manifest.py:432): a symlinked
	// plugin.json or .claude-plugin is named as written, since that entry is
	// what the RootWriter replaces.
	resolvedRoot, err := build.EnsureWithinRoot(projectRoot, ".")
	if err != nil {
		return "", Skipped, err
	}
	absPath = filepath.Join(resolvedRoot, filepath.FromSlash(relPath))

	// Every write below goes through the RootWriter, whose directory handle
	// is taken once here. The path string absPath is used only for messages
	// from this point on: a check that returns a string cannot stop an
	// ancestor from being swapped for a junction before the write reaches it
	// (external audit 2026-08-13, reproduced locally), and the second
	// EnsureWithinRoot this code used to make after MkdirAll only narrowed
	// that window rather than closing it.
	rw, err := rootfs.OpenRootWriter(projectRoot)
	if err != nil {
		return "", Skipped, err
	}
	defer rw.Close()

	_, statErr := rw.Stat(relPath)
	exists := statErr == nil
	if exists && !force {
		ux.Warn(w, "%s already exists; skipping plugin.json generation. Re-run with --force to overwrite it.", absPath)
		return absPath, Skipped, nil
	}

	if dryRun {
		if exists {
			ux.Info(w, "Would overwrite plugin manifest at %s with generated manifest from apm.yml (--force).", absPath)
		} else {
			ux.Info(w, "Would write plugin manifest to %s", absPath)
		}
		return absPath, DryRun, nil
	}

	if exists {
		ux.Warn(w, "Overwriting %s with generated manifest from apm.yml (--force).", absPath)
	}

	if strings.HasPrefix(filepath.ToSlash(relPath), ".github/") {
		ux.Info(w, "Writing generated plugin manifest under .github/: %s", absPath)
	}

	data := append(bundle.MarshalIndent(m.ToJSONValue()), '\n')
	if err := rw.WriteFileAtomic(relPath, data); err != nil {
		return "", Skipped, err
	}

	// Oracle core/plugin_manifest.py:483-484 emits this through
	// _emit(..., "check"), so the stream glyph is "[+]".
	ux.Check(w, "Generated plugin manifest: %s", absPath)
	return absPath, Written, nil
}
