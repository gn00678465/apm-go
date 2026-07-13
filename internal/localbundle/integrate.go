package localbundle

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
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

// IntegrateLocalBundle deploys bundleDir's plugin-native content (agents/,
// skills/, commands/, instructions/, hooks.json, .mcp.json) into every
// resolved target, mirroring integrate_local_bundle
// (install/services.py:702+) restricted to what apm-go's deploy pipeline
// actually supports (no canvas/"extensions" primitive type exists in
// apm-go's deploy package -- a pre-existing gap this task does not add).
//
// This is a deliberately independent code path from deploy.Run: a local
// bundle has no apm.yml, no lockfile-resolved dependency graph, and no
// per-dependency provenance to thread through the resolver -- it is an
// imperative deploy of already-merged, already-conflict-resolved bundle
// content (BundleProducer already ran file_map/hooks/mcp merge at pack
// time), so there is nothing left here to run ResolvePrimitives over. It
// DOES reuse deploy.Adapters' per-target DeployPrimitive/WriteMCP dispatch,
// since that is exactly the per-target path convention/transformation logic
// a plugin-native file needs, regardless of which code path constructed its
// Primitive.
//
// Returns an empty, non-nil result (no error) when targets is empty --
// mirroring Python's "no active targets resolved -- nothing will be
// deployed" warn-and-return (not a failure): the caller
// (cmd/apm/install.go) is responsible for deciding whether/how to warn
// before ever calling this function with an empty targets slice.
func IntegrateLocalBundle(bundleDir string, targets []string, projectDir string) (*IntegrateResult, error) {
	result := &IntegrateResult{Hashes: map[string]string{}}
	if len(targets) == 0 {
		return result, nil
	}

	prims := collectBundlePrimitives(bundleDir)
	mcpPrims, mcpDiags := collectBundleMCPPrimitives(bundleDir)
	result.Diags = append(result.Diags, mcpDiags...)

	deployedSkills := make(map[string]bool)
	writtenBy := make(map[string]string)

	for _, target := range targets {
		adapter, ok := deploy.Adapters[target]
		if !ok {
			continue
		}
		for _, p := range prims {
			if !adapterSupportsType(adapter, p.Type) {
				continue
			}
			files, err := adapter.DeployPrimitive(p, projectDir)
			if err != nil {
				result.Diags = append(result.Diags, fmt.Sprintf("deploy %s to %s failed: %v", p.Name, target, err))
				continue
			}

			// Deduplicate skill file writes across targets (mirrors
			// deploy.Run): most targets converge on the same canonical
			// .agents/skills/<name>/... path, so only count/hash each
			// distinct path once.
			if p.Type == deploy.TypeSkills {
				var deduped []string
				for _, f := range files {
					if deployedSkills[f] {
						continue
					}
					deployedSkills[f] = true
					deduped = append(deduped, f)
				}
				files = deduped
			}

			for _, f := range files {
				if prev, ok := writtenBy[f]; ok && prev != p.Name {
					result.Diags = append(result.Diags, fmt.Sprintf(
						"%s %q overwrites %s already written by %q (single-file target)",
						p.Type, p.Name, f, prev))
				}
				writtenBy[f] = p.Name
				result.Files = append(result.Files, f)
				hash, herr := lockfile.HashFileBytes(filepath.Join(projectDir, f))
				if herr != nil {
					return nil, fmt.Errorf("hash deployed file %s: %w", f, herr)
				}
				result.Hashes[f] = hash
			}
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

func adapterSupportsType(adapter deploy.TargetAdapter, t deploy.PrimitiveType) bool {
	for _, st := range adapter.SupportedTypes() {
		if st == t {
			return true
		}
	}
	return false
}

// collectBundlePrimitives reads bundleDir's top-level plugin-native
// convention directories (agents/, commands/, instructions/, skills/) plus
// hooks.json, mirroring the SHAPE _collect_apm_components/
// _collect_root_plugin_components produce (findings §3.2) -- BundleProducer
// already flattened/merged everything at pack time, so this only needs to
// re-derive deploy.Primitive from the bundle's OWN directory layout, not
// re-run any collection/merge logic.
//
// agents/commands/instructions are read NON-recursively (direct children
// only): this matches internal/deploy/primitive.go's own
// collectFromAPMDir, which is flat for every .apm/ subdirectory except
// skills/ -- apm-go's deploy pipeline has no concept of a nested command/
// instruction namespace to deploy INTO, regardless of whether
// BundleProducer's own collect.go preserved a source subdirectory
// hierarchy when PACKING (findings §3.2's collectRecursive) -- a
// pre-existing apm-go limitation, not something this task's install-side
// consumption can paper over.
func collectBundlePrimitives(bundleDir string) []deploy.Primitive {
	var prims []deploy.Primitive
	prims = append(prims, collectFlatPrimitives(filepath.Join(bundleDir, "agents"), deploy.TypeAgents, extractAgentName)...)
	prims = append(prims, collectFlatPrimitives(filepath.Join(bundleDir, "commands"), deploy.TypeCommands, extractCommandName)...)
	prims = append(prims, collectFlatPrimitives(filepath.Join(bundleDir, "instructions"), deploy.TypeInstructions, extractInstructionName)...)
	prims = append(prims, collectSkillPrimitives(filepath.Join(bundleDir, "skills"))...)

	if hooksPath := filepath.Join(bundleDir, "hooks.json"); isRegularFile(hooksPath) {
		prims = append(prims, deploy.Primitive{Name: "hooks", Type: deploy.TypeHooks, Source: "local", SrcPath: hooksPath})
	}
	return prims
}

func collectFlatPrimitives(dir string, t deploy.PrimitiveType, nameFn func(string) string) []deploy.Primitive {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []deploy.Primitive
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := nameFn(e.Name())
		if name == "" {
			continue
		}
		out = append(out, deploy.Primitive{
			Name:    name,
			Type:    t,
			Source:  "local",
			SrcPath: filepath.Join(dir, e.Name()),
		})
	}
	return out
}

func collectSkillPrimitives(skillsDir string) []deploy.Primitive {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil
	}
	var out []deploy.Primitive
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if !isRegularFile(filepath.Join(skillsDir, e.Name(), "SKILL.md")) {
			continue
		}
		out = append(out, deploy.Primitive{
			Name:    e.Name(),
			Type:    deploy.TypeSkills,
			Source:  "local",
			SrcPath: filepath.Join(skillsDir, e.Name()),
		})
	}
	return out
}

// extractAgentName/extractCommandName/extractInstructionName mirror
// internal/deploy/primitive.go's identically-purposed unexported helpers
// (extractAgentName/extractBaseName/extractInstructionName) -- duplicated
// here rather than exported from internal/deploy, since this task's
// Rollback Points restrict internal/deploy/primitive.go to an untouched
// file (design.md's five-file edit boundary: pack.go/audit.go/install.go/
// manifest.go/target.go).
func extractAgentName(filename string) string {
	if strings.HasSuffix(filename, ".agent.md") {
		return strings.TrimSuffix(filename, ".agent.md")
	}
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

func extractCommandName(filename string) string {
	if strings.HasSuffix(filename, ".md") {
		return strings.TrimSuffix(filename, ".md")
	}
	return ""
}

func extractInstructionName(filename string) string {
	if strings.HasSuffix(filename, ".instructions.md") {
		return strings.TrimSuffix(filename, ".instructions.md")
	}
	return ""
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
