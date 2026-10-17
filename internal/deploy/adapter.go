package deploy

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/manifest"
)

// activeDeployMode is set by Run() for the duration of a deploy pipeline.
// symlink=true makes per-primitive deploys create symlinks to apm_modules
// sources instead of copying. global is a --global deploy, by symlink or, on
// a Windows account that cannot make one, by copy. projectDir is the
// directory the sources are read from.
var activeDeployMode struct {
	symlink    bool
	global     bool
	projectDir string
}

// symlinkProbe is canSymlink, replaced in tests.
var symlinkProbe = canSymlink

type TargetAdapter interface {
	Name() string
	DeployRoots() []string
	SupportedTypes() []PrimitiveType
	DeployPrimitive(p Primitive, projectDir string) ([]string, error)
}

// MCPTarget is implemented by adapters that can write a merged MCP config
// file. Unlike DeployPrimitive (one file-copy per primitive), WriteMCP is
// called once per target with every TypeMCP winner, because N MCP servers
// merge into a single config file. written names the servers that actually
// landed in files (a server can be dropped from prims for this target
// without erroring, e.g. refused or a non-https/unsupported transport), so
// the caller can build accurate per-server source provenance (pr-001).
type MCPTarget interface {
	MCPResolveMode() manifest.ResolveMode
	WriteMCP(prims []Primitive, projectDir string) (files []string, written []string, diags []string, err error)
}

// BundleTarget is implemented by adapters that group a dependency's
// primitives into a per-package bundle directory (e.g. antigravity's
// .agents/plugins/<pkg>/) instead of writing them at fixed, shared paths.
//
// ValidateBundleNames is called once per target, for every DepKey that will
// receive at least one primitive under this target, BEFORE any primitive is
// deployed to ANY target this Run. It must fail closed (return a non-nil
// error, with nothing yet written) if two different DepKeys would collide on
// the same bundle directory, rather than let their files silently mix.
//
// FinalizeBundles is called once per target, AFTER every primitive has been
// deployed, with the DepKeys that actually produced at least one file. It
// writes any per-bundle manifest (e.g. plugin.json) and returns the
// manifest's relative path(s) keyed by DepKey, mirroring MCPTarget.WriteMCP's
// once-per-target shape. It must be idempotent: every Run() call rewrites
// and re-reports the manifest, so a re-install never drops it from
// deployed_files/deployed_file_hashes provenance.
type BundleTarget interface {
	ValidateBundleNames(depKeys []string) error
	FinalizeBundles(depKeys []string, projectDir string) (map[string][]string, error)
}

// promptCommander is a target that deploys a prompt as a command, converted
// the way the Oracle's CommandIntegrator converts it, to commandPath.
type promptCommander interface {
	commandPath(name string) string
}

// instructionChecker is a target that deploys instructions: checkInstruction
// is the check its deploy of p makes before it writes. Run calls it for every
// instruction before the first write, because a refused instruction fails the
// whole install, as the Oracle's instruction integrator raises
// (integration/instruction_integrator.py:237,440).
type instructionChecker interface {
	checkInstruction(p Primitive, deployDir string) error
}

var Adapters = map[string]TargetAdapter{
	"claude":       &claudeAdapter{},
	"codex":        &codexAdapter{},
	"copilot":      &copilotAdapter{},
	"antigravity":  &antigravityAdapter{},
	"opencode":     &opencodeAdapter{},
	"agent-skills": &agentSkillsAdapter{},
}

// SplitTargetFlag splits a --target/-t CLI flag value on commas (trimming
// whitespace, dropping empty segments) and validates each resulting token
// against the canonical target vocabulary via manifest.ValidateTarget --
// req-mf-005. That is the SAME validator apm.yml's target: field already
// uses, so the CLI flag and the manifest field reject exactly the same set
// of unknown tokens (canonical names including "all", known aliases, and
// x-<vendor>-<name> extension tokens are all accepted). A genuinely unknown
// token is rejected, naming it.
//
// Known-but-adapterless canonical targets (cursor/gemini/windsurf) pass
// validation here -- ResolveTargets's checkUnsupported separately reports
// the non-fatal "no registered handler" diagnostic for those (req-tg-004);
// this function must never turn that into a hard error.
func SplitTargetFlag(flagTarget string) ([]string, error) {
	var tokens []string
	for _, part := range strings.Split(flagTarget, ",") {
		t := strings.TrimSpace(part)
		if t == "" {
			continue
		}
		normalized, err := manifest.ValidateTarget(t)
		if err != nil {
			return nil, fmt.Errorf("--target %q: %w", t, err)
		}
		tokens = append(tokens, normalized)
	}
	return tokens, nil
}

// ResolveTargets determines active targets by priority:
//  1. --target flag (explicit CLI, comma-separated, validated against the
//     canonical target vocabulary -- see SplitTargetFlag)
//  2. manifest target: field
//  3. auto-detection from filesystem signals
//
// Returns empty if nothing detected (no-deploy).
func ResolveTargets(flagTarget string, manifestTargets []string, projectDir string) ([]string, []string) {
	var diags []string

	if flagTarget != "" {
		tokens, err := SplitTargetFlag(flagTarget)
		if err != nil {
			return nil, []string{err.Error()}
		}
		var targets []string
		for _, t := range tokens {
			if t == "all" {
				targets = append(targets, allAutoDetectableTargets()...)
			} else {
				targets = append(targets, t)
			}
		}
		diags = append(diags, checkUnsupported(targets)...)
		return filterSupported(targets), diags
	}

	if len(manifestTargets) > 0 {
		var targets []string
		for _, t := range manifestTargets {
			if t == "all" {
				targets = append(targets, allAutoDetectableTargets()...)
			} else {
				targets = append(targets, t)
			}
		}
		diags = append(diags, checkUnsupported(targets)...)
		return filterSupported(targets), diags
	}

	detected := manifest.DetectTargets(projectDir)
	detected = filterExplicitOnly(detected)
	if len(detected) > 0 {
		return detected, nil
	}

	return nil, nil
}

// explicitOnlyTargets must never be activated by auto-detection
// (req-tg-001), nor be included in the "all" expansion. agent-skills is the
// target the spec designates explicit-only. antigravity is explicit-only per
// the user decision of 2026-07-05, aligning with Python apm_cli's
// EXPLICIT_ONLY_TARGETS={"agent-skills","antigravity"}: its former detection
// signals (GEMINI.md/AGENTS.md) are cross-tool files also read by
// opencode/agent-skills tooling, so their presence must not auto-enable
// antigravity. Select it via --target antigravity (alias agy) or apm.yml
// target:. (This supersedes acceptance-checklist.md's earlier research note
// that had antigravity auto-detecting.)
var explicitOnlyTargets = map[string]bool{
	"agent-skills": true,
	"antigravity":  true,
}

func allAutoDetectableTargets() []string {
	return []string{"claude", "codex", "copilot", "opencode"}
}

// filterExplicitOnly removes targets that require explicit --target selection.
func filterExplicitOnly(targets []string) []string {
	var result []string
	for _, t := range targets {
		if !explicitOnlyTargets[t] {
			result = append(result, t)
		}
	}
	return result
}

func checkUnsupported(targets []string) []string {
	var diags []string
	for _, t := range targets {
		if _, ok := Adapters[t]; !ok {
			diags = append(diags, fmt.Sprintf("no registered handler for target %q", t))
		}
	}
	return diags
}

func filterSupported(targets []string) []string {
	var result []string
	for _, t := range targets {
		if _, ok := Adapters[t]; ok {
			result = append(result, t)
		}
	}
	return result
}

// sharedSkillRoot is the canonical skills directory several targets copy a
// skill into (req-tg-003), so two targets can write the same skill directory.
const sharedSkillRoot = ".agents/skills"

// skillRooter is implemented by every adapter: skillRoot is the directory its
// DeployPrimitive copies skill p into, as <root>/<name>.
type skillRooter interface {
	skillRoot(p Primitive) string
}

// deploySkillTo recursively copies a skill directory to <root>/<name>/.
// Adapters with a target-native skill root deploy there instead of the
// canonical path: claude deploys ONLY to
// .claude/skills/ (Claude Code does not discover skills from the cross-tool
// .agents/skills/ canonical path -- see claude.go), and antigravity routes
// dependency skills into per-package bundle directories.
func deploySkillTo(p Primitive, projectDir, root string) ([]string, error) {
	destDir := path.Join(root, p.Name)
	absDestDir := filepath.Join(projectDir, filepath.FromSlash(destDir))
	if err := CheckDeployPath(projectDir, destDir); err != nil {
		return nil, err
	}

	if activeDeployMode.symlink {
		return symlinkSkillTo(p.SrcPath, absDestDir, destDir, p.DepKey != "", p.SkipRootGit)
	}

	// A symlink here is not the deploy's: a copy through it would write into
	// the directory it leads to. It is replaced by the skill's own directory,
	// as a symlink deploy replaces one (symlinkSkillTo), so nothing below it
	// is written through it. In an existing directory every file, then every
	// directory (an empty one has no file to catch it), is checked before the
	// first write: a directory the user linked elsewhere would take a copy, or
	// an os.MkdirAll, there, so it has to stay in the skill's directory. A
	// file that is a symlink is replaced by the copy, wherever it points, as
	// the Oracle replaces the whole directory.
	links := skillLinksOf(p)
	info, err := os.Lstat(absDestDir)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(absDestDir); err != nil {
			return nil, fmt.Errorf("replace skill destination: %w", err)
		}
	} else {
		for _, dirs := range []bool{false, true} {
			err := walkSkillDir(p.SrcPath, "", links, p.SkipRootGit, func(_, rel string, isDir bool) error {
				if isDir != dirs {
					return nil
				}
				if !isDir {
					return CheckDeployPath(projectDir, path.Join(destDir, rel))
				}
				return CheckDeployWrite(projectDir, path.Join(destDir, rel))
			})
			if err != nil {
				return nil, err
			}
		}
	}
	if err := os.MkdirAll(absDestDir, 0755); err != nil {
		return nil, fmt.Errorf("create skill dir: %w", err)
	}
	var deployed []string
	err = walkSkillDir(p.SrcPath, "", links, p.SkipRootGit, func(src, rel string, isDir bool) error {
		dst := filepath.Join(absDestDir, filepath.FromSlash(rel))
		if isDir {
			return os.MkdirAll(dst, 0755)
		}
		if info, err := os.Lstat(dst); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(dst); err != nil {
				return err
			}
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
		deployed = append(deployed, path.Join(destDir, rel))
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("deploy skill %s: %w", p.Name, err)
	}
	return deployed, nil
}

// symlinkSkillTo makes absDestDir a symlink to srcDir. A real directory at
// absDestDir is the user's (the deploy only ever makes a symlink there), so it
// is deleted only when the source has every file in it with the same bytes;
// otherwise it is left as it is and the error is a *keptSkillDirError. The
// files it returns are those a copy deploy writes (follow and skipRootGit as
// walkSkillDir); the link still shows a skipped .git (#113).
func symlinkSkillTo(srcDir, absDestDir, destDir string, follow, skipRootGit bool) ([]string, error) {
	absSrc, err := filepath.Abs(srcDir)
	if err != nil {
		return nil, fmt.Errorf("resolve source: %w", err)
	}
	info, err := os.Lstat(absDestDir)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return nil, fmt.Errorf("inspect skill destination: %w", err)
	case info.IsDir():
		if mismatch, ok := firstEntryNotInSource(absDestDir, absSrc); !ok {
			return nil, &keptSkillDirError{dir: destDir, mismatch: mismatch}
		}
	}
	if err := os.RemoveAll(absDestDir); err != nil {
		return nil, fmt.Errorf("replace skill destination: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absDestDir), 0755); err != nil {
		return nil, fmt.Errorf("create parent dir: %w", err)
	}
	if err := os.Symlink(absSrc, absDestDir); err != nil {
		return nil, fmt.Errorf("symlink skill: %w", err)
	}
	var deployed []string
	if follow {
		_ = walkSkillDir(srcDir, "", followLinks, skipRootGit, func(_, rel string, isDir bool) error {
			if !isDir {
				deployed = append(deployed, path.Join(destDir, rel))
			}
			return nil
		})
		return deployed, nil
	}
	_ = filepath.WalkDir(srcDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(srcDir, p)
		deployed = append(deployed, path.Join(destDir, filepath.ToSlash(rel)))
		return nil
	})
	return deployed, nil
}

// skillLinks is what walkSkillDir does with a symlink in a skill.
type skillLinks int

const (
	// skipLinks skips it: a copy through it would read what it points at,
	// e.g. /etc/passwd from a malicious package.
	skipLinks skillLinks = iota
	// followLinks visits it as what it points at. That is only for a
	// dependency skill, whose tree collection has checked (archive.CheckTree),
	// so every symlink in it stays in the package and none loops.
	followLinks
	// fileLinks visits a symlink to a regular file as that file and skips any
	// other. That is for the project's own skill in a --global copy deploy,
	// which has to record the paths and hashes a symlink deploy records
	// through its link to the skill directory, so one lock serves both.
	fileLinks
)

// skillLinksOf is how a copy deploy walks the skill p.
func skillLinksOf(p Primitive) skillLinks {
	switch {
	case p.DepKey != "":
		return followLinks
	case activeDeployMode.global:
		return fileLinks
	}
	return skipLinks
}

// walkSkillDir calls visit for each entry below srcDir, a directory before
// what is in it, with its slash-separated path below relPrefix, and a
// symlink as links says. With skipRootGit the entry .git of srcDir itself is
// not visited (Primitive.SkipRootGit); one deeper down is.
func walkSkillDir(srcDir, relPrefix string, links skillLinks, skipRootGit bool, visit func(src, rel string, isDir bool) error) error {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if skipRootGit && e.Name() == ".git" {
			continue
		}
		src, rel, isDir := filepath.Join(srcDir, e.Name()), path.Join(relPrefix, e.Name()), e.IsDir()
		if e.Type()&os.ModeSymlink != 0 {
			switch links {
			case skipLinks:
				continue
			case fileLinks:
				if info, err := os.Stat(src); err != nil || !info.Mode().IsRegular() {
					continue
				}
			case followLinks:
				info, err := os.Stat(src)
				if err != nil {
					return err
				}
				isDir = info.IsDir()
			}
		}
		if err := visit(src, rel, isDir); err != nil {
			return err
		}
		if isDir {
			if err := walkSkillDir(src, rel, links, false, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0644)
}

func deployFileToPath(p Primitive, destPath, projectDir string) ([]string, error) {
	if err := checkFileDest(destPath, projectDir); err != nil {
		return nil, err
	}
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	if err := os.MkdirAll(filepath.Dir(absDest), 0755); err != nil {
		return nil, err
	}
	if activeDeployMode.symlink {
		absSrc, err := filepath.Abs(p.SrcPath)
		if err != nil {
			return nil, fmt.Errorf("resolve source: %w", err)
		}
		os.Remove(absDest)
		if err := os.Symlink(absSrc, absDest); err != nil {
			return nil, fmt.Errorf("symlink %s %s: %w", p.Type, p.Name, err)
		}
		return []string{destPath}, nil
	}
	// os.WriteFile follows a symlink: through an earlier symlink deploy's
	// link the copy would overwrite the package source it points at.
	if activeDeployMode.global && isDeployLink(absDest) {
		if err := os.Remove(absDest); err != nil {
			return nil, fmt.Errorf("replace %s %s: %w", p.Type, p.Name, err)
		}
	}
	if err := copyPrimitiveFile(p, absDest); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}

// checkFileDest is the check deployFileToPath makes before it writes
// destPath. A copy is written through a symlink there, so it takes
// CheckDeployWrite. A --global deploy replaces a symlink there that leads into
// apm_modules/ or .apm/, a deploy's own whichever package it was deployed for,
// since the packages that have a primitive of this name change with their
// order in apm.yml; a symlink deploy replaces whatever is there (os.Remove,
// then os.Symlink). Both take CheckDeployPath. Any other symlink is the
// user's, refused and kept when it leads out of the primitive's directory, as
// a copy deploy refuses to write through it.
func checkFileDest(destPath, deployDir string) error {
	if !activeDeployMode.global {
		return CheckDeployWrite(deployDir, destPath)
	}
	if err := CheckDeployPath(deployDir, destPath); err != nil {
		return err
	}
	abs := filepath.Join(deployDir, filepath.FromSlash(destPath))
	if isDeployLink(abs) {
		return nil
	}
	if _, err := os.Readlink(abs); err != nil && activeDeployMode.symlink {
		return nil
	}
	return CheckDeployWrite(deployDir, destPath)
}

// isDeployLink reports whether abs is a symlink into apm_modules/ or .apm/ of
// the global project directory, dangling or not.
func isDeployLink(abs string) bool {
	target, err := os.Readlink(abs)
	if err != nil {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(abs), target)
	}
	for _, source := range []string{"apm_modules", ".apm"} {
		source = filepath.Join(activeDeployMode.projectDir, source)
		if archive.Contained(source, target) || containedReal(source, target) {
			return true
		}
	}
	return false
}
