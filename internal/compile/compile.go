// Package compile implements apm-go's minimal agents-family subset of the
// Python oracle's `apm compile`: it reads local + dependency
// *.instructions.md primitives and compiles them into a single project-root
// AGENTS.md, for targets whose Python compile_family is "agents"
// (antigravity, codex, opencode).
package compile

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"unicode/utf8"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

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
// first-declared wins), drops any symlinked instruction file of the
// project's own (deploy.CollectLocalPrimitives does not filter symlinks; a
// dependency's come resolved, or fail the collection when they leave the
// package), and reads and parses each one (readCandidate); one it cannot
// use takes no part in the conflicts and is returned apart when it is to be
// reported.
func CollectInstructions(projectDir string, m *manifest.Manifest) ([]SourcedInstruction, []SkippedInstruction, error) {
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
		prims, err := deploy.CollectDependencyPrimitives(key, filepath.Join(projectDir, "apm_modules", key))
		if err != nil {
			return nil, nil, err
		}
		ordered = append(ordered, prims...)
	}

	for _, dep := range sortedTransitiveDeps(loadLockfileDeps(projectDir), directKeys) {
		key := dep.UniqueKey()
		prims, err := deploy.CollectDependencyPrimitives(key, filepath.Join(projectDir, "apm_modules", key))
		if err != nil {
			return nil, nil, err
		}
		ordered = append(ordered, prims...)
	}

	var instructionPrims []deploy.Primitive
	for _, p := range ordered {
		if p.Type == deploy.TypeInstructions {
			instructionPrims = append(instructionPrims, p)
		}
	}
	instructionPrims = filterSymlinks(instructionPrims)

	// The Oracle parses each file before it resolves conflicts, so one it
	// cannot use leaves its name to the next instruction that has it.
	parsed := make(map[string]ParsedInstruction, len(instructionPrims))
	var readable []deploy.Primitive
	var skipped []SkippedInstruction
	for _, p := range instructionPrims {
		pi, ok, err := readCandidate(p)
		if err != nil {
			skipped = append(skipped, SkippedInstruction{RelPath: displayPath(projectDir, p.SrcPath), Err: err})
		}
		if !ok {
			continue
		}
		parsed[p.SrcPath] = pi
		readable = append(readable, p)
	}

	winners, _ := deploy.ResolvePrimitives(readable)

	result := make([]SourcedInstruction, 0, len(winners))
	for _, p := range winners {
		result = append(result, SourcedInstruction{
			RelPath:           displayPath(projectDir, p.SrcPath),
			ParsedInstruction: parsed[p.SrcPath],
		})
	}
	return result, skipped, nil
}

// SkippedInstruction is an instruction compile leaves out and reports.
type SkippedInstruction struct {
	RelPath string
	Err     error
}

// oracleProbeBytes is the chunk Python's text reader decodes first, which
// is all the Oracle's _is_readable reads of a dependency's file.
const oracleProbeBytes = 8192

// readCandidate reads an instruction the way the Oracle's discovery does
// (primitives/discovery.py:225-263, 385-430). ok is false for a file that
// is no candidate; err is then the warning to print, or nil when the Oracle
// drops the file without one: a dependency's file that _is_readable
// (:678-692) cannot open or decode.
func readCandidate(p deploy.Primitive) (pi ParsedInstruction, ok bool, err error) {
	dependency := p.DepKey != ""
	content, err := os.ReadFile(p.SrcPath)
	if err != nil {
		if dependency {
			return pi, false, nil
		}
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) {
			err = pathErr.Err
		}
		return pi, false, fmt.Errorf("cannot read: %w", err)
	}
	if at := firstInvalidUTF8(content); at >= 0 {
		if dependency && !probeDecodes(content) {
			return pi, false, nil
		}
		return pi, false, fmt.Errorf("not valid UTF-8 at byte %d", at)
	}
	pi, err = ParseInstruction(content)
	if err != nil {
		return pi, false, err
	}
	return pi, true, nil
}

// firstInvalidUTF8 is the offset of the first byte that is not valid
// UTF-8, or -1.
func firstInvalidUTF8(b []byte) int {
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return -1
}

// probeDecodes is the Oracle's _is_readable on content: the first chunk
// decodes, a sequence the chunk cuts short not yet being an error.
func probeDecodes(content []byte) bool {
	chunk := content[:min(len(content), oracleProbeBytes)]
	at := firstInvalidUTF8(chunk)
	return at < 0 || !utf8.FullRune(chunk[at:])
}

// displayPath is srcPath relative to projectDir, with forward slashes.
func displayPath(projectDir, srcPath string) string {
	if rel, err := filepath.Rel(projectDir, srcPath); err == nil {
		srcPath = rel
	}
	return filepath.ToSlash(srcPath)
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
	// Sources lists each compiled instruction's display-relative source
	// path, in the same winner order the AGENTS.md render used. Read-only
	// exposure of data CollectInstructions already computed (R12e: the
	// output-parity gap was "sources are known but never surfaced" -- no
	// compile decision, ordering, or rendering changes ride along).
	Sources []string
	// Skipped are the instructions left out for their frontmatter, for the
	// command to report.
	Skipped []SkippedInstruction
}

// sourcePaths projects the compiled instructions down to their
// display-relative source paths for Result.Sources.
func sourcePaths(instructions []SourcedInstruction) []string {
	paths := make([]string, 0, len(instructions))
	for _, ins := range instructions {
		paths = append(paths, ins.RelPath)
	}
	return paths
}

// Run collects instructions, renders AGENTS.md, stabilizes its Build ID,
// and writes it idempotently to projectDir/AGENTS.md (design.md §3/§6).
func Run(projectDir string, m *manifest.Manifest) (*Result, error) {
	instructions, skipped, err := CollectInstructions(projectDir, m)
	if err != nil {
		return nil, err
	}
	content := StabilizeBuildID(RenderAgentsMD(instructions))
	wrote, err := WriteAGENTSMD(projectDir, content)
	if err != nil {
		return nil, err
	}
	return &Result{Wrote: wrote, InstructionCount: len(instructions), Path: "AGENTS.md", Sources: sourcePaths(instructions), Skipped: skipped}, nil
}
