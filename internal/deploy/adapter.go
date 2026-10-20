package deploy

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
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
	adopted    map[string]bool
	// unmanaged is the project deploy's guard of the files it does not
	// manage, nil for a --global deploy and when Run is given no Managed.
	unmanaged *unmanagedGuard
	// hookPackage is the package a copilot hook of the project's own
	// content is named after (projectHookPackage).
	hookPackage string
	// links is the link rewriting of the Run, nil outside one.
	links *linkRun
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
//  3. auto-detection from filesystem signals (manifest.SignalWhitelist)
//
// A target named twice is kept at its first place. Returns empty if nothing
// detected (no-deploy).
func ResolveTargets(flagTarget string, manifestTargets []string, projectDir string) ([]string, []string) {
	if targets, diags, named := namedTargets(flagTarget, manifestTargets); named {
		return targets, diags
	}
	return filterExplicitOnly(manifest.DetectTargets(projectDir)), nil
}

// ResolveInstallTargets is ResolveTargets for install and update, which
// detect by the Oracle's signal table (manifest.HarnessSignals) and refuse
// more than one detected harness, as its resolve_targets does
// (core/target_detection.py:1137-1146): ambiguous then names them, sorted,
// and targets is empty. One detected harness with no adapter is no target.
func ResolveInstallTargets(flagTarget string, manifestTargets []string, projectDir string) (targets, diags, ambiguous []string) {
	if targets, diags, named := namedTargets(flagTarget, manifestTargets); named {
		return targets, diags, nil
	}
	detected := manifest.DetectHarnesses(projectDir)
	if len(detected) > 1 {
		return nil, nil, detected
	}
	return filterSupported(filterExplicitOnly(detected)), nil, nil
}

// UserScopeTargets is ResolveTargets for a --global deploy, the Oracle's
// active_targets_user_scope (integration/targets.py:1204-1280): with no
// --target and no targets: in ~/.apm/apm.yml, each target whose user-scope
// root is a directory below home (userScopeRoots), else copilot, which is
// then fallback.
func UserScopeTargets(flagTarget string, manifestTargets []string, home string) (targets, diags []string, fallback bool) {
	if targets, diags, named := namedTargets(flagTarget, manifestTargets); named {
		return targets, diags, false
	}
	for _, r := range userScopeRoots {
		if info, err := os.Stat(filepath.Join(home, filepath.FromSlash(r.dir))); err == nil && info.IsDir() {
			targets = append(targets, r.target)
		}
	}
	if len(targets) == 0 {
		return []string{"copilot"}, nil, true
	}
	return targets, nil, false
}

// userScopeRoots are the user-scope roots by which the Oracle detects the
// targets apm-go has an adapter for, sorted by target: effective_root(
// user_scope=True) of each profile with detect_by_dir (integration/
// targets.py:538-700). antigravity and agent-skills are not detected.
var userScopeRoots = []struct{ target, dir string }{
	{"claude", ".claude"},
	{"codex", ".codex"},
	{"copilot", ".copilot"},
	{"opencode", ".config/opencode"},
}

// namedTargets is the targets --target or apm.yml's targets: names, all
// expanded, each once at its first place; named is false when neither names
// any.
func namedTargets(flagTarget string, manifestTargets []string) (targets, diags []string, named bool) {
	tokens := manifestTargets
	if flagTarget != "" {
		var err error
		if tokens, err = SplitTargetFlag(flagTarget); err != nil {
			return nil, []string{err.Error()}, true
		}
	} else if len(manifestTargets) == 0 {
		return nil, nil, false
	}
	for _, t := range tokens {
		expanded := []string{t}
		if t == "all" {
			expanded = allAutoDetectableTargets()
		}
		for _, e := range expanded {
			if !slices.Contains(targets, e) {
				targets = append(targets, e)
			}
		}
	}
	return filterSupported(targets), checkUnsupported(targets), true
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
		return symlinkSkillTo(p, absDestDir, destDir)
	}

	links, filter := skillLinksOf(p), skillCopyRulesOf(p)
	if g := activeDeployMode.unmanaged; g != nil {
		var rels []string
		_ = walkSkillDir(p.SrcPath, links, filter, func(_, rel string, isDir bool) error {
			if !isDir {
				rels = append(rels, rel)
			}
			return nil
		})
		switch g.skill(p.DepKey, destDir, rels, func(abs string) bool { return skillSameOnDisk(p, destDir, abs) }) {
		case placeSkip:
			return nil, nil
		case placeAdopt:
			deployed := make([]string, len(rels))
			for i, rel := range rels {
				deployed[i] = path.Join(destDir, rel)
			}
			return deployed, nil
		}
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
	info, err := os.Lstat(absDestDir)
	if err == nil && info.Mode()&os.ModeSymlink != 0 {
		if err := os.Remove(absDestDir); err != nil {
			return nil, fmt.Errorf("replace skill destination: %w", err)
		}
	} else {
		for _, dirs := range []bool{false, true} {
			err := walkSkillDir(p.SrcPath, links, filter, func(_, rel string, isDir bool) error {
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
	err = walkSkillDir(p.SrcPath, links, filter, func(src, rel string, isDir bool) error {
		dst := filepath.Join(absDestDir, filepath.FromSlash(rel))
		if isDir {
			return os.MkdirAll(dst, 0755)
		}
		if info, err := os.Lstat(dst); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(dst); err != nil {
				return err
			}
		}
		data, err := skillFileBytes(p, destDir, rel, src)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
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

// symlinkSkillTo makes absDestDir a symlink to p's source. A real directory at
// absDestDir is the user's (the deploy only ever makes a symlink there), so it
// is deleted only when the source has every file in it with the same bytes;
// otherwise it is left as it is and the error is a *keptSkillDirError. The
// files it returns are those a copy deploy writes (walkSkillDir), so one lock
// serves both; the link still shows what the copy leaves out (#113, issue
// #126 comment 6267289737 item 1).
func symlinkSkillTo(p Primitive, absDestDir, destDir string) ([]string, error) {
	absSrc, err := filepath.Abs(p.SrcPath)
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
	_ = walkSkillDir(p.SrcPath, skillLinksOf(p), skillCopyRulesOf(p), func(_, rel string, isDir bool) error {
		if !isDir {
			deployed = append(deployed, path.Join(destDir, rel))
		}
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

// walkSkillTree calls visit for each entry below srcDir, a directory before
// what is in it, with its slash-separated path below relPrefix, and a
// symlink as links says. With skipRootGit the entry .git of srcDir itself is
// not visited (Primitive.SkipRootGit); one deeper down is. When visit returns
// fs.SkipDir the entry is passed over, with what is in it.
func walkSkillTree(srcDir, relPrefix string, links skillLinks, skipRootGit bool, visit func(src, rel string, isDir bool) error) error {
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
		switch err := visit(src, rel, isDir); {
		case err == fs.SkipDir:
			continue
		case err != nil:
			return err
		}
		if isDir {
			if err := walkSkillTree(src, rel, links, false, visit); err != nil {
				return err
			}
		}
	}
	return nil
}

func deployFileToPath(p Primitive, destPath, projectDir string) ([]string, error) {
	if err := checkFileDest(destPath, projectDir); err != nil {
		return nil, err
	}
	absDest := filepath.Join(projectDir, filepath.FromSlash(destPath))
	switch placeFile(p, destPath) {
	case placeSkip:
		return nil, nil
	case placeAdopt:
		return []string{destPath}, nil
	}
	if copiedAsText(p.Type) {
		if src, err := os.ReadFile(p.SrcPath); err == nil {
			noteAdopted(destPath, absDest, comparedContent(p, destPath, src), true)
		}
	}
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
	if err := copyPrimitiveFile(p, destPath, absDest); err != nil {
		return nil, fmt.Errorf("deploy %s %s: %w", p.Type, p.Name, err)
	}
	return []string{destPath}, nil
}

// noteAdopted records, before the deploy writes destPath, whether the file
// there is the one the Oracle compares with want and adopts, as
// oracleFinds reads it (integration/base_integrator.py:429-495).
func noteAdopted(destPath, abs string, want []byte, text bool) {
	if activeDeployMode.adopted == nil {
		return
	}
	got, ok := oracleFinds(abs, text)
	activeDeployMode.adopted[destPath] = ok && bytes.Equal(got, want)
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

// UserScopeMCP reports whether target takes MCP servers at user scope: the
// Oracle refuses opencode and agent-skills there (commands/install.py:
// 769-805, integration/mcp_integrator_install.py:585-605).
func UserScopeMCP(target string) bool {
	return target != "opencode" && target != "agent-skills"
}
