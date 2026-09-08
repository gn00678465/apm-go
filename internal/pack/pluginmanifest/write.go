package pluginmanifest

import (
	"fmt"
	"io"
	"os"
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

	if _, statErr := os.Stat(absPath); statErr == nil {
		if !force {
			ux.Warn(w, "%s already exists; skipping plugin.json generation. Re-run with --force to overwrite it.", absPath)
			return false, nil
		}
		ux.Warn(w, "Overwriting %s with generated manifest from apm.yml (--force).", absPath)
	}

	if strings.HasPrefix(filepath.ToSlash(relPath), ".github/") {
		ux.Info(w, "Writing generated plugin manifest under .github/: %s", absPath)
	}

	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		return false, fmt.Errorf("create plugin manifest directory: %w", err)
	}
	// Re-check containment after mkdir to shrink the TOCTOU window, mirroring
	// write_plugin_manifest's second ensure_path_within call.
	//
	// The re-resolved path replaces absPath rather than being discarded:
	// EnsureWithinRoot returns the RESOLVED path precisely so that callers
	// write to the location that was checked (output.go's doc comment), and
	// MkdirAll has just materialised directories that did not exist during the
	// first call -- resolveSymlinks treats a missing component as literal
	// (output.go:276-280), so the first call's answer was taken before those
	// components could be resolved at all.
	checkedPath, err := build.EnsureWithinRoot(projectRoot, relPath)
	if err != nil {
		return false, err
	}
	absPath = checkedPath

	data := append(bundle.MarshalIndent(m.ToJSONValue()), '\n')
	if err := writeFileAtomic(absPath, data); err != nil {
		return false, err
	}

	// Oracle core/plugin_manifest.py:483-484 emits this through
	// _emit(..., "check"), so the stream glyph is "[+]".
	ux.Check(w, "Generated plugin manifest: %s", absPath)
	return true, nil
}

// writeFileAtomic writes data to path via a temp file in the same directory
// plus a rename, matching build.WriteOutput's existing pattern for the
// marketplace document.
//
// os.WriteFile is not used because it TRUNCATES the existing file in place,
// which writes through every name that file has. An external audit
// (2026-08-12) showed the consequence: a hard link planted inside the project
// at .codex-plugin/plugin.json, pointing at a file outside the project, has
// no distinguishing feature any path check can see -- Lstat reports an
// ordinary file -- and `pack --force` destroyed the outside file's contents.
// A rename replaces the directory entry instead, so the other name keeps its
// original inode and contents. Verified end to end: same fixture, victim file
// unchanged afterwards.
//
// Containment still governs WHERE the temp file and the final path live; this
// only changes HOW the bytes land.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".plugin-*.json.tmp")
	if err != nil {
		return fmt.Errorf("create temp file for %s: %w", path, err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file for %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file for %s: %w", path, err)
	}
	if err := os.Chmod(tmpPath, manifestMode(path)); err != nil {
		return fmt.Errorf("chmod temp file for %s: %w", path, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("write plugin manifest %s: %w", path, err)
	}
	return nil
}

// manifestMode is the permission set writeFileAtomic gives the manifest: the
// mode an existing manifest already carries, or 0644 for a new one.
//
// os.CreateTemp always creates 0600 and ignores the process umask, so the temp
// file's mode has to be set explicitly. The unconditional os.Chmod(tmp, 0o644)
// this replaced got the new-file case roughly right but reset the mode of
// every file it overwrote -- os.WriteFile(path, data, 0o644), which the atomic
// write replaced (86ada0b), applies its mode argument only when it CREATES the
// file, so a manifest a user or a collaborator had narrowed to 0600 stayed
// 0600 across `pack --force` before that commit and stopped doing so after it.
//
// Known deviation, recorded rather than silently kept: for a NEW manifest this
// still bypasses the process umask, because the kernel applies umask only to
// the mode passed to open(2) and os.CreateTemp hardcodes 0600. Reproducing
// os.WriteFile exactly would mean hand-rolling the temp-file creation with
// os.OpenFile(O_CREATE|O_EXCL) and its own random naming, which trades a
// well-tested stdlib primitive for ~20 lines of security-relevant code. The
// sibling writer for marketplace.json (build.WriteOutput) does not chmod at
// all and therefore leaves 0600; these two should be made consistent, which is
// a separate decision from this regression fix.
func manifestMode(path string) os.FileMode {
	if info, err := os.Stat(path); err == nil {
		return info.Mode().Perm()
	}
	return 0o644
}
