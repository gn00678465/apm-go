package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/ux"
)

// resolveRefusedError is a resolve that refused a package directory below
// apm_modules/ that leads out of it. The Oracle refuses it before it prints
// the package's Resolving line and reports it as a failed install
// (install/phases/resolve.py:455-464, commands/install.py:1948-1957).
type resolveRefusedError struct {
	err     error
	reason  *archive.ContainmentError
	verbose bool
	started time.Time
}

func (e *resolveRefusedError) Error() string { return e.err.Error() }

func (e *resolveRefusedError) Unwrap() error { return e.err }

func (e *resolveRefusedError) print() {
	ux.Error(os.Stdout, "Failed to install APM dependencies: %v", e.reason)
	if !e.verbose {
		ux.Info(os.Stdout, "Run with --verbose for detailed diagnostics")
	}
	ux.Error(os.Stdout, "Install failed after %.1fs.", time.Since(e.started).Seconds())
}

// summaryKinds is the order of the per-kind lines: the Oracle's dispatch
// table (integration/dispatch.py:52-84), then apm-go's own "files"
// (antigravity's plugin.json, which the Oracle does not deploy). Skills come
// last, from their own integrator (install/services.py:597-660).
var summaryKinds = []string{"prompts", "agents", "commands", "instructions", "hooks", "files"}

// adoptable tells whether a deployed file can count as adopted. The Oracle
// adopts a file whose bytes on disk are its source's
// (integration/base_integrator.py:378-405), so a command and a codex agent,
// which are converted, and a hook, which it merges, are integrated on every
// run (integration/agent_integrator.py:259-283, install/services.py:534-537).
func adoptable(f string) bool {
	switch summaryKindOf(f) {
	case "commands", "hooks":
		return false
	case "agents":
		return !strings.HasPrefix(f, ".codex/")
	}
	return true
}

func summaryKindOf(f string) string {
	switch deployedFileKind(f) {
	case "prompt":
		return "prompts"
	case "agent":
		return "agents"
	case "command":
		return "commands"
	case "instruction":
		return "instructions"
	case "skill":
		return "skills"
	}
	if path.Base(f) == "hooks.json" || strings.Contains(f, "/hooks/") {
		return "hooks"
	}
	return "files"
}

// summaryTargetOf names the target that wrote a deployed path. Sorted, the
// names are the order the Oracle walks its targets in
// (core/target_detection.py:1101-1130).
func summaryTargetOf(f string) string {
	switch strings.SplitN(f, "/", 2)[0] {
	case ".claude":
		return "claude"
	case ".codex":
		return "codex"
	case ".github":
		return "copilot"
	case ".opencode":
		return "opencode"
	}
	return "antigravity"
}

// summaryDir is the directory a per-kind line names for a target: the
// Oracle's "<root>/<subdir>/", or a hook's config file
// (install/services.py:105-120,527-541).
func summaryDir(f, kind string) string {
	switch kind {
	case "hooks":
		if path.Base(f) == "hooks.json" {
			return f
		}
		return path.Dir(f) + "/"
	case "files":
		return path.Dir(f) + "/"
	}
	return deployedFileRoot(f, deployedFileKind(f))
}

// collapseTargets is the Oracle's format_target_collapse
// (install/deployed_paths.py:13-24): one or two directories by name, more
// as a count, and with verbose each one on a line of its own.
func collapseTargets(paths []string, verbose bool) (string, []string) {
	var deduped []string
	for _, p := range paths {
		if !slices.Contains(deduped, p) {
			deduped = append(deduped, p)
		}
	}
	switch {
	case verbose && len(deduped) >= 2:
		var lines []string
		for _, p := range deduped {
			lines = append(lines, "  |     -> "+p)
		}
		return "", lines
	case len(deduped) == 0:
		return "", nil
	case len(deduped) <= 2:
		return strings.Join(deduped, ", "), nil
	}
	return fmt.Sprintf("%d targets", len(deduped)), nil
}

func printSummaryLine(phrase string, paths []string, verbose bool) {
	suffix, expansion := collapseTargets(paths, verbose)
	if expansion != nil {
		ux.Plain(os.Stdout, "  |-- %s:", phrase)
		for _, line := range expansion {
			ux.Plain(os.Stdout, "%s", line)
		}
		return
	}
	ux.Plain(os.Stdout, "  |-- %s -> %s", phrase, suffix)
}

// blockFiles is what one package of the install summary deployed: files
// with their hashes now, and the paths of its shadowed primitives that count
// as adopted or integrated, a skill's by its name.
type blockFiles struct {
	files      []string
	hashes     map[string]string
	adopted    []string
	integrated []string
	skills     map[string][]string
	rootSkill  *deploy.RootSkill
}

// printSummaryLines prints the lines of one package of the install summary
// as the Oracle aggregates them (install/services.py:440-689): one line per
// kind in dispatch order, the files of every target added together, a file
// whose hash the lock recorded and still finds on disk adopted, the
// directories in target order, and the label of instructions the last
// target's. A command is integrated on every run, and so is each skill
// below .apm/skills/; a package whose root is a skill prints its skill
// alone, integrated only when its directory was new. It returns the number
// of integrated files.
func printSummaryLines(b blockFiles, before map[string]string, verbose bool) int {
	type perTarget struct {
		integrated, adopted int
		dir                 string
	}
	byKind := map[string]map[string]*perTarget{}
	note := func(f string, adopted bool) {
		kind := summaryKindOf(f)
		target := summaryTargetOf(f)
		if byKind[kind] == nil {
			byKind[kind] = map[string]*perTarget{}
		}
		t := byKind[kind][target]
		if t == nil {
			t = &perTarget{dir: summaryDir(f, kind)}
			byKind[kind][target] = t
		}
		if adopted {
			t.adopted++
		} else {
			t.integrated++
		}
	}
	skillNames := map[string]bool{}
	skillRoots := map[string]bool{}
	for _, f := range b.files {
		if summaryKindOf(f) == "skills" {
			root := deployedFileRoot(f, "skill")
			skillNames[deployedPrimitiveName(f, root)] = true
			skillRoots[strings.SplitN(f, "/", 2)[0]+"/skills/"] = true
			continue
		}
		h, recorded := before[f]
		note(f, recorded && h == b.hashes[f] && adoptable(f))
	}
	for _, f := range b.adopted {
		note(f, true)
	}
	for _, f := range b.integrated {
		note(f, false)
	}
	for name, dirs := range b.skills {
		skillNames[name] = true
		for _, d := range dirs {
			skillRoots[path.Dir(d)+"/"] = true
		}
	}

	total := 0
	for _, kind := range summaryKinds {
		targets := byKind[kind]
		if len(targets) == 0 {
			continue
		}
		names := make([]string, 0, len(targets))
		for t := range targets {
			names = append(names, t)
		}
		sort.Strings(names)
		var integrated, adopted int
		var dirs []string
		for _, name := range names {
			t := targets[name]
			integrated += t.integrated
			adopted += t.adopted
			dirs = append(dirs, t.dir)
		}
		label := map[string]string{"prompts": "prompts", "agents": "agents", "commands": "commands",
			"instructions": "instruction(s)", "hooks": "hook(s)", "files": "file(s)"}[kind]
		if kind == "instructions" && names[len(names)-1] == "claude" {
			label = "rule(s)"
		}
		phrase := fmt.Sprintf("%d %s adopted", adopted, label)
		if integrated > 0 {
			phrase = fmt.Sprintf("%d %s integrated", integrated, label)
			if adopted > 0 {
				phrase += fmt.Sprintf(" (%d adopted)", adopted)
			}
		}
		total += integrated
		printSummaryLine(phrase, dirs, verbose)
	}

	roots := make([]string, 0, len(skillRoots))
	for r := range skillRoots {
		roots = append(roots, r)
	}
	sort.Strings(roots)
	if b.rootSkill != nil {
		// The Oracle counts a package's sub-skills only below
		// .github/skills/, where no target writes them
		// (integration/skill_integrator.py:1241-1245).
		skillNames = nil
		if b.rootSkill.Created {
			total++
			printSummaryLine("Skill integrated", roots, verbose)
		}
	}
	if n := len(skillNames); n > 0 {
		total += n
		printSummaryLine(fmt.Sprintf("%d skill(s) integrated", n), roots, verbose)
	}
	if total == 0 {
		ux.Plain(os.Stdout, "  |-- (files unchanged)")
	}
	return total
}

// blockHeader is the Oracle's download_complete line of a package
// (core/command_logger.py:405-438): a local package's declared path, else
// its repository path, or for a virtual package str(dep_ref)
// (install/sources.py:370), then the reference and short commit the run
// resolved, or for a package it did not fetch the ones the lock recorded
// (install/sources.py:370-395,726-731), and "(cached)" for that one.
func (r *installRun) blockHeader(dep resolver.ResolvedDep, newLock, oldLock *lockfile.Lockfile) string {
	if declared, ok := r.localDeclared[dep.Key]; ok {
		return declared + " (local)"
	}
	name := dep.RepoURL
	if dep.VirtualPath != "" {
		host := dep.Host
		if dep.Port != 0 {
			host = fmt.Sprintf("%s:%d", host, dep.Port)
		}
		name = host + "/" + dep.RepoURL + "/" + dep.VirtualPath
		if dep.Constraint != "" {
			name += "#" + dep.Constraint
		}
	}
	var now, old *lockfile.LockedDep
	if newLock != nil {
		now = newLock.FindByKey(dep.Key)
	}
	if oldLock != nil {
		old = oldLock.FindByKey(dep.Key)
	}
	fetched := r.fetched[dep.Key]
	var ref, sha string
	switch {
	case dep.Kind == resolver.KindRegistry:
		ref = dep.Constraint
		if now != nil && now.Version != "" {
			ref = now.Version
		}
	case fetched:
		switch {
		case dep.ResolvedTag != "":
			ref = dep.ResolvedTag
		case dep.ResolvedRef != "":
			ref = dep.ResolvedRef
		case isSubdirectoryPackage(dep.VirtualPath):
			// The Oracle's subdirectory download names no branch
			// (deps/github_downloader.py:1746-1747).
			ref = "default"
		default:
			ref = gitops.CheckedOutBranch(filepath.Join("apm_modules", filepath.FromSlash(dep.Key)))
		}
		if now != nil {
			sha = now.ResolvedCommit
		}
	default:
		// apm-go's lock records a semver pin as resolved_tag alone, where
		// the Oracle's records it as resolved_ref too.
		switch {
		case old != nil && old.ResolvedRef != "":
			ref = old.ResolvedRef
		case old != nil && old.ResolvedTag != "":
			ref = old.ResolvedTag
		default:
			ref = dep.Constraint
		}
		if old != nil {
			sha = old.ResolvedCommit
		}
	}
	if len(sha) > shortCommitLen {
		sha = sha[:shortCommitLen]
	}
	switch {
	case ref != "" && sha != "":
		name += " #" + ref + " @" + sha
	case ref != "":
		name += " #" + ref
	case sha != "":
		name += " @" + sha
	}
	if !fetched {
		name += " (cached)"
	}
	return name
}

// listedDeps is the set of packages the summary lists: every resolved one,
// or for `install <pkg>` the named ones and what they bring, as the Oracle
// integrates only those (install/phases/integrate.py:614-674).
func listedDeps(result *resolver.ResolutionResult, requestedKeys map[string]bool, scoped bool) map[string]bool {
	listed := map[string]bool{"": true}
	parent := map[string]string{}
	for _, dep := range result.Deps {
		parent[dep.Key] = dep.ResolvedBy
	}
	for _, dep := range result.Deps {
		if !scoped {
			listed[dep.Key] = true
			continue
		}
		seen := map[string]bool{}
		for key := dep.Key; key != "" && !seen[key]; key = parent[key] {
			seen[key] = true
			if requestedKeys[key] {
				listed[dep.Key] = true
				break
			}
		}
	}
	return listed
}

// shadowOutcome is what the Oracle reports for a primitive that another
// dependency's primitive of the same name kept off a path.
type shadowOutcome int

const (
	shadowNone shadowOutcome = iota
	shadowAdopted
	shadowIntegrated
	shadowCollision
)

// managedPaths is the Oracle's managed set (install/pipeline.py:723-728):
// every path the lock before the run records, for a dependency and for the
// project's own content, which the Oracle reads as a dependency of its own
// (deps/lockfile.py:951-963).
func managedPaths(oldLock *lockfile.Lockfile) map[string]bool {
	managed := map[string]bool{}
	if oldLock == nil {
		return managed
	}
	for i := range oldLock.Dependencies {
		for _, f := range oldLock.Dependencies[i].DeployedFiles {
			managed[normalizeDeployPath(f)] = true
		}
	}
	for _, f := range oldLock.LocalDeployedFiles {
		managed[normalizeDeployPath(f)] = true
	}
	return managed
}

// shadowedOutcome decides it as the Oracle's integrators do
// (integration/base_integrator.py:198-241,429-495): what the Oracle compares
// on disk adopts the file, or for a skill integrates it
// (skill_integrator.py, measured as review122/probes/sameskill); a managed
// path is written over, which leaves the disk as it is when the primitive
// deploys what is there and is the owner-pick deviation otherwise; anything
// else is a skipped file, but a claude rule, which the Oracle writes over
// (integration/instruction_integrator.py:450-485).
func shadowedOutcome(s deploy.Shadowed, managed map[string]bool) shadowOutcome {
	switch {
	case s.Same && s.Type == deploy.TypeSkills:
		return shadowIntegrated
	case s.Same:
		return shadowAdopted
	case anyManaged(s.Paths, managed):
		if s.Deployed {
			return shadowIntegrated
		}
		return shadowNone
	}
	for _, p := range s.Paths {
		if strings.HasPrefix(p, ".claude/rules/") {
			return shadowNone
		}
	}
	return shadowCollision
}

// anyManaged tells whether a path, or for a skill's directory a path below
// it, is managed.
func anyManaged(paths []string, managed map[string]bool) bool {
	for _, p := range paths {
		p = normalizeDeployPath(p)
		if managed[p] {
			return true
		}
		for m := range managed {
			if strings.HasPrefix(m, p+"/") {
				return true
			}
		}
	}
	return false
}

// collisionCount is the Oracle's count of files it skipped because a file
// it does not manage was there (install/pipeline.py:723-728), over the
// listed packages; a skill counts once whatever its directories.
func collisionCount(shadowed []deploy.Shadowed, listed map[string]bool, managed map[string]bool) int {
	seen := map[string]bool{}
	count := 0
	for _, s := range shadowed {
		if !listed[s.DepKey] || shadowedOutcome(s, managed) != shadowCollision {
			continue
		}
		for _, p := range s.Paths {
			key := s.DepKey + "\x00" + p
			if s.Type == deploy.TypeSkills {
				key = s.DepKey + "\x00skill\x00" + s.Name
			}
			if !seen[key] {
				seen[key] = true
				count++
			}
		}
	}
	return count
}

// unpinnedLine is the Oracle's warning about git packages with no
// reference (install/phases/finalize.py:226-261): the count of such listed
// packages and up to five of their repository paths. Deliberate deviation
// by owner ruling (issue #122 comment 6232421913): the Oracle also names
// each local package, by its _local/<name>, which has no version to pin;
// apm-go names git packages alone, as it counts them.
func (r *installRun) unpinnedLine(result *resolver.ResolutionResult, listed map[string]bool) string {
	count := 0
	var names []string
	for _, dep := range result.Deps {
		if !listed[dep.Key] || dep.Kind == resolver.KindRegistry || dep.Constraint != "" {
			continue
		}
		if _, local := r.localDeclared[dep.Key]; local {
			continue
		}
		count++
		if !slices.Contains(names, dep.RepoURL) {
			names = append(names, dep.RepoURL)
		}
	}
	if count == 0 {
		return ""
	}
	shown := strings.Join(names[:min(5, len(names))], ", ")
	if extra := len(names) - 5; extra > 0 {
		shown += fmt.Sprintf(", and %d more", extra)
	}
	return fmt.Sprintf("%d %s unpinned: %s -- add #tag or #sha to prevent drift",
		count, pluralWord(count, "dependency", "dependencies"), shown)
}
