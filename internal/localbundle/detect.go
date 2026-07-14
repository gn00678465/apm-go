// Package localbundle implements `apm-go install <local-bundle-path>`'s
// imperative, resolver-free deploy path for a bundle directory/.zip/.tar.gz
// produced by `apm-go pack` (research/pack-parity-findings.md §6; design.md
// "install <bundle-path> 消費回路"), mirroring Python's
// bundle/local_bundle.py + install/local_bundle_handler.py +
// install/services.py::integrate_local_bundle.
package localbundle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/pack/bundle"
	"github.com/apm-go/apm/internal/yamlcore"
)

// BundleInfo describes a detected local bundle, mirroring Python's
// bundle/local_bundle.py::LocalBundleInfo -- narrowed to the fields this
// package's consumers (verify.go/integrate.go, cmd/apm-go/install.go) actually
// use (design.md's "plugin_json"/"package_id" fields are Python-side
// logging/aliasing details this task does not port -- no --as ALIAS support
// exists in apm-go's install command).
type BundleInfo struct {
	// SourceDir is the bundle's root directory on disk -- for an archive,
	// this points inside the TempDir extraction directory.
	SourceDir string
	// HasLockfile reports whether the bundle carries an apm.lock.yaml that
	// parsed as a YAML mapping document at all (bundles produced by an
	// older APM version, or hand-assembled ones, may have none) --
	// mirrors Python's `bundle_info.lockfile is None` check.
	HasLockfile bool
	// HasPackMeta reports whether that apm.lock.yaml has a "pack:"
	// top-level section (see bundle.ParsePackMetadata). Always true for a
	// bundle produced by `apm-go pack`/`apm pack` whenever HasLockfile is
	// true.
	HasPackMeta bool
	PackMeta    bundle.PackMetadata
	// PackTargets is PackMeta.Target split on commas and trimmed, mirroring
	// _extract_pack_targets (bundle/local_bundle.py:113-125). Empty when
	// HasPackMeta is false or Target is empty.
	PackTargets []string
	// TempDir is the archive-extraction temp directory the caller must
	// remove once done (via Cleanup) -- empty for a directory bundle
	// (nothing to clean up).
	TempDir string
}

// Cleanup removes the archive-extraction temp directory, if any. Safe to
// call on a directory bundle (no-op) or more than once.
func (b *BundleInfo) Cleanup() {
	if b.TempDir != "" {
		os.RemoveAll(b.TempDir)
	}
}

// DetectLocalBundle probes path, returning a BundleInfo when it recognizes
// an APM-pack bundle: a directory with plugin.json at its root, or a
// .zip/.tar.gz/.tgz archive whose extracted root has one -- mirroring
// Python's detect_local_bundle (bundle/local_bundle.py:222-260). Returns
// (nil, nil) when path does not exist, or exists but is not a recognized
// bundle shape (the caller falls through to the ordinary
// dependency-resolution install path, or -- for an archive extension that
// still isn't a valid bundle -- raises its own targeted usage error, IM7).
// Returns a non-nil error only when an archive fails to extract at all
// (corrupt container OR a security violation: path traversal, symlink/hard
// link entry, entry-count/size cap exceeded) -- deliberately more
// conservative than Python's fail-open-on-corrupt-archive behavior (which
// only raises for an explicit ValueError security signal): a security-
// sensitive extraction step failing closed on ANY error, rather than
// string-matching to distinguish "corrupt" from "attack", is the safer and
// simpler rule (see also DetectLocalBundle's caller in cmd/apm-go/install.go,
// which surfaces this error as "bundle security check failed").
func DetectLocalBundle(path string) (*BundleInfo, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil
	}

	if fi.IsDir() {
		if !isRegularFile(filepath.Join(path, "plugin.json")) {
			return nil, nil
		}
		return buildBundleInfo(path, "")
	}

	lower := strings.ToLower(path)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return detectArchiveBundle(path, extractZipArchive)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return detectArchiveBundle(path, extractTarGzArchive)
	default:
		return nil, nil
	}
}

func detectArchiveBundle(archivePath string, extract func(archivePath, dest string) error) (*BundleInfo, error) {
	tempDir, err := os.MkdirTemp("", "apm-go-local-bundle-*")
	if err != nil {
		return nil, fmt.Errorf("create bundle extraction temp dir: %w", err)
	}
	if err := extract(archivePath, tempDir); err != nil {
		os.RemoveAll(tempDir)
		return nil, fmt.Errorf("extract bundle archive %s: %w", archivePath, err)
	}

	root := findExtractedRoot(tempDir)
	if root == "" {
		os.RemoveAll(tempDir)
		return nil, nil
	}

	info, err := buildBundleInfo(root, tempDir)
	if err != nil {
		os.RemoveAll(tempDir)
		return nil, err
	}
	return info, nil
}

func extractZipArchive(archivePath, dest string) error {
	_, err := archive.SafeExtractZip(archivePath, dest, archive.Limits{})
	return err
}

func extractTarGzArchive(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = archive.SafeExtract(f, dest, archive.Limits{})
	return err
}

// findExtractedRoot locates the bundle root inside an extraction directory,
// mirroring _find_extracted_root (bundle/local_bundle.py:173-188): `apm
// pack`'s archive layout nests contents under a single top-level
// "<name>-<version>/" directory, so this checks extractDir itself first,
// then a single directory child, then (defensively) any child at all.
// Returns "" when no plugin.json is found anywhere shallow enough to
// qualify.
func findExtractedRoot(extractDir string) string {
	if isRegularFile(filepath.Join(extractDir, "plugin.json")) {
		return extractDir
	}
	entries, err := os.ReadDir(extractDir)
	if err != nil {
		return ""
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 1 && isRegularFile(filepath.Join(extractDir, dirs[0], "plugin.json")) {
		return filepath.Join(extractDir, dirs[0])
	}
	for _, d := range dirs {
		if isRegularFile(filepath.Join(extractDir, d, "plugin.json")) {
			return filepath.Join(extractDir, d)
		}
	}
	return ""
}

// buildBundleInfo reads bundleDir's apm.lock.yaml (if any) and extracts its
// pack: metadata section, mirroring Python's _build_info
// (bundle/local_bundle.py:128-140). A missing or unparseable apm.lock.yaml
// is not an error -- HasLockfile stays false (fail-open detection,
// mirroring Python's `_read_bundle_lockfile`'s own except-and-return-None).
func buildBundleInfo(bundleDir, tempDir string) (*BundleInfo, error) {
	info := &BundleInfo{SourceDir: bundleDir, TempDir: tempDir}

	data, err := os.ReadFile(filepath.Join(bundleDir, "apm.lock.yaml"))
	if err != nil {
		return info, nil
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil || len(doc.Content) == 0 {
		return info, nil
	}
	info.HasLockfile = true

	meta, ok := bundle.ParsePackMetadata(doc.Content[0])
	if !ok {
		return info, nil
	}
	info.HasPackMeta = true
	info.PackMeta = meta
	info.PackTargets = splitCommaTrim(meta.Target)
	return info, nil
}

func splitCommaTrim(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
