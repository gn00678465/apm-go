package localbundle

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/pack/bundle"
)

// IntegrateResult mirrors integrate_local_bundle's return shape (Python:
// {"deployed_files": [...], "deployed_file_hashes": {...}}), narrowed to
// what cmd/apm/install.go's caller needs to print a summary and persist
// LocalDeployedFiles/LocalDeployedHashes.
type IntegrateResult struct {
	Files  []string
	Hashes map[string]string
	Diags  []string
}

// targetRouting mirrors one entry of Python's KNOWN_TARGETS
// (integration/targets.py), restricted to the fields integrate_local_bundle
// (install/services.py:702-1057) actually consults for a local-bundle
// deploy: RootDir is TargetProfile.root_dir (the default deploy root every
// bundle-relative path is joined against); SkillsRoot is the "skills"
// primitive's PrimitiveMapping.deploy_root override when Python sets one
// ("" means no override -- falls back to RootDir, exactly like Claude's own
// skills primitive); HasInstructions reports whether "instructions" is a
// key in that target's primitives dict, which gates Python's compile-only-
// target staging branch (see deployBundleFile's doc comment for what
// apm-go does instead).
type targetRouting struct {
	RootDir         string
	SkillsRoot      string
	HasInstructions bool
}

// targetRoutingTable is a literal, hand-verified transcription of
// targets.py's KNOWN_TARGETS entries for exactly the six targets apm-go's
// deploy.Adapters registers (adapter.go) -- an adapterless target
// (cursor/gemini/windsurf/...) never reaches this table because
// cmd/apm/install.go's deploy.ResolveTargets already filters to
// deploy.Adapters keys before calling IntegrateLocalBundle.
var targetRoutingTable = map[string]targetRouting{
	"claude":       {RootDir: ".claude", HasInstructions: true},
	"copilot":      {RootDir: ".github", SkillsRoot: ".agents", HasInstructions: true},
	"codex":        {RootDir: ".codex", SkillsRoot: ".agents"},
	"opencode":     {RootDir: ".opencode", SkillsRoot: ".agents"},
	"antigravity":  {RootDir: ".agents", HasInstructions: true},
	"agent-skills": {RootDir: ".agents"},
}

// IntegrateLocalBundle deploys bundleDir's already-plugin-final content
// (whatever pack.bundle_files lists) into every resolved target, mirroring
// integrate_local_bundle (install/services.py:702-1057).
//
// This is a deliberately independent code path from deploy.Run: a local
// bundle has no apm.yml, no lockfile-resolved dependency graph, and no
// per-dependency provenance to thread through the resolver -- it is an
// imperative deploy of already-merged, already-conflict-resolved bundle
// content (BundleProducer already ran file_map/hooks/mcp merge, and
// PluginManifestProducer/BundleProducer authors already wrote each file
// under its FINAL per-client name at pack time -- e.g. "agents/foo.agent.md"
// -- not an apm-go .apm/ source primitive awaiting a per-target naming
// transform). Deploying it means routing each pack.bundle_files entry,
// UNCHANGED byte-for-byte and UNCHANGED filename, to the correct resolved
// target's deploy root: NOT re-deriving a deploy.Primitive from it and
// running it back through deploy.Adapters[target].DeployPrimitive (which
// would re-apply that target's OWN naming/extension convention a second
// time -- the bug this rewrite fixes; see Gate 6b's dual-bundle-fixture
// transcript for the byte-identical proof against Python's own `apm
// install`).
//
// MCP wiring is unaffected by this rewrite and still runs after the main
// deploy loop via collectBundleMCPPrimitives + deploy.MCPTarget.WriteMCP --
// mirroring local_bundle_handler.py's _wire_bundle_mcp_servers, which
// ALSO deploys through the per-target MCPIntegrator rather than copying
// .mcp.json verbatim (.mcp.json is bundle metadata, filtered out of the
// verbatim per-file deploy loop below exactly like Python's pack_files
// filter, services.py:801-806).
//
// Returns an empty, non-nil result (no error) when targets is empty --
// mirroring Python's "no active targets resolved -- nothing will be
// deployed" warn-and-return (not a failure): the caller
// (cmd/apm/install.go) is responsible for deciding whether/how to warn
// before ever calling this function with an empty targets slice.
func IntegrateLocalBundle(bundleDir string, meta *bundle.PackMetadata, targets []string, projectDir string) (*IntegrateResult, error) {
	result := &IntegrateResult{Hashes: map[string]string{}}
	if len(targets) == 0 {
		return result, nil
	}

	relKeys := bundleDeployFileRels(bundleDir, meta)

	mcpPrims, mcpDiags := collectBundleMCPPrimitives(bundleDir)
	result.Diags = append(result.Diags, mcpDiags...)

	for _, target := range targets {
		routing, ok := targetRoutingTable[target]
		if !ok {
			continue
		}
		for _, rel := range relKeys {
			record, deployed, err := deployBundleFile(bundleDir, rel, routing, target, projectDir, result)
			if err != nil {
				return nil, err
			}
			if !deployed {
				continue
			}
			hash, herr := lockfile.HashFileBytes(filepath.Join(projectDir, filepath.FromSlash(record)))
			if herr != nil {
				return nil, fmt.Errorf("hash deployed file %s: %w", record, herr)
			}
			result.Files = append(result.Files, record)
			result.Hashes[record] = hash
		}
	}

	if len(mcpPrims) > 0 {
		mcpFilesWritten := 0
		for _, target := range targets {
			adapter, ok := deploy.Adapters[target]
			if !ok {
				continue
			}
			mcpAdapter, ok := adapter.(deploy.MCPTarget)
			if !ok {
				continue
			}
			files, _, diags, err := mcpAdapter.WriteMCP(mcpPrims, projectDir)
			if err != nil {
				result.Diags = append(result.Diags, fmt.Sprintf("write mcp config for %s failed: %v", target, err))
				continue
			}
			result.Diags = append(result.Diags, diags...)
			for _, f := range files {
				mcpFilesWritten++
				hash, herr := lockfile.HashFileBytes(filepath.Join(projectDir, f))
				if herr != nil {
					return nil, fmt.Errorf("hash mcp file %s: %w", f, herr)
				}
				result.Files = append(result.Files, f)
				result.Hashes[f] = hash
			}
		}
		if mcpFilesWritten == 0 {
			result.Diags = append(result.Diags, fmt.Sprintf(
				"bundle .mcp.json declared %d server(s); no MCP config changes for the resolved target(s)", len(mcpPrims)))
		}
	}

	sort.Strings(result.Files)
	result.Files = dedupeSorted(result.Files)
	return result, nil
}

// deployBundleFile routes and verbatim-copies bundleDir/rel to target's
// resolved deploy root, mirroring one iteration of integrate_local_bundle's
// inner per-target/per-file loop (services.py:877-1043) restricted to what
// apm-go's local-bundle consumer supports today. Returns ok=false (no
// error) for every case Python's loop would `skipped += 1; continue` on --
// this function does not track that counter (result.Diags carries the
// human-readable reasons instead).
//
// Two Python special cases are handled as DOCUMENTED DEVIATIONS rather than
// full ports (design.md's Gate 1 disposition table, Gate 6b "此修正不做什麼"):
//
//   - extensions/* (Anthropic "canvas" executable bundles): Python only
//     deploys these when BOTH an experimental feature flag AND
//     --trust-canvas-extensions are set; by default (both off) Python
//     itself silently drops them before the per-target loop even starts.
//     apm-go has no --trust-canvas-extensions flag and no "canvas"
//     PrimitiveType in internal/deploy (a pre-existing gap, not something
//     this task adds) -- so apm-go's behavior already matches Python's own
//     default (off) behavior: always drop, never deploy.
//   - instructions/* for a target with no native "instructions" primitive
//     (codex, opencode in apm-go's registered set): Python stages these
//     under apm_modules/<slug>/.apm/instructions/ for a later `apm compile`
//     to merge into AGENTS.md/equivalent. apm-go has no local-bundle
//     compile-staging counterpart (and no `apm-go compile` consumer for a
//     staged bundle instruction file), so these files are skipped with a
//     diagnostic instead of silently vanishing. test1 (Gate 6b's fixture)
//     only exercises claude+copilot, both of which DO have a native
//     instructions primitive, so this deviation does not affect that
//     fixture's byte-identical comparison.
func deployBundleFile(bundleDir, rel string, routing targetRouting, target, projectDir string, result *IntegrateResult) (record string, ok bool, err error) {
	firstSeg := ""
	if idx := strings.IndexByte(rel, '/'); idx >= 0 {
		firstSeg = rel[:idx]
	}

	if firstSeg == "extensions" {
		return "", false, nil
	}
	if firstSeg == "instructions" && !routing.HasInstructions {
		result.Diags = append(result.Diags, fmt.Sprintf(
			"target %q has no native instructions surface; skipped bundle file %s "+
				"(Python stages these under apm_modules/<slug>/.apm/instructions/ for "+
				"'apm compile' -- local-bundle compile-staging is a documented deviation, not yet ported)",
			target, rel))
		return "", false, nil
	}
	if !safeBundleRelPath(rel) {
		result.Diags = append(result.Diags, fmt.Sprintf("skipped unsafe bundle entry %q", rel))
		return "", false, nil
	}

	root := routing.RootDir
	if firstSeg == "skills" && routing.SkillsRoot != "" {
		root = routing.SkillsRoot
	}

	// A leaf-only os.Lstat(srcPath) is fooled by a reparse point (e.g. an
	// NTFS junction) sitting at an INTERMEDIATE path segment: Windows
	// transparently resolves every path component except the final one, so
	// Lstat on the full joined path reports whatever regular file sits on
	// the OTHER side of the junction, never the junction itself (Gate 6b's
	// B2 finding -- VerifyBundleIntegrity's own sweep already rejects this
	// case for a normal `install` call, but IntegrateLocalBundle must not
	// rely on always being called after it).
	if hasReparsePointAncestor(bundleDir, rel) {
		return "", false, nil
	}

	srcPath := filepath.Join(bundleDir, filepath.FromSlash(rel))
	info, lerr := os.Lstat(srcPath)
	if lerr != nil || isSymlinkOrReparsePoint(info) || !info.Mode().IsRegular() {
		return "", false, nil
	}

	absDest, derr := build.EnsureWithinRoot(filepath.Join(projectDir, filepath.FromSlash(root)), filepath.FromSlash(rel))
	if derr != nil {
		result.Diags = append(result.Diags, fmt.Sprintf("skipped unsafe bundle entry %q: %v", rel, derr))
		return "", false, nil
	}

	data, rerr := os.ReadFile(srcPath)
	if rerr != nil {
		return "", false, fmt.Errorf("read bundle file %s: %w", rel, rerr)
	}
	if normalized, isText := normalizedBundleText(rel, data); isText {
		data = normalized
	}
	if err := os.MkdirAll(filepath.Dir(absDest), 0o755); err != nil {
		return "", false, fmt.Errorf("create deploy dir for %s: %w", rel, err)
	}
	if err := os.WriteFile(absDest, data, 0o644); err != nil {
		return "", false, fmt.Errorf("write bundle file %s: %w", rel, err)
	}

	return path.Join(root, rel), true, nil
}

// hasReparsePointAncestor reports whether any path segment from bundleDir
// down to (and including) rel is a symlink or Windows reparse point
// (isSymlinkOrReparsePoint) -- not just rel's own leaf component, which
// os.Lstat(filepath.Join(bundleDir, rel)) alone cannot see through an
// intermediate junction.
func hasReparsePointAncestor(bundleDir, rel string) bool {
	cur := bundleDir
	for _, seg := range strings.Split(path.Clean(filepath.ToSlash(rel)), "/") {
		if seg == "" || seg == "." {
			continue
		}
		cur = filepath.Join(cur, seg)
		info, err := os.Lstat(cur)
		if err != nil {
			return false // let the caller's own Lstat/ReadFile surface this
		}
		if isSymlinkOrReparsePoint(info) {
			return true
		}
	}
	return false
}

// textBundleSuffixes is the exact set install/services.py's
// integrate_local_bundle (v0.23.1+) applies CRLF->LF text normalization to
// -- a lowercase, dot-prefixed extension match against services.py:779
// (`{".json", ".md", ".toml", ".txt", ".yaml", ".yml"}`).
var textBundleSuffixes = map[string]bool{
	".json": true, ".md": true, ".toml": true, ".txt": true, ".yaml": true, ".yml": true,
}

// normalizedBundleText returns rel's CRLF-normalized content when rel's
// extension is a text bundle suffix AND data decodes as valid UTF-8, else
// (nil, false) meaning "deploy data verbatim" -- mirrors
// _normalized_bundle_text + normalize_crlf_to_lf (install/services.py +
// utils/atomic_io.py, v0.23.1+): a bundle's markdown/JSON/TOML/YAML/txt
// files are LF-normalized on deploy so their content hash never diverges
// between a Windows-checked-out source repo (CRLF) and the consuming
// project (which always gets LF); a suffix match that fails UTF-8 decoding
// (rare, e.g. a binary file misnamed with a text extension) falls back to a
// raw byte copy, matching Python's UnicodeDecodeError fallback.
func normalizedBundleText(rel string, data []byte) ([]byte, bool) {
	if !textBundleSuffixes[strings.ToLower(path.Ext(rel))] {
		return nil, false
	}
	if !utf8.Valid(data) {
		return nil, false
	}
	normalized := strings.ReplaceAll(string(data), "\r\n", "\n")
	return []byte(normalized), true
}

// bundleDeployFileRels returns the sorted, deduplicated set of bundle-
// relative paths this bundle should deploy, mirroring integrate_local_bundle's
// pack_files derivation (services.py:766-806): meta.BundleFiles's keys when
// available (already vetted for path-safety/hash-integrity by an earlier
// VerifyBundleIntegrity call whenever meta came from a HasPackMeta bundle),
// else a fallback walk of bundleDir (matching Python's fallback for an
// older/non-apm-go-produced bundle with no bundle_files manifest -- issue
// #1098's "prevents zero-deploy when an older bundle lands"), skipping any
// symlink/reparse-point entry (isSymlinkOrReparsePoint) and never descending
// into one even if it looks like a directory. Either way, apm.lock.yaml/
// plugin.json/.mcp.json are excluded CASE-INSENSITIVELY (Gate 6b's A6
// finding: an uppercase-named metadata file -- e.g. a bundle carrying
// "APM.LOCK.YAML" verbatim on an NTFS volume, which is case-preserving, not
// case-sensitive -- must be excluded exactly like the lowercase form; the
// previous code only case-folded plugin.json/.mcp.json here, while the
// fallback walk excluded apm.lock.yaml via a separate, case-SENSITIVE
// comparison, so an uppercase-named lockfile fell through both filters and
// deployed as ordinary bundle content) -- they are bundle metadata, never
// deployable content, mirroring services.py:801-806's `_rel.lower() in
// {...}` filter.
func bundleDeployFileRels(bundleDir string, meta *bundle.PackMetadata) []string {
	seen := map[string]bool{}
	if meta != nil && len(meta.BundleFiles) > 0 {
		for rel := range meta.BundleFiles {
			seen[rel] = true
		}
	} else {
		_ = filepath.WalkDir(bundleDir, func(p string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return nil
			}
			info, lerr := os.Lstat(p)
			if lerr != nil {
				return nil
			}
			if isSymlinkOrReparsePoint(info) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if d.IsDir() || !info.Mode().IsRegular() {
				return nil
			}
			rel, rerr := filepath.Rel(bundleDir, p)
			if rerr != nil {
				return nil
			}
			seen[filepath.ToSlash(rel)] = true
			return nil
		})
	}

	out := make([]string, 0, len(seen))
	for rel := range seen {
		lower := strings.ToLower(rel)
		if lower == "plugin.json" || lower == ".mcp.json" || lower == "apm.lock.yaml" {
			continue
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

func dedupeSorted(sorted []string) []string {
	if len(sorted) < 2 {
		return sorted
	}
	out := sorted[:1]
	for _, f := range sorted[1:] {
		if f != out[len(out)-1] {
			out = append(out, f)
		}
	}
	return out
}

// collectBundleMCPPrimitives parses bundleDir/.mcp.json's mcpServers into
// deploy.Primitive (Type: TypeMCP), mirroring
// _parse_bundle_mcp_servers (install/local_bundle_handler.py:300-345):
// every bundle-shipped server is self-defined (Registry=false) -- a bundle's
// .mcp.json carries transport/command/url directly, never a registry
// indirection. Missing/unparseable/malformed input is fail-open (no
// servers, no error) matching Python; a per-server validation failure is
// reported as a diagnostic and that entry is skipped, not the whole file.
func collectBundleMCPPrimitives(bundleDir string) ([]deploy.Primitive, []string) {
	data, err := os.ReadFile(filepath.Join(bundleDir, ".mcp.json"))
	if err != nil {
		return nil, nil
	}
	root, derr := bundle.DecodeJSONValue(data)
	if derr != nil || root.Kind != bundle.KindObject {
		return nil, nil
	}
	servers, ok := root.Get("mcpServers")
	if !ok || servers.Kind != bundle.KindObject {
		return nil, nil
	}

	var prims []deploy.Primitive
	var diags []string
	for _, f := range servers.O {
		if f.Val.Kind != bundle.KindObject {
			continue
		}
		dep := mcpDependencyFromJSON(f.Key, f.Val)
		if verr := manifest.ValidateMCP(dep); verr != nil {
			diags = append(diags, fmt.Sprintf("bundle mcp server %q: %v; skipped", f.Key, verr))
			continue
		}
		prims = append(prims, deploy.Primitive{Name: dep.Name, Type: deploy.TypeMCP, Source: "local", MCP: dep})
	}
	return prims, diags
}

// mcpDependencyFromJSON converts one bundle .mcp.json server object (the
// Claude-Code-native mcpServers schema: {"type"|omitted implies
// stdio,"command","args","env"} for stdio, {"type","url","headers"} for a
// remote transport) into a *manifest.MCPDependency, mirroring
// MCPDependency.from_dict's field aliasing ("type" -> transport) applied by
// _parse_bundle_mcp_servers.
func mcpDependencyFromJSON(name string, v bundle.JSONValue) *manifest.MCPDependency {
	dep := &manifest.MCPDependency{Name: name, Registry: false}
	if t, ok := v.Get("type"); ok && t.Kind == bundle.KindString {
		dep.Transport = t.S
	}
	if c, ok := v.Get("command"); ok && c.Kind == bundle.KindString {
		dep.Command = c.S
	}
	if u, ok := v.Get("url"); ok && u.Kind == bundle.KindString {
		dep.URL = u.S
	}
	if a, ok := v.Get("args"); ok && a.Kind == bundle.KindArray {
		args := make([]string, 0, len(a.A))
		for _, e := range a.A {
			if e.Kind == bundle.KindString {
				args = append(args, e.S)
			}
		}
		dep.Args = &args
	}
	if e, ok := v.Get("env"); ok && e.Kind == bundle.KindObject {
		dep.Env = map[string]string{}
		for _, f := range e.O {
			if f.Val.Kind == bundle.KindString {
				dep.Env[f.Key] = f.Val.S
			}
		}
	}
	if h, ok := v.Get("headers"); ok && h.Kind == bundle.KindObject {
		dep.Headers = map[string]string{}
		for _, f := range h.O {
			if f.Val.Kind == bundle.KindString {
				dep.Headers[f.Key] = f.Val.S
			}
		}
	}
	return dep
}
