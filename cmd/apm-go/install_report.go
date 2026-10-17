package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/compile"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/rootfs"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/version"
	"github.com/apm-go/apm/internal/yamlcore"
	yamllib "go.yaml.in/yaml/v4"
)

// installRun carries what the success output of one `install` needs from
// runInstall. A nil *installRun is `update`, whose output is not the
// Oracle's install transcript (issue #50, ruling R13).
type installRun struct {
	started time.Time
	// localDeclared maps the apm_modules key of a direct local package to
	// its path as declared; the summary names a local package by that path.
	localDeclared map[string]string
	targetFlag    string
	noProvenance  bool
	// manifestCreated: this run created apm.yml, which then has to get the
	// packages also when the lock does not change (a lock left from an
	// earlier install), as in the Oracle.
	manifestCreated bool
}

func (r *installRun) elapsed() string {
	return fmt.Sprintf("%.1fs", time.Since(r.started).Seconds())
}

// installRefusedError is an install the deploy refused before it wrote
// anything (deploy.RejectedInstructionError). The block of the refused
// package is printed; runInstall prints the rest with printEnd, after it
// removed what the install created.
type installRefusedError struct {
	run *installRun
	// name and reason are the Oracle's failure line: the package's name, and
	// the reason with the prefix its source gives (install/sources.py:85,
	// 115,283).
	name, reason string
	err          error
}

func (e *installRefusedError) Error() string { return e.err.Error() }

func (e *installRefusedError) Unwrap() error { return e.err }

// rejectedInstall prints the header of the package whose instruction the
// deploy refused, and for the project's own content the Oracle's line about
// it (install/phases/integrate.py:396-407).
func (r *installRun) rejectedInstall(e *deploy.RejectedInstructionError, result *resolver.ResolutionResult) error {
	refused := &installRefusedError{run: r, err: e}
	if e.DepKey == "" {
		ux.SuccessAt(os.Stdout, "  ", "<project root> (local)")
		ux.Error(os.Stdout, "Root project primitives could not be integrated: %v", e.Err)
		refused.name, refused.reason = "<root>", "Failed to integrate root project primitives: "+e.Err.Error()
		return refused
	}
	for _, dep := range result.Deps {
		if dep.Key == e.DepKey {
			ux.SuccessAt(os.Stdout, "  ", "%s", r.installBlockLabel(dep))
		}
	}
	// A git package the Oracle names "from cached package" also when this
	// run fetched it (measured).
	prefix := "Failed to integrate primitives from cached package"
	if _, local := r.localDeclared[e.DepKey]; local {
		prefix = "Failed to integrate primitives from local package"
	}
	refused.name, refused.reason = r.dependencyName(e.DepKey, result), prefix+": "+e.Err.Error()
	return refused
}

// printEnd prints the Oracle's last lines of a refused install
// (utils/diagnostics.py:500-507, core/command_logger.py:925-931).
func (e *installRefusedError) printEnd() {
	ux.ErrorAt(os.Stdout, "  ", "1 package failed:")
	ux.Plain(os.Stdout, "    +- %s -- %s", e.name, e.reason)
	ux.Error(os.Stdout, "Installation failed with 1 error(s) in %s. No install transaction changes were committed.", e.run.elapsed())
}

// installKindLabels names a deployed-file kind the way the Oracle's per-kind
// line does (_label_and_deploy_dir, install/services.py:105). Every label but
// "file(s)" was compared against the output of Oracle v0.33.0 for a local
// package (the claude kinds, and copilot's "instruction(s)" and "prompts");
// "file(s)" comes from the Oracle's source only.
var installKindLabels = map[string]string{
	"skill":       "skill(s)",
	"agent":       "agents",
	"instruction": "instruction(s)",
	"command":     "commands",
	"prompt":      "prompts",
	"file":        "file(s)",
}

// printInstallBlock prints one package of the install summary: its header and
// its printInstallBlockLines. It returns the number of integrated files.
func printInstallBlock(label string, files []string, hashes, before map[string]string) int {
	ux.SuccessAt(os.Stdout, "  ", "%s", label)
	return printInstallBlockLines(files, hashes, before)
}

// printInstallBlockLines prints one line per kind of deployed file, the
// Oracle's shape (install/services.py:560-689). A file the lock already
// recorded with the hash it has now was adopted, not integrated; a package
// with no integrated file ends with "(files unchanged)". It returns the number
// of integrated files.
func printInstallBlockLines(files []string, hashes, before map[string]string) int {
	type group struct {
		label               string
		roots               []string
		integrated, adopted map[string]bool
	}
	var order []string
	groups := map[string]*group{}
	for _, f := range files {
		kind := deployedFileKind(f)
		root := deployedFileRoot(f, kind)
		key := kind
		kindLabel := installKindLabels[kind]
		if kind == "instruction" && strings.HasSuffix(root, "/rules/") {
			key, kindLabel = "rule", "rule(s)"
		}
		g, ok := groups[key]
		if !ok {
			g = &group{label: kindLabel, integrated: map[string]bool{}, adopted: map[string]bool{}}
			groups[key] = g
			order = append(order, key)
		}
		if !containsString(g.roots, root) {
			g.roots = append(g.roots, root)
		}
		name := f
		if kind != "file" {
			name = deployedPrimitiveName(f, root)
		}
		// The Oracle reports a skill and a command as integrated on every
		// run, also when no file changed (measured on v0.33.0).
		if h, ok := before[f]; ok && h == hashes[f] && kind != "skill" && kind != "command" {
			g.adopted[name] = true
		} else {
			g.integrated[name] = true
		}
	}

	total := 0
	for _, key := range order {
		g := groups[key]
		// One primitive can be several files (a skill directory): it is
		// integrated when any of them is.
		for name := range g.integrated {
			delete(g.adopted, name)
		}
		integrated, adopted := len(g.integrated), len(g.adopted)
		total += integrated
		phrase := fmt.Sprintf("%d %s adopted", adopted, g.label)
		if integrated > 0 {
			phrase = fmt.Sprintf("%d %s integrated", integrated, g.label)
			if adopted > 0 {
				phrase += fmt.Sprintf(" (%d adopted)", adopted)
			}
		}
		sort.Strings(g.roots)
		ux.Plain(os.Stdout, "  |-- %s -> %s", phrase, strings.Join(g.roots, ", "))
	}
	if total == 0 {
		ux.Plain(os.Stdout, "  |-- (files unchanged)")
	}
	return total
}

func containsString(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

// installBlockLabel names a dependency in the install summary: a local
// package by its declared path, as the Oracle does; any other by its key and
// version, which was not compared against the Oracle (no remote was
// reachable).
func (r *installRun) installBlockLabel(dep resolver.ResolvedDep) string {
	if declared, ok := r.localDeclared[dep.Key]; ok {
		return declared + " (local)"
	}
	return dep.Key + depVersionLabel(dep)
}

// dependencyName is the Oracle's get_unique_key (models/dependency/
// identity.py:184-233): a local package's declared path, else its key, behind
// its host when that is not github.com.
func (r *installRun) dependencyName(key string, result *resolver.ResolutionResult) string {
	if declared, ok := r.localDeclared[key]; ok {
		return declared
	}
	for _, dep := range result.Deps {
		if dep.Key == key {
			if host := strings.ToLower(strings.TrimSpace(dep.Host)); host != "" && host != "github.com" && dep.ArtifactoryPrefix == "" {
				return host + "/" + key
			}
		}
	}
	return key
}

// hashesOnDisk reads the current hash of every file the lock records, so the
// install summary can tell an adopted file from an integrated one.
func hashesOnDisk(lock *lockfile.Lockfile, root string) map[string]string {
	before := map[string]string{}
	if lock == nil {
		return before
	}
	record := func(files []string) {
		for _, f := range files {
			if h, err := lockfile.HashDeployedFile(filepath.Join(root, filepath.FromSlash(f))); err == nil {
				before[f] = h
			}
		}
	}
	for i := range lock.Dependencies {
		record(lock.Dependencies[i].DeployedFiles)
	}
	record(lock.LocalDeployedFiles)
	return before
}

// printCompileHint tells the user to run compile when a dependency brought
// instructions and a target reads them from AGENTS.md
// (install/phases/finalize.py:147).
func printCompileHint(targets []string, result *resolver.ResolutionResult) {
	family := compile.FilterAgentsFamily(targets)
	if len(family) == 0 {
		return
	}
	for _, dep := range result.Deps {
		matches, _ := filepath.Glob(filepath.Join("apm_modules", filepath.FromSlash(dep.Key), ".apm", "instructions", "*.instructions.md"))
		if len(matches) > 0 {
			ux.Info(os.Stdout, "Instructions installed for %s. Run 'apm-go compile' to update AGENTS.md.", strings.Join(family, ", "))
			return
		}
	}
}

// printInstallSummary prints the last line of an install
// (core/command_logger.py:958). apmCount and mcpCount are what this run
// installed or configured, not what the lock holds.
func (r *installRun) printInstallSummary(apmCount, mcpCount int) {
	var parts []string
	if apmCount > 0 {
		parts = append(parts, fmt.Sprintf("%d APM %s", apmCount, pluralWord(apmCount, "dependency", "dependencies")))
	}
	if mcpCount > 0 {
		parts = append(parts, fmt.Sprintf("%d MCP %s", mcpCount, pluralWord(mcpCount, "server", "servers")))
	}
	if len(parts) == 0 {
		ux.Info(os.Stdout, "No changes -- install state already up to date in %s.", r.elapsed())
		return
	}
	ux.Success(os.Stdout, "Installed %s in %s.", strings.Join(parts, " and "), r.elapsed())
}

// pathTargets names the target that owns each deployed path of newLock, for
// the lock's deployments section: the first of this run's targets whose
// adapter can write the path, else what the old lock recorded.
func pathTargets(newLock, existingLock *lockfile.Lockfile, targets []string) map[string]string {
	owners := map[string]string{}
	assign := func(files []string, bucket deploy.DeployBucket) {
		for _, f := range files {
			for _, t := range targets {
				if deploy.TargetsGovernPath([]string{t}, bucket, f) {
					owners[f] = t
					break
				}
			}
			if _, ok := owners[f]; !ok && existingLock != nil && existingLock.PathTargets[f] != "" {
				owners[f] = existingLock.PathTargets[f]
			}
		}
	}
	for i := range newLock.Dependencies {
		assign(newLock.Dependencies[i].DeployedFiles, deploy.DependencyBucket)
	}
	assign(newLock.LocalDeployedFiles, deploy.LocalBucket)
	return owners
}

// installOutcome is what deployAndFinalize hands to finish.
type installOutcome struct {
	node                        *yamllib.Node
	packages                    []string
	effectiveSubsets            map[string][]string
	dev                         bool
	manifestSectionMoved        bool
	result                      *resolver.ResolutionResult
	targets                     []string
	newLock, existingLock       *lockfile.Lockfile
	existingNode                *yamllib.Node
	apmInstalled, mcpConfigured int
	apmPhase                    bool
	skippedDeps                 []deploy.SkippedDep
	commandNotes                []deploy.CommandNote
}

// finish writes the lock and apm.yml of an install and prints its last lines.
func (r *installRun) finish(o installOutcome) error {
	lock := o.newLock
	existingVersion := ""
	if o.existingLock != nil {
		existingVersion = o.existingLock.APMVersion
	}
	hasVersion := lockHasAPMVersion(o.existingNode)
	lock.APMVersion = apmVersionFor(lock, existingVersion, hasVersion, r.noProvenance)

	// With nothing to record and no lock yet, the Oracle writes no file.
	empty := len(lock.Dependencies) == 0 && len(lock.LocalDeployedFiles) == 0 && len(lock.MCPServers) == 0
	unchanged := o.existingLock != nil && lockfile.IsSemanticEqual(o.existingLock, lock) && !o.manifestSectionMoved &&
		lockIsCanonical(o.existingLock, o.existingNode)
	lockCurrent := unchanged && hasVersion == wantsAPMVersion(lock, hasVersion, r.noProvenance)
	if !lockCurrent && !(o.existingLock == nil && empty) {
		outBytes, err := lockfile.WriteLockfile(lock, o.existingNode)
		if err != nil {
			return fmt.Errorf("serialize lockfile: %w", err)
		}
		if err := writeLockfileBytes(outBytes); err != nil {
			return err
		}
		removeLegacyLocalModules(o.existingLock, lock)
	}
	if len(o.packages) > 0 && (!unchanged || r.manifestCreated) {
		if err := persistPackagesToManifest(o.node, o.packages, o.effectiveSubsets, o.dev); err != nil {
			return fmt.Errorf("update apm.yml: %w", err)
		}
		manifestBytes, err := yamlcore.SafeDump(o.node)
		if err != nil {
			return fmt.Errorf("serialize apm.yml: %w", err)
		}
		if err := os.WriteFile("apm.yml", manifestBytes, 0644); err != nil {
			return fmt.Errorf("write apm.yml: %w", err)
		}
	}

	if o.apmInstalled > 0 {
		printCompileHint(o.targets, o.result)
	}
	if !o.apmPhase {
		// The Oracle leaves one empty line when it had no package and no
		// project content to integrate.
		ux.Plain(os.Stdout, "")
	}
	for _, sd := range o.skippedDeps {
		// The Oracle's diagnostic (install/target_filter.py:62-67), printed
		// with the other warnings before the last line.
		ux.WarnAt(os.Stdout, "  ", "[%s] Per-dependency targets [%s] do not overlap active install targets; skipping",
			r.dependencyName(sd.Key, o.result), strings.Join(sd.Targets, ", "))
	}
	printCommandNotes(o.commandNotes, o.result)
	r.printInstallSummary(o.apmInstalled, o.mcpConfigured)
	return nil
}

// printCommandNotes prints what deploying prompts as commands had to tell,
// as the Oracle's diagnostics summary does (utils/diagnostics.py:39-50,
// 486-519): every warning, then every info, each in the order of the install
// blocks.
func printCommandNotes(notes []deploy.CommandNote, result *resolver.ResolutionResult) {
	rank := map[string]int{"": len(result.Deps)}
	for i, dep := range result.Deps {
		rank[dep.Key] = i
	}
	sorted := slices.Clone(notes)
	slices.SortStableFunc(sorted, func(a, b deploy.CommandNote) int { return rank[a.DepKey] - rank[b.DepKey] })
	for _, n := range sorted {
		if !n.Info {
			ux.WarnAt(os.Stdout, "  ", "%s", commandNoteText(n))
		}
	}
	for _, n := range sorted {
		if n.Info {
			ux.InfoAt(os.Stdout, "  ", "%s", commandNoteText(n))
		}
	}
}

// commandNoteText is a note with the package prefix the Oracle gives a
// warning; an info has none.
func commandNoteText(n deploy.CommandNote) string {
	if n.Info || n.Package == "" {
		return n.Text
	}
	return "[" + n.Package + "] " + n.Text
}

// lockHasAPMVersion reports whether the lock document has a top-level
// apm_version key, whatever its value or quoting; a nil node is no lock.
func lockHasAPMVersion(lockNode *yamllib.Node) bool {
	if lockNode == nil {
		return false
	}
	root := lockNode.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "apm_version" {
			return true
		}
	}
	return false
}

// wantsAPMVersion reports whether a lock is to have the apm_version line;
// has is whether the lock it replaces has it. The Oracle stamps its version
// when it locks a dependency or an MCP server, and a lock of the project's
// own content alone stays as it was; --no-provenance leaves the line out of
// any lock. The value of the line is informational: a lock that has the
// line and is to have it is not written again for the value. Adding the
// line to a lock that has none is a deliberate deviation: the Oracle, which
// has no such flag, leaves it out.
func wantsAPMVersion(lock *lockfile.Lockfile, has, noProvenance bool) bool {
	switch {
	case noProvenance:
		return false
	case len(lock.Dependencies) == 0 && len(lock.MCPServers) == 0:
		return has
	}
	return true
}

// apmVersionFor is the apm_version a lock is written with; existing is the
// value and has the presence of the line in the lock it replaces. A line
// with an empty value cannot be written, so a lock that keeps such a line
// gets the current version.
func apmVersionFor(lock *lockfile.Lockfile, existing string, has, noProvenance bool) string {
	switch {
	case !wantsAPMVersion(lock, has, noProvenance):
		return ""
	case len(lock.Dependencies) == 0 && len(lock.MCPServers) == 0 && existing != "":
		return existing
	}
	return version.Version
}

// syncLockProvenance makes the apm_version line of an existing apm.lock.yaml
// follow --no-provenance, for a command that ends without recording anything
// else. A lock whose line has to come or go is written whole, like every
// other lock; one that already agrees is not touched.
func syncLockProvenance(noProvenance bool) error {
	lock, lockNode, err := readUninstallLockfile()
	if err != nil || lock == nil {
		return err
	}
	has := lockHasAPMVersion(lockNode)
	if has == wantsAPMVersion(lock, has, noProvenance) {
		return nil
	}
	lock.APMVersion = apmVersionFor(lock, lock.APMVersion, has, noProvenance)
	out, err := lockfile.WriteLockfile(lock, lockNode)
	if err == nil {
		err = writeLockfileBytes(out)
	}
	return err
}

// lockIsCanonical reports whether apm.lock.yaml on disk is what apm-go would
// write for the lock it parsed from it. A lock in the format before issue #50
// is not, and is written again in the current format even when it records
// the same state (ruling R16). Line endings are not the format: git checks a
// committed lock out with CRLF under core.autocrlf=true, and writing it again
// would change the file on every Windows checkout (issue #119).
func lockIsCanonical(lock *lockfile.Lockfile, node *yamllib.Node) bool {
	onDisk, readErr := os.ReadFile("apm.lock.yaml")
	canonical, err := lockfile.WriteLockfile(lock, node)
	return readErr == nil && err == nil && string(canonical) == strings.ReplaceAll(string(onDisk), "\r\n", "\n")
}

// writeLockfileBytes replaces apm.lock.yaml through a temporary file in the
// same directory. The mode is 0600, the Oracle's.
func writeLockfileBytes(data []byte) error {
	rw, err := rootfs.OpenRootWriter(".")
	if err == nil {
		defer rw.Close()
		err = rw.WriteFileAtomicMode("apm.lock.yaml", data, 0o600)
	}
	if err != nil {
		return fmt.Errorf("write apm.lock.yaml: %w", err)
	}
	return nil
}

// isLegacyLocalEntry reports whether a lock entry is a local package in the
// format before issue #50: repo_url _local/<name>-<hash8>, recorded as a git
// source.
func isLegacyLocalEntry(dep *lockfile.LockedDep) bool {
	return strings.HasPrefix(dep.RepoURL, localModulesKeyPrefix) && dep.Source != "local"
}

// removeLegacyLocalModules deletes the apm_modules directory of each local
// package the old lock names in the format before issue #50 and the new lock
// no longer has, and the clone of each `git:` local path the old lock records
// as a git source (isGitPathEntry) where the new lock records that path as a
// local package. Only a directory an old repo_url names is removed, and not
// one that is, holds or lies in the directory of a package the new lock has:
// another key can name the same place (`git: ./_local/repo` was cloned to the
// apm_modules/_local/repo its local copy now takes).
func removeLegacyLocalModules(oldLock, newLock *lockfile.Lockfile) {
	if oldLock == nil {
		return
	}
	nowLocal := map[string]bool{}
	for i := range newLock.Dependencies {
		if d := &newLock.Dependencies[i]; d.Source == "local" {
			nowLocal[d.LocalPath] = true
		}
	}
	for i := range oldLock.Dependencies {
		dep := &oldLock.Dependencies[i]
		gitPath := nowLocal[dep.RepoURL] && isGitPathEntry(dep, dep.RepoURL)
		if (!isLegacyLocalEntry(dep) && !gitPath) || overlapsModuleDir(newLock, dep.UniqueKey()) {
			continue
		}
		if _, err := deploy.SafeRemoveModuleDir(".", dep.UniqueKey()); err != nil {
			ux.Warn(os.Stderr, "could not remove apm_modules/%s: %v", dep.UniqueKey(), err)
		}
	}
}

// overlapsModuleDir reports whether apm_modules/<key>, cleaned as
// deploy.SafeRemoveModuleDir resolves it, is the directory of a package lock
// has, or holds it, or lies in it.
func overlapsModuleDir(lock *lockfile.Lockfile, key string) bool {
	dir := moduleDirOf(key)
	for i := range lock.Dependencies {
		pkg := moduleDirOf(lock.Dependencies[i].UniqueKey())
		if dir == pkg || strings.HasPrefix(pkg, dir+string(filepath.Separator)) || strings.HasPrefix(dir, pkg+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func moduleDirOf(key string) string {
	return filepath.Join("apm_modules", filepath.FromSlash(key))
}
