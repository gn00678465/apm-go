package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/compile"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/rootfs"
	"github.com/apm-go/apm/internal/ux"
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
}

func (r *installRun) elapsed() string {
	return fmt.Sprintf("%.1fs", time.Since(r.started).Seconds())
}

// installKindLabels names a deployed-file kind the way the Oracle's per-kind
// line does (_label_and_deploy_dir, install/services.py:105). The lines for
// the four kinds the claude target deploys (rule, skill, agent, command) were
// compared against the output of Oracle v0.33.0 for a local package;
// "instruction(s)", "prompts" and "file(s)" belong to other targets and come
// from the Oracle's source only.
var installKindLabels = map[string]string{
	"skill":       "skill(s)",
	"agent":       "agents",
	"instruction": "instruction(s)",
	"command":     "commands",
	"prompt":      "prompts",
	"file":        "file(s)",
}

// printInstallBlock prints one package of the install summary: its header and
// one line per kind of deployed file, the Oracle's shape
// (install/services.py:560-689). A file the lock already recorded with the
// hash it has now was adopted, not integrated; a package with no integrated
// file ends with "(files unchanged)". It returns the number of integrated
// files.
func printInstallBlock(label string, files []string, hashes, before map[string]string) int {
	ux.SuccessAt(os.Stdout, "  ", "%s", label)

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

// hashesOnDisk reads the current hash of every file the lock records, so the
// install summary can tell an adopted file from an integrated one.
func hashesOnDisk(lock *lockfile.Lockfile, root string) map[string]string {
	before := map[string]string{}
	if lock == nil {
		return before
	}
	record := func(files []string) {
		for _, f := range files {
			if h, err := lockfile.HashFileBytes(filepath.Join(root, filepath.FromSlash(f))); err == nil {
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
}

// finish writes the lock and apm.yml of an install and prints its last lines.
func (r *installRun) finish(o installOutcome) error {
	lock := o.newLock
	if r.noProvenance {
		lock.APMVersion = ""
	} else if len(lock.Dependencies) == 0 && len(lock.MCPServers) == 0 {
		// The Oracle stamps its version when it locks a dependency or an
		// MCP server; a lock of the project's own content alone keeps what
		// it had.
		lock.APMVersion = ""
		if o.existingLock != nil {
			lock.APMVersion = o.existingLock.APMVersion
		}
	}

	// With nothing to record and no lock yet, the Oracle writes no file.
	empty := len(lock.Dependencies) == 0 && len(lock.LocalDeployedFiles) == 0 && len(lock.MCPServers) == 0
	unchanged := o.existingLock != nil && lockfile.IsSemanticEqual(o.existingLock, lock) && !o.manifestSectionMoved &&
		lockIsCanonical(o.existingLock, o.existingNode)
	// Whether the apm_version line is there follows --no-provenance, so a
	// lock that differs in that alone is written again; another value in
	// the line is not a difference. Adding the line to a lock that has
	// none is a deliberate deviation: the Oracle, which has no such flag,
	// leaves it out.
	lockCurrent := unchanged && (o.existingLock.APMVersion == "") == (lock.APMVersion == "")
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
	if len(o.packages) > 0 && !unchanged {
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
	r.printInstallSummary(o.apmInstalled, o.mcpConfigured)
	return nil
}

// lockIsCanonical reports whether apm.lock.yaml on disk is what apm-go would
// write for the lock it parsed from it. A lock in the format before issue #50
// is not, and is written again in the current format even when it records
// the same state (ruling R16).
func lockIsCanonical(lock *lockfile.Lockfile, node *yamllib.Node) bool {
	onDisk, readErr := os.ReadFile("apm.lock.yaml")
	canonical, err := lockfile.WriteLockfile(lock, node)
	return readErr == nil && err == nil && string(canonical) == string(onDisk)
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
// no longer has. Only a directory an old repo_url names is removed.
func removeLegacyLocalModules(oldLock, newLock *lockfile.Lockfile) {
	if oldLock == nil {
		return
	}
	for i := range oldLock.Dependencies {
		dep := &oldLock.Dependencies[i]
		if !isLegacyLocalEntry(dep) || newLock.FindByKey(dep.UniqueKey()) != nil {
			continue
		}
		if _, err := deploy.SafeRemoveModuleDir(".", dep.UniqueKey()); err != nil {
			ux.Warn(os.Stderr, "could not remove apm_modules/%s: %v", dep.UniqueKey(), err)
		}
	}
}
