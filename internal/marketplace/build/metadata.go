// This file (metadata.go) implements mkt-050 修訂版 (c)'s metadata
// enrichment for BOTH remote and local packages: each package's own
// description/version, read from its own apm.yml -- a remote package's at
// the ref ResolvePackages just resolved (enrichRemoteMetadata), a local
// package's on disk within the project (enrichLocalMetadata, F1 fix; the
// original implementation only enriched remote packages, leaving local
// packages' description/version permanently blank whenever the curator's
// own marketplace.packages[] entry omitted them). The curator's
// marketplace.packages[] entry always wins when it already supplies a
// usable value; each package's own apm.yml is only fetched/read to fill in
// whatever the curator left out, and a fetch/read failure never fails the
// build -- it downgrades to a warning message ResolvePackages surfaces to
// its caller (design.md: "抓取失敗降級為「無 metadata」警告,不中斷 build").
// Both a remote clone's and a local package's apm.yml are size-capped
// (64KiB) before being read into memory (F4 fix, readCapped).
package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/marketplace/authoring"
	"github.com/apm-go/apm/internal/yamlcore"

	"go.yaml.in/yaml/v4"
)

// remoteMetadataMaxBytes bounds a remote (cloned, untrusted) package's own
// apm.yml (F4): reading and parsing an unbounded file from an arbitrary
// third-party repo is unsafe, so anything over this cap is skipped
// (downgraded to a warning by enrichRemoteMetadata's caller) rather than
// read into memory in full. Mirrors localMetadataMaxBytes' identical cap
// for a local package's own apm.yml (F1) and the Python original's
// _LOCAL_METADATA_MAX_BYTES.
const remoteMetadataMaxBytes = 64 * 1024

// localMetadataMaxBytes bounds a local package's own apm.yml (F1), read
// straight off disk by enrichLocalMetadata -- mirroring
// remoteMetadataMaxBytes' identical remote-side cap and the Python
// original's _LOCAL_METADATA_MAX_BYTES.
const localMetadataMaxBytes = 64 * 1024

// readCapped reads path but refuses to return content over maxBytes,
// erroring instead of silently truncating -- the shared size-cap primitive
// behind both F1 (a local package's own apm.yml) and F4 (a remote
// package's own apm.yml).
func readCapped(path string, maxBytes int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, fmt.Errorf("exceeds %d byte size cap", maxBytes)
	}
	return data, nil
}

// MetadataFetcher abstracts "read description/version from a remote
// package's own apm.yml" so tests can substitute a fake instead of
// performing a real git clone.
type MetadataFetcher interface {
	// FetchMetadata returns the description/version declared by source's
	// own apm.yml at ref (read from subdir/apm.yml when subdir is
	// non-empty, otherwise from the repo root). Either return value may be
	// "" when the remote apm.yml has no such field, or parses to something
	// other than a mapping -- that is not itself an error. An error return
	// means the fetch itself could not be completed (clone failed, the
	// file does not exist, or it is not valid YAML).
	FetchMetadata(source, ref, subdir string) (description, version string, err error)
}

// DefaultMetadataFetcher is ResolvePackages' production MetadataFetcher: a
// git clone pinned to the resolved ref/sha into a temporary directory,
// reading <subdir>/apm.yml from the checkout.
var DefaultMetadataFetcher MetadataFetcher = gitMetadataFetcher{}

type gitMetadataFetcher struct{}

// metadataFetchTimeout bounds how long a single metadata-fetch clone may
// run before being killed, mirroring reflister.go's listRefsTimeout. A var,
// not a const, so tests can shrink it.
var metadataFetchTimeout = 30 * time.Second

const metadataCloneTempDirPrefix = "apm-pack-metadata-*"

func (gitMetadataFetcher) FetchMetadata(source, ref, subdir string) (string, string, error) {
	cloneURL := resolveCloneURL(source)

	tmpDir, err := os.MkdirTemp("", metadataCloneTempDirPrefix)
	if err != nil {
		return "", "", fmt.Errorf("create temp clone directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	ctx, cancel := context.WithTimeout(context.Background(), metadataFetchTimeout)
	defer cancel()

	if err := cloneAtRef(ctx, cloneURL, ref, tmpDir); err != nil {
		return "", "", err
	}

	apmYmlPath := filepath.Join(tmpDir, filepath.FromSlash(subdir), "apm.yml")
	data, err := readCapped(apmYmlPath, remoteMetadataMaxBytes)
	if err != nil {
		return "", "", fmt.Errorf("read remote apm.yml at %q: %w", filepath.ToSlash(filepath.Join(subdir, "apm.yml")), err)
	}

	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return "", "", fmt.Errorf("parse remote apm.yml: %w", err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", "", nil
	}
	root := doc.Content[0]
	return metadataScalarString(root, "description"), metadataScalarString(root, "version"), nil
}

// cloneAtRef clones cloneURL into dir, pinned to ref. A 40-char lowercase
// hex ref is a literal commit SHA -- `git clone --branch` cannot resolve
// that, so it takes a full clone + `git checkout <sha>` instead (mirroring
// gitops/clone.go's cloneRepoAtCommit); any other ref is a tag or branch
// name, cloned directly via `--depth 1 --branch <ref>` (mirroring
// client_git.go's shallowCloneGit).
func cloneAtRef(ctx context.Context, cloneURL, ref, dir string) error {
	safeURL := gitops.SanitizeGitOutput(cloneURL)

	if sha40LowerRe.MatchString(ref) {
		cloneCmd := exec.CommandContext(ctx, "git", "clone", cloneURL, dir)
		gitops.ApplySecureGitEnv(cloneCmd)
		if out, err := cloneCmd.CombinedOutput(); err != nil {
			return cloneError(ctx, safeURL, out, err)
		}
		checkoutCmd := exec.CommandContext(ctx, "git", "-C", dir, "checkout", ref)
		gitops.ApplySecureGitEnv(checkoutCmd)
		if out, err := checkoutCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git checkout %s: %s", ref, gitops.SanitizeGitOutput(strings.TrimSpace(string(out))))
		}
		return nil
	}

	cmd := exec.CommandContext(ctx, "git", "clone", "--depth", "1", "--branch", ref, cloneURL, dir)
	gitops.ApplySecureGitEnv(cmd)
	if out, err := cmd.CombinedOutput(); err != nil {
		return cloneError(ctx, safeURL, out, err)
	}
	return nil
}

func cloneError(ctx context.Context, safeURL string, out []byte, err error) error {
	if ctx.Err() == context.DeadlineExceeded {
		return fmt.Errorf("git clone %s: timed out after %s", safeURL, metadataFetchTimeout)
	}
	return fmt.Errorf("git clone %s: %s", safeURL, gitops.SanitizeGitOutput(strings.TrimSpace(string(out))))
}

// metadataScalarString reads key's scalar string value out of mapping node
// m ("" when the key is absent, explicit null, or not a scalar).
func metadataScalarString(m *yaml.Node, key string) string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			v := m.Content[i+1]
			if v.Kind == yaml.ScalarNode && v.ShortTag() != "!!null" {
				return v.Value
			}
			return ""
		}
	}
	return ""
}

// enrichRemoteMetadata implements mkt-050 修訂版 (c) for one resolved
// remote package. The curator's own PackageEntry value always wins when
// present -- for version, only when it is a genuine display version rather
// than a semver range/pattern (isDisplayVersion, mirroring Python's
// _is_display_version): a remote package's version: field is normally the
// RANGE used to resolve ref/sha, and mkt-050(c) forbids echoing that range
// back out as the plugin's displayed version. The package's own remote
// apm.yml is fetched only when at least one of description/version still
// needs a fallback value -- when the curator already supplies both, the
// fetch would never change the outcome, so it is skipped entirely
// (extending mkt-051's "local packages never touch the network" principle
// to this enrichment step). A fetch failure never fails the build: it
// downgrades to a warning message, and the fields fall back to whatever the
// curator already supplied (or "" when nothing is available).
func enrichRemoteMetadata(entry authoring.PackageEntry, ref, subdir, source string, fetcher MetadataFetcher) (description, version, warning string) {
	description = entry.Description
	if isDisplayVersion(entry.Version) {
		version = entry.Version
	}

	if description != "" && version != "" {
		return description, version, ""
	}

	remoteDescription, remoteVersion, err := fetcher.FetchMetadata(source, ref, subdir)
	if err != nil {
		return description, version, fmt.Sprintf(
			"package %q: could not fetch remote apm.yml metadata, continuing without it: %s",
			entry.Name, err,
		)
	}

	if description == "" {
		description = remoteDescription
	}
	if version == "" {
		version = remoteVersion
	}
	return description, version, ""
}

// localApmYMLPath resolves a local package's own apm.yml path within
// projectRoot, or ok=false when entry.Source (already req-mf-017-validated
// to start with "./" and contain no ".." segment, so it can never traverse
// outside projectRoot) resolves to projectRoot itself -- that file is the
// marketplace's own apm.yml, not a package manifest, mirroring the Python
// original's identical "package_root == project_root" skip in
// _fetch_local_metadata (builder.py).
func localApmYMLPath(projectRoot, source string) (path string, ok bool) {
	root := filepath.Clean(projectRoot)
	packageRoot := filepath.Clean(filepath.Join(projectRoot, source))
	if packageRoot == root {
		return "", false
	}
	return filepath.Join(packageRoot, "apm.yml"), true
}

// enrichLocalMetadata implements F1: a local package (source: "./...") is
// enriched from its own apm.yml on disk at
// <projectRoot>/<entry.Source>/apm.yml -- the same curator-wins precedence
// enrichRemoteMetadata applies for a remote package's own apm.yml, except a
// local package's curator `version` is never gated by isDisplayVersion (a
// local package's version: field is always a plain display value, never a
// resolution range, mirroring output_mappers.py's is_local branch, which
// applies no such filter to a local entry's version). A missing apm.yml is
// the ordinary "nothing to enrich from" case, not a failure; only a genuine
// read/parse error (over the size cap, unreadable, not valid YAML)
// downgrades to a warning -- either way enrichment is skipped rather than
// failing the build.
func enrichLocalMetadata(entry authoring.PackageEntry, projectRoot string) (description, version, warning string) {
	description = entry.Description
	version = entry.Version
	if description != "" && version != "" {
		return description, version, ""
	}

	apmYmlPath, ok := localApmYMLPath(projectRoot, entry.Source)
	if !ok {
		return description, version, ""
	}
	if _, statErr := os.Stat(apmYmlPath); statErr != nil {
		return description, version, ""
	}

	data, err := readCapped(apmYmlPath, localMetadataMaxBytes)
	if err != nil {
		return description, version, fmt.Sprintf(
			"package %q: could not read local apm.yml metadata, continuing without it: %s",
			entry.Name, err,
		)
	}

	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return description, version, fmt.Sprintf(
			"package %q: could not parse local apm.yml metadata, continuing without it: %s",
			entry.Name, err,
		)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return description, version, ""
	}
	root := doc.Content[0]
	if description == "" {
		description = metadataScalarString(root, "description")
	}
	if version == "" {
		version = metadataScalarString(root, "version")
	}
	return description, version, ""
}

// isDisplayVersion mirrors Python's output_mappers._is_display_version
// (mkt-050 修訂版 (c)): true when value looks like a fixed display version
// rather than a semver range/pattern -- it doesn't start with a range
// operator, contains no whitespace or wildcard "*", and its final
// dot-separated segment is not the literal "x" wildcard (case-insensitive).
func isDisplayVersion(value string) bool {
	if value == "" {
		return false
	}
	trimmed := strings.TrimSpace(value)
	for _, prefix := range []string{"^", "~", ">", "<", "="} {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	if strings.Contains(trimmed, " ") || strings.Contains(trimmed, "*") {
		return false
	}
	segments := strings.Split(strings.ToLower(trimmed), ".")
	return segments[len(segments)-1] != "x"
}
