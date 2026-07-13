package localbundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/pack/bundle"
)

// allowedExtraBundleFiles are the only files a bundle may carry that are
// never listed in pack.bundle_files, mirroring _ALLOWED_EXTRAS
// (bundle/local_bundle.py:348): the embedded lockfile itself and the
// top-level plugin.json (BundleProducer always appends plugin.json to
// output_files -- findings §3.4 -- so in practice it IS listed too, but
// Python excludes it unconditionally regardless).
var allowedExtraBundleFiles = map[string]bool{"apm.lock.yaml": true, "plugin.json": true}

// VerifyBundleIntegrity walks bundleDir and verifies every file against
// meta.BundleFiles, mirroring verify_bundle_integrity
// (bundle/local_bundle.py:282-357). Returns a list of human-readable error
// strings -- empty means the bundle is intact. Symlinks anywhere under
// bundleDir are always rejected, even when not listed in the manifest (a
// symlink injected after pack time is itself a tampering signal). A file
// present in the bundle but absent from meta.BundleFiles (other than
// apm.lock.yaml/plugin.json) is also flagged: the manifest is the source of
// truth, so an "unlisted" file is exactly as suspicious as a hash mismatch.
func VerifyBundleIntegrity(bundleDir string, meta bundle.PackMetadata) []string {
	var errs []string

	// 1) Reject any symlink under the bundle root, regardless of manifest.
	_ = filepath.WalkDir(bundleDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || p == bundleDir {
			return nil
		}
		info, lerr := os.Lstat(p)
		if lerr == nil && info.Mode()&os.ModeSymlink != 0 {
			rel, rerr := filepath.Rel(bundleDir, p)
			if rerr == nil {
				errs = append(errs, fmt.Sprintf("Symlink rejected in bundle: %s", filepath.ToSlash(rel)))
			}
		}
		return nil
	})

	// 2) Verify each file listed in pack.bundle_files (sorted for
	// deterministic error ordering, mirroring Python's `sorted(...)`).
	listed := make(map[string]bool, len(meta.BundleFiles))
	relKeys := make([]string, 0, len(meta.BundleFiles))
	for rel := range meta.BundleFiles {
		relKeys = append(relKeys, rel)
	}
	sort.Strings(relKeys)

	for _, rel := range relKeys {
		expected := meta.BundleFiles[rel]
		if !safeBundleRelPath(rel) {
			errs = append(errs, fmt.Sprintf("Unsafe bundle_files entry %q: path escapes the bundle root", rel))
			continue
		}
		listed[rel] = true
		target := filepath.Join(bundleDir, filepath.FromSlash(rel))

		fi, lerr := os.Lstat(target)
		if lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
			continue // already reported by the symlink sweep above
		}
		if lerr != nil || !fi.Mode().IsRegular() {
			errs = append(errs, fmt.Sprintf("Missing bundle file: %s", rel))
			continue
		}
		data, rerr := os.ReadFile(target)
		if rerr != nil {
			errs = append(errs, fmt.Sprintf("Cannot read bundle file %s: %v", rel, rerr))
			continue
		}
		normalizedExpected, nerr := normalizeHash(expected)
		if nerr != nil {
			errs = append(errs, fmt.Sprintf("Invalid hash for %s: %v", rel, nerr))
			continue
		}
		sum := sha256.Sum256(data)
		actual := hex.EncodeToString(sum[:])
		if actual != normalizedExpected {
			errs = append(errs, fmt.Sprintf("Hash mismatch for %s: expected %s..., got %s...",
				rel, truncate12(normalizedExpected), truncate12(actual)))
		}
	}

	// 3) Detect extra files present in the bundle but not listed in
	// pack.bundle_files -- anything outside the manifest is a tampering
	// signal, the only allowed exclusions being the bundle's own
	// apm.lock.yaml and plugin.json.
	_ = filepath.WalkDir(bundleDir, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		info, lerr := os.Lstat(p)
		if lerr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(bundleDir, p)
		if rerr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if allowedExtraBundleFiles[relSlash] || listed[relSlash] {
			return nil
		}
		errs = append(errs, fmt.Sprintf("Unlisted bundle file (not in pack.bundle_files): %s", relSlash))
		return nil
	})

	return errs
}

// normalizeHash strips an optional "sha256:" prefix and lowercases the hex
// digest, mirroring _normalize_hash (bundle/local_bundle.py:268-279).
// Returns an error for any other algorithm prefix, so a tampered lockfile
// declaring an unsupported algorithm cannot be silently accepted.
func normalizeHash(value string) (string, error) {
	if strings.HasPrefix(value, "sha256:") {
		return strings.ToLower(strings.TrimSpace(strings.TrimPrefix(value, "sha256:"))), nil
	}
	if strings.Contains(value, ":") {
		return "", fmt.Errorf("unsupported hash algorithm prefix in: %q", value)
	}
	return strings.ToLower(strings.TrimSpace(value)), nil
}

func truncate12(s string) string {
	if len(s) <= 12 {
		return s
	}
	return s[:12]
}

// safeBundleRelPath rejects a bundle_files key that would escape the
// bundle root, mirroring validate_path_segments + ensure_path_within
// (bundle/local_bundle.py:310-320): empty, absolute, or containing a ".."
// segment.
func safeBundleRelPath(rel string) bool {
	if rel == "" {
		return false
	}
	norm := strings.ReplaceAll(rel, "\\", "/")
	if path.IsAbs(norm) || filepath.IsAbs(rel) || filepath.VolumeName(filepath.FromSlash(rel)) != "" {
		return false
	}
	for _, seg := range strings.Split(norm, "/") {
		if seg == ".." {
			return false
		}
	}
	return true
}

// CheckTargetMismatch returns a warning string when bundleTargets are not
// covered by installTargets, mirroring check_target_mismatch
// (bundle/local_bundle.py:365-399). Returns "" when bundleTargets is empty
// (a pre-constraint bundle carrying no target metadata), bundleTargets
// contains "all" (a target-agnostic bundle), or installTargets is a
// superset of bundleTargets.
func CheckTargetMismatch(bundleTargets, installTargets []string) string {
	bundleSet := trimmedSet(bundleTargets)
	if len(bundleSet) == 0 {
		return ""
	}
	if bundleSet["all"] {
		return ""
	}
	installSet := trimmedSet(installTargets)

	var missing []string
	for t := range bundleSet {
		if !installSet[t] {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	sort.Strings(missing)

	installList := sortedKeys(installSet)
	installStr := strings.Join(installList, ", ")
	if installStr == "" {
		installStr = "<none>"
	}
	return fmt.Sprintf(
		"Bundle was packed for targets [%s] but install resolved to [%s]. "+
			"The following packed targets will not receive files: %s",
		strings.Join(sortedKeys(bundleSet), ", "), installStr, strings.Join(missing, ", "))
}

func trimmedSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, t := range items {
		t = strings.TrimSpace(t)
		if t != "" {
			set[t] = true
		}
	}
	return set
}

func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
