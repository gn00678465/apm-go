package bundle

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/marketplace/build"
	"github.com/apm-go/apm/internal/security"
)

// DepSource is one dependency's already-resolved install location, fed to
// BundleProducer's collection loop (findings §3.2 step 6). Name attributes
// collision messages; VirtualPath/RepoURL feed CollectBareSkill's slug
// derivation. Callers must already have filtered out dev dependencies
// before building this slice -- apm-go's lockfile has no is_dev flag
// (unlike Python's LockedDependency), so the caller derives the skip set
// from manifest.ParsedDevDeps + deploy.DepRefKey, matching Python's
// _get_dev_dependency_urls fallback path (findings §3.2 point 1).
type DepSource struct {
	Name        string
	InstallPath string
	VirtualPath string
	RepoURL     string
}

// ProduceOptions carries BundleProducer's inputs, mirroring
// export_plugin_bundle's parameters (plugin_exporter.py:416-425) restricted
// to this task's scope (§3.1/§3.7: --format apm/--archive/-t are out of
// scope).
type ProduceOptions struct {
	ProjectRoot string
	OutputDir   string // parent directory for the generated bundle, e.g. "<root>/build"
	PkgName     string
	PkgVersion  string // caller already applied the "0.0.0" fallback
	Target      string // pack.target metadata (comma-joined); pure informational metadata
	Force       bool
	DryRun      bool

	// HasLocalDep mirrors export_plugin_bundle's guard (plugin_exporter.py:
	// 454-462): true when any DIRECT dependencies.apm entry IsLocal ->
	// Produce rejects the whole bundle. Computed by the caller so this
	// package need not import internal/manifest.
	HasLocalDep bool

	Deps []DepSource

	// ApmYMLNode is apm.yml's parsed root mapping node, used only as the
	// Synthesize fallback when no on-disk plugin.json is found.
	ApmYMLNode *yaml.Node
	// SuppressMissingPluginJSONInfo mirrors _has_marketplace_block: true
	// when apm.yml also has a marketplace: block, suppressing the "No
	// plugin.json found; synthesising..." info line (it's the expected
	// path for authoring-plus-marketplace projects).
	SuppressMissingPluginJSONInfo bool

	// Lockfile/LockfileNode: when Lockfile is non-nil, its pack: section
	// is embedded as apm.lock.yaml inside the bundle (§3.6). nil means no
	// apm.lock.yaml was found -- skip embedding entirely (mirrors "if
	// lockfile is not None").
	Lockfile     *lockfile.Lockfile
	LockfileNode *yaml.Node
}

// ProduceResult mirrors Python's PackResult.
type ProduceResult struct {
	BundleDir string
	Files     []string // sorted output-relative file list
}

// Produce runs BundleProducer end to end, mirroring export_plugin_bundle
// (plugin_exporter.py:416-675): collect dependency + root-package
// components (deps first, root last -- §3.2's collection order), merge
// file_map/hooks/mcp per the documented direction table (§3.2), compute the
// sorted output file list, sanitize the bundle directory name, dry-run
// returns early (skipping the security scan entirely), otherwise scan
// source files (warn-only, never blocks), write everything, and -- when a
// lockfile was found -- embed a pack: section ahead of apm.lock.yaml.
func Produce(w io.Writer, opts ProduceOptions) (*ProduceResult, error) {
	if opts.HasLocalDep {
		return nil, fmt.Errorf("cannot pack -- apm.yml contains a local path dependency. " +
			"Local dependencies are for development only. Replace them with remote " +
			"references (e.g., 'owner/repo') before packing.")
	}

	fileMap := NewFileMap()
	mergedHooks := JSONValue{Kind: KindObject}
	mergedMCP := JSONValue{Kind: KindObject}
	var err error

	for _, dep := range opts.Deps {
		if !isDir(dep.InstallPath) {
			continue
		}
		depApmDir := filepath.Join(dep.InstallPath, ".apm")
		components := CollectAPMComponents(depApmDir)
		components = append(components, CollectRootPluginComponents(dep.InstallPath)...)
		components = append(components, CollectBareSkill(dep.InstallPath, dep.VirtualPath, dep.RepoURL, components)...)
		fileMap.MergeFileMap(components, dep.Name, opts.Force)

		depHooks, herr := DeepMerge(collectHooksFromAPM(depApmDir), collectHooksFromRoot(dep.InstallPath), false)
		if herr != nil {
			return nil, herr
		}
		if mergedHooks, err = DeepMerge(mergedHooks, depHooks, false); err != nil {
			return nil, err
		}

		sanitizedDepMCP, dropped := SanitizeServers(ReadMCPServers(dep.InstallPath))
		PrintSecretWarning(w, dropped)
		if mergedMCP, err = DeepMerge(mergedMCP, sanitizedDepMCP, false); err != nil {
			return nil, err
		}
	}

	// Root package's own components merge LAST -- file_map's collision
	// rule (dep wins without --force) falls directly out of this ordering,
	// not a separate branch (merge.go's MergeFileMap doc comment).
	ownApmDir := filepath.Join(opts.ProjectRoot, ".apm")
	ownComponents := CollectAPMComponents(ownApmDir)
	ownComponents = append(ownComponents, CollectRootPluginComponents(opts.ProjectRoot)...)
	fileMap.MergeFileMap(ownComponents, opts.PkgName, opts.Force)

	rootHooks, herr := DeepMerge(collectHooksFromAPM(ownApmDir), collectHooksFromRoot(opts.ProjectRoot), false)
	if herr != nil {
		return nil, herr
	}
	// Root package wins hooks/mcp unconditionally (overwrite=true) -- the
	// OPPOSITE direction from file_map.
	if mergedHooks, err = DeepMerge(mergedHooks, rootHooks, true); err != nil {
		return nil, err
	}
	rootMCP, rootDropped := SanitizeServers(ReadMCPServers(opts.ProjectRoot))
	PrintSecretWarning(w, rootDropped)
	if mergedMCP, err = DeepMerge(mergedMCP, rootMCP, true); err != nil {
		return nil, err
	}

	for _, c := range fileMap.Collisions {
		fmt.Fprintf(w, "[warn] %s\n", c)
	}

	outputFiles := fileMap.Keys()
	sort.Strings(outputFiles)
	if !mergedHooks.IsEmptyObject() {
		outputFiles = append(outputFiles, "hooks.json")
	}
	if !mergedMCP.IsEmptyObject() {
		outputFiles = append(outputFiles, ".mcp.json")
	}
	outputFiles = append(outputFiles, "plugin.json")

	safeName := sanitizeBundleName(opts.PkgName)
	safeVersion := sanitizeBundleName(opts.PkgVersion)
	bundleRel := safeName + "-" + safeVersion
	absBundleDir, err := build.EnsureWithinRoot(opts.OutputDir, bundleRel)
	if err != nil {
		return nil, err
	}

	if opts.DryRun {
		return &ProduceResult{BundleDir: absBundleDir, Files: outputFiles}, nil
	}

	scanBundleSources(w, fileMap, opts.Force)

	if err := os.RemoveAll(absBundleDir); err != nil {
		return nil, fmt.Errorf("clear existing bundle directory: %w", err)
	}
	if err := os.MkdirAll(absBundleDir, 0o755); err != nil {
		return nil, err
	}
	if err := writeBundleFiles(absBundleDir, fileMap); err != nil {
		return nil, err
	}

	// hooks.json/.mcp.json use sort_keys=True (plugin_exporter.py:616,622)
	// -- unlike plugin.json's sort_keys=False -- and neither gets a
	// trailing newline (only write_plugin_manifest's standalone top-level
	// plugin.json does).
	if !mergedHooks.IsEmptyObject() {
		if err := os.WriteFile(filepath.Join(absBundleDir, "hooks.json"), MarshalIndent(mergedHooks.SortedClone()), 0o644); err != nil {
			return nil, err
		}
	}
	if !mergedMCP.IsEmptyObject() {
		wrapped := ObjectValue(JSONField{Key: "mcpServers", Val: mergedMCP})
		if err := os.WriteFile(filepath.Join(absBundleDir, ".mcp.json"), MarshalIndent(wrapped.SortedClone()), 0o644); err != nil {
			return nil, err
		}
	}

	pluginJSON, err := findOrSynthesizePluginJSON(w, opts.ProjectRoot, opts.ApmYMLNode, opts.SuppressMissingPluginJSONInfo)
	if err != nil {
		return nil, err
	}
	pluginJSON = stripSchemaInvalidKeys(w, pluginJSON)
	if err := os.WriteFile(filepath.Join(absBundleDir, "plugin.json"), MarshalIndent(pluginJSON), 0o644); err != nil {
		return nil, err
	}

	if opts.Lockfile != nil {
		if err := embedPackLockfile(absBundleDir, opts.Lockfile, opts.LockfileNode, opts.Target); err != nil {
			return nil, err
		}
	}

	return &ProduceResult{BundleDir: absBundleDir, Files: outputFiles}, nil
}

// PrintSecretWarning mirrors _sanitize_mcp_servers's warning
// (core/plugin_manifest.py:270-277), printed by every caller of
// ReadMCPServers+SanitizeServers (both PluginManifestProducer and
// BundleProducer -- collect_mcp_servers is the single Python function that
// does both steps together and emits this warning internally). Exported so
// package pluginmanifest -- which calls ReadMCPServers/SanitizeServers
// separately too -- can print the identical warning.
func PrintSecretWarning(w io.Writer, dropped []string) {
	if len(dropped) == 0 {
		return
	}
	fmt.Fprintf(w, "[warn] Secrets withheld from plugin.json so they are never committed as "+
		"plaintext -- stripped from .mcp.json before writing: %s. Use $ENV_VAR references in "+
		".mcp.json to keep secrets out of the manifest.\n", strings.Join(dropped, ", "))
}

// collectHooksFromAPM returns merged hooks from apmDir/hooks/*.json,
// mirroring _collect_hooks_from_apm (plugin_exporter.py:237-251).
func collectHooksFromAPM(apmDir string) JSONValue {
	hooks := JSONValue{Kind: KindObject}
	entries, err := os.ReadDir(filepath.Join(apmDir, "hooks"))
	if err != nil {
		return hooks
	}
	for _, e := range entries {
		data, ok := readRegularFile(filepath.Join(apmDir, "hooks", e.Name()))
		if !ok || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		v, perr := DecodeJSONValue(data)
		if perr != nil || v.Kind != KindObject {
			continue
		}
		if merged, merr := DeepMerge(hooks, v, false); merr == nil {
			hooks = merged
		}
	}
	return hooks
}

// collectHooksFromRoot returns hooks from packageRoot/hooks.json and/or
// packageRoot/hooks/*.json, mirroring _collect_hooks_from_root
// (plugin_exporter.py:254-277).
func collectHooksFromRoot(packageRoot string) JSONValue {
	hooks := JSONValue{Kind: KindObject}
	if data, ok := readRegularFile(filepath.Join(packageRoot, "hooks.json")); ok {
		if v, perr := DecodeJSONValue(data); perr == nil && v.Kind == KindObject {
			if merged, merr := DeepMerge(hooks, v, false); merr == nil {
				hooks = merged
			}
		}
	}
	entries, err := os.ReadDir(filepath.Join(packageRoot, "hooks"))
	if err == nil {
		for _, e := range entries {
			data, ok := readRegularFile(filepath.Join(packageRoot, "hooks", e.Name()))
			if !ok || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			v, perr := DecodeJSONValue(data)
			if perr != nil || v.Kind != KindObject {
				continue
			}
			if merged, merr := DeepMerge(hooks, v, false); merr == nil {
				hooks = merged
			}
		}
	}
	return hooks
}

// readRegularFile reads path's contents, but only when it is a regular
// file that is not a symlink -- mirroring the "is_file() and not
// is_symlink()" guard every Python collector in this file applies.
func readRegularFile(path string) ([]byte, bool) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// scanBundleSources runs the warn-only hidden-Unicode scan over every
// file_map SOURCE (pre-copy) entry, mirroring plugin_exporter.py:568-593:
// symlinks are skipped, directories walk via SecurityGate.ScanFiles, files
// via SecurityGate.ScanText (WARN_POLICY never blocks regardless of
// force -- force only affects file_map/plugin.json overwrite, never the
// scan). Only the total finding count is reported (no per-file detail).
func scanBundleSources(w io.Writer, fileMap *FileMap, force bool) {
	total := 0
	for _, key := range fileMap.Keys() {
		src, _ := fileMap.Source(key)
		info, err := os.Lstat(src)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		if info.IsDir() {
			verdict := security.SecurityGate{}.ScanFiles(src, security.WarnPolicy, force)
			total += len(verdict.AllFindings())
			continue
		}
		data, err := os.ReadFile(src)
		if err != nil {
			continue
		}
		verdict := security.SecurityGate{}.ScanText(string(data), src, security.WarnPolicy)
		total += len(verdict.AllFindings())
	}
	if total > 0 {
		fmt.Fprintf(w, "[warn] Bundle contains %d hidden character(s) across source files "+
			"-- run 'apm-go audit' to inspect before publishing\n", total)
	}
}

// writeBundleFiles copies every file_map entry's source into bundleDir at
// its output-relative path, skipping symlinked sources and any path that
// would escape bundleDir (defense in depth -- CollectXxx already produces
// only valid relative paths, and MergeFileMap's validOutputRel already
// rejected traversal/absolute paths, but the write loop re-checks
// containment per file, mirroring plugin_exporter.py:600-611).
func writeBundleFiles(bundleDir string, fileMap *FileMap) error {
	for _, key := range fileMap.Keys() {
		src, _ := fileMap.Source(key)
		info, err := os.Lstat(src)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		absDest, err := build.EnsureWithinRoot(bundleDir, key)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(absDest), 0o755); err != nil {
			return err
		}
		if err := copyFile(src, absDest); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dest string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read %s: %w", src, err)
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(src); err == nil {
		mode = info.Mode()
	}
	if err := os.WriteFile(dest, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", dest, err)
	}
	return nil
}

// pluginJSONCandidates mirrors find_plugin_json's search order
// (utils/helpers.py:105-129): project-root plugin.json first, then each
// known ecosystem's convention path.
var pluginJSONCandidates = []string{
	"plugin.json",
	filepath.Join(".github", "plugin", "plugin.json"),
	filepath.Join(".claude-plugin", "plugin.json"),
	filepath.Join(".cursor-plugin", "plugin.json"),
}

// findOrSynthesizePluginJSON locates an existing plugin.json or synthesizes
// one from apmYMLNode, mirroring _find_or_synthesize_plugin_json
// (plugin_exporter.py:336-352) -> find_or_synthesize_plugin_json
// (core/plugin_manifest.py:286-330). A found-but-unparsable plugin.json
// warns and falls back to synthesis rather than erroring.
func findOrSynthesizePluginJSON(w io.Writer, projectRoot string, apmYMLNode *yaml.Node, suppressMissingInfo bool) (JSONValue, error) {
	for _, rel := range pluginJSONCandidates {
		p := filepath.Join(projectRoot, rel)
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		v, perr := DecodeJSONValue(data)
		if perr != nil {
			fmt.Fprintf(w, "[warn] Found plugin.json at %s but could not parse it: %v. Falling back to synthesis from apm.yml.\n", p, perr)
			break
		}
		return v, nil
	}
	if !suppressMissingInfo {
		fmt.Fprintln(w, "[i] No plugin.json found; synthesising from apm.yml.")
	}
	m, err := Synthesize(apmYMLNode)
	if err != nil {
		return JSONValue{}, err
	}
	return m.ToJSONValue(), nil
}

var schemaInvalidPluginJSONKeys = map[string]bool{
	"agents": true, "skills": true, "commands": true, "instructions": true,
}

// stripSchemaInvalidKeys mirrors _update_plugin_json_paths
// (plugin_exporter.py:365-397): the convention directories are
// auto-discovered by plugin hosts, so an authored plugin.json's
// agents/skills/commands/instructions keys (which point at files OUTSIDE
// those directories per the official schema) are stripped, with a warning
// naming every key removed.
func stripSchemaInvalidKeys(w io.Writer, v JSONValue) JSONValue {
	if v.Kind != KindObject {
		return v
	}
	var stripped []string
	kept := JSONValue{Kind: KindObject}
	for _, f := range v.O {
		if schemaInvalidPluginJSONKeys[f.Key] {
			stripped = append(stripped, f.Key)
			continue
		}
		kept.O = append(kept.O, f)
	}
	if len(stripped) > 0 {
		fmt.Fprintf(w, "[warn] Stripped schema-invalid keys from authored plugin.json: %s "+
			"-- convention directories are auto-discovered by Claude Code\n", strings.Join(stripped, ", "))
	}
	return kept
}

var safeBundleNameRe = regexp.MustCompile(`[^a-zA-Z0-9._-]`)

// sanitizeBundleName mirrors _sanitize_bundle_name (plugin_exporter.py:
// 52-61): replace path separators/traversal characters with hyphens, strip
// leading/trailing hyphens, then double-check the result is a single safe
// path component (defense in depth against the regex missing an edge
// case).
func sanitizeBundleName(name string) string {
	sanitized := strings.Trim(safeBundleNameRe.ReplaceAllString(name, "-"), "-")
	if sanitized == "" {
		return "unnamed"
	}
	if strings.Contains(sanitized, "..") || strings.Contains(sanitized, "/") || strings.Contains(sanitized, "\\") {
		return "unnamed"
	}
	return sanitized
}

// embedPackLockfile computes each bundle file's bare-hex sha256 (excluding
// apm.lock.yaml itself) and writes apm.lock.yaml with an embedded pack:
// section, mirroring export_plugin_bundle step 14b (plugin_exporter.py:
// 632-660).
func embedPackLockfile(bundleDir string, lf *lockfile.Lockfile, original *yaml.Node, target string) error {
	bundleFiles := map[string]string{}
	walkErr := filepath.WalkDir(bundleDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		rel, rerr := filepath.Rel(bundleDir, p)
		if rerr != nil {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if relSlash == "apm.lock.yaml" {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		sum := sha256.Sum256(data)
		bundleFiles[relSlash] = hex.EncodeToString(sum[:])
		return nil
	})
	if walkErr != nil {
		return walkErr
	}

	effectiveTarget := target
	if effectiveTarget == "" {
		effectiveTarget = "all"
	}
	meta := NewPackMetadata("plugin", effectiveTarget, bundleFiles)
	enriched, err := EnrichLockfileForPack(lf, meta, original)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(bundleDir, "apm.lock.yaml"), enriched, 0o644)
}
