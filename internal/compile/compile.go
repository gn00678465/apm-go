// Package compile implements apm-go's minimal agents-family subset of the
// Python oracle's `apm compile`: it reads local + dependency
// *.instructions.md primitives and compiles them into a single project-root
// AGENTS.md, for targets whose Python compile_family is "agents"
// (antigravity, codex, opencode). See
// .trellis/tasks/07-11-agents-md-compile/design.md for the full contract.
package compile

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/version"
	"github.com/apm-go/apm/internal/yamlcore"
)

// apmGoVersion is the "APM Version" compile writes into AGENTS.md, sourced
// from the single version const (internal/version) that also drives the root
// command's --version flag and install.go's lockfile apm_version field.
const apmGoVersion = version.Version

// agentsFamilyTargets is apm-go compile's v1 target vocabulary -- the three
// adapters that mirror Python's compile_family="agents" routing
// (design.md §1/§2; research/findings.md section C).
var agentsFamilyTargets = map[string]bool{
	"antigravity": true,
	"codex":       true,
	"opencode":    true,
}

// FilterAgentsFamily returns the subset of targets compile supports,
// preserving input order.
func FilterAgentsFamily(targets []string) []string {
	var out []string
	for _, t := range targets {
		if agentsFamilyTargets[t] {
			out = append(out, t)
		}
	}
	return out
}

// HasCompilableContent reports whether projectDir has anything for compile
// to read: an apm_modules/ directory, or at least one local
// .apm/instructions/*.instructions.md file (design.md §2 project gate,
// simplified from the oracle's broader constitution/chatmode checks --
// v1 compiles neither).
func HasCompilableContent(projectDir string) bool {
	if info, err := os.Stat(filepath.Join(projectDir, "apm_modules")); err == nil && info.IsDir() {
		return true
	}
	for _, p := range deploy.CollectLocalPrimitives(projectDir) {
		if p.Type == deploy.TypeInstructions {
			return true
		}
	}
	return false
}

// CollectInstructions gathers every *.instructions.md primitive in
// priority order -- local, then direct dependencies in manifest declaration
// order, then transitive dependencies in lockfile sorted order (design.md
// §3; mirrors deploy.Run's collection order, deploy.go:72-118) -- resolves
// same-name conflicts via deploy.ResolvePrimitives (local wins, then
// first-declared wins), drops any symlinked instruction file (defense in
// depth: deploy.CollectLocalPrimitives/CollectDependencyPrimitives do not
// themselves filter symlinks), and parses each winner's frontmatter.
func CollectInstructions(projectDir string, m *manifest.Manifest) ([]SourcedInstruction, error) {
	var ordered []deploy.Primitive
	ordered = append(ordered, deploy.CollectLocalPrimitives(projectDir)...)

	directDeps := make([]*manifest.DependencyReference, 0, len(m.ParsedDeps)+len(m.ParsedDevDeps))
	directDeps = append(directDeps, m.ParsedDeps...)
	directDeps = append(directDeps, m.ParsedDevDeps...)

	directKeys := make(map[string]bool)
	for _, dep := range directDeps {
		key := deploy.DepRefKey(dep)
		if key == "" || directKeys[key] {
			continue
		}
		directKeys[key] = true
		modulePath := filepath.Join(projectDir, "apm_modules", key)
		ordered = append(ordered, deploy.CollectDependencyPrimitives(key, modulePath)...)
	}

	for _, dep := range sortedTransitiveDeps(loadLockfileDeps(projectDir), directKeys) {
		key := dep.UniqueKey()
		modulePath := filepath.Join(projectDir, "apm_modules", key)
		ordered = append(ordered, deploy.CollectDependencyPrimitives(key, modulePath)...)
	}

	var instructionPrims []deploy.Primitive
	for _, p := range ordered {
		if p.Type == deploy.TypeInstructions {
			instructionPrims = append(instructionPrims, p)
		}
	}
	instructionPrims = filterSymlinks(instructionPrims)

	winners, _ := deploy.ResolvePrimitives(instructionPrims)

	result := make([]SourcedInstruction, 0, len(winners))
	for _, p := range winners {
		content, err := os.ReadFile(p.SrcPath)
		if err != nil {
			return nil, err
		}
		relPath := p.SrcPath
		if rel, err := filepath.Rel(projectDir, p.SrcPath); err == nil {
			relPath = rel
		}
		result = append(result, SourcedInstruction{
			RelPath:           filepath.ToSlash(relPath),
			ParsedInstruction: ParseInstruction(content),
		})
	}
	return result, nil
}

// filterSymlinks drops any primitive whose source file is a symlink (or is
// no longer statable), so a symlink can never make external-tree content
// reachable from the compiled output.
func filterSymlinks(prims []deploy.Primitive) []deploy.Primitive {
	var out []deploy.Primitive
	for _, p := range prims {
		info, err := os.Lstat(p.SrcPath)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		out = append(out, p)
	}
	return out
}

// loadLockfileDeps reads projectDir/apm.lock.yaml, if present, and returns
// its locked dependencies. Any read/parse failure yields no transitive
// deps rather than failing compile -- a missing or unreadable lockfile
// simply means no transitive dependencies are known yet.
func loadLockfileDeps(projectDir string) []lockfile.LockedDep {
	data, err := os.ReadFile(filepath.Join(projectDir, "apm.lock.yaml"))
	if err != nil {
		return nil
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		return nil
	}
	return lock.Dependencies
}

// sortedTransitiveDeps returns the locked dependencies that are NOT already
// direct, sorted by (RepoURL, VirtualPath) -- mirroring deploy.go's
// sortedTransitiveDeps, adapted from resolver.ResolvedDep to
// lockfile.LockedDep since compile reads an already-resolved lockfile
// rather than re-resolving over the network.
func sortedTransitiveDeps(deps []lockfile.LockedDep, directKeys map[string]bool) []lockfile.LockedDep {
	var transitive []lockfile.LockedDep
	for _, d := range deps {
		if !directKeys[d.UniqueKey()] {
			transitive = append(transitive, d)
		}
	}
	sort.Slice(transitive, func(i, j int) bool {
		if transitive[i].RepoURL != transitive[j].RepoURL {
			return transitive[i].RepoURL < transitive[j].RepoURL
		}
		return transitive[i].VirtualPath < transitive[j].VirtualPath
	})
	return transitive
}

// Result summarizes one compile Run for the CLI to report.
type Result struct {
	Wrote            bool
	InstructionCount int
	Path             string // "AGENTS.md", relative to projectDir
}

// Run collects instructions, renders AGENTS.md, stabilizes its Build ID,
// and writes it idempotently to projectDir/AGENTS.md (design.md §3/§6).
func Run(projectDir string, m *manifest.Manifest) (*Result, error) {
	instructions, err := CollectInstructions(projectDir, m)
	if err != nil {
		return nil, err
	}
	content := StabilizeBuildID(RenderAgentsMD(instructions, apmGoVersion))
	wrote, err := WriteAGENTSMD(projectDir, content)
	if err != nil {
		return nil, err
	}
	return &Result{Wrote: wrote, InstructionCount: len(instructions), Path: "AGENTS.md"}, nil
}
