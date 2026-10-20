package main

import (
	"errors"
	"os"
	"regexp"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/semver"
	"github.com/apm-go/apm/internal/ux"
)

// fullRevisionPin is is_full_revision_pin's _SHA_RE (deps/revision_pins.py:21,77-79).
var fullRevisionPin = regexp.MustCompile(`^[a-fA-F0-9]{40}$`)

// checkFrozenManifestSync is the Oracle's lockfile_satisfies_manifest
// (install/plan.py:453-506), which its --frozen runs before it resolves
// anything (install/service.py:300-353): every direct dependency that is not
// local has an entry in the lock, and the entry still pins what apm.yml
// declares. The Oracle also compares MCP servers there; apm-go does not.
func checkFrozenManifestSync(deps []*manifest.DependencyReference, lock *lockfile.Lockfile) error {
	locked := make(map[string]*lockfile.LockedDep, len(lock.Dependencies))
	for i := range lock.Dependencies {
		locked[lockedUniqueKey(&lock.Dependencies[i])] = &lock.Dependencies[i]
	}
	var reasons []string
	for _, dep := range deps {
		if dep.IsLocal || dep.IsParent || dep.LocalSourcePath != "" {
			continue
		}
		key := refUniqueKey(dep)
		entry, ok := locked[key]
		if !ok {
			reasons = append(reasons, "  - "+key+" is declared in apm.yml but missing from apm.lock.yaml")
			continue
		}
		if ref := dep.Reference; fullRevisionPin.MatchString(strings.TrimSpace(ref)) && !strings.EqualFold(entry.ResolvedCommit, ref) {
			commit := entry.ResolvedCommit
			if commit == "" {
				commit = "(missing)"
			}
			reasons = append(reasons, "  - "+key+": manifest commit '"+ref+"' != lockfile resolved_commit '"+commit+"'")
			continue
		}
		if lockedRefChanged(dep, entry) {
			reasons = append(reasons, "  - "+key+": declared source, ref, host provider, or transport differs from apm.lock.yaml")
		}
	}
	if len(reasons) == 0 {
		return nil
	}
	ux.Error(os.Stdout, "--frozen: apm.lock.yaml is out of sync with apm.yml.")
	for _, r := range reasons {
		ux.Plain(os.Stdout, "%s", r)
	}
	// The Oracle's tip names `apm install` and `--root DIR or --global`;
	// apm-go has no --root.
	ux.Info(os.Stdout, "Tip: rerun 'apm-go install' without --frozen to create or repair package lock state, retaining the original options, including --global if specified.")
	return withSilentExitCode(1, errors.New("--frozen: apm.lock.yaml is out of sync with apm.yml:\n"+strings.Join(reasons, "\n")))
}

// lockedRefChanged is the Oracle's detect_ref_change (drift.py:95-195) for
// an entry the lock has. The host provider comparison of the Oracle follows
// the host and the git `type:`; the host is part of the key both sides are
// matched by, and apm-go reads no `type:`, so it never differs here.
func lockedRefChanged(dep *manifest.DependencyReference, entry *lockfile.LockedDep) bool {
	source := func(s string) string {
		if s == "" {
			return "git"
		}
		return s
	}
	if source(dep.Source) != source(entry.Source) {
		return true
	}
	if dep.Source == "registry" {
		if !semver.IsSemverRange(dep.Reference) {
			return dep.Reference != entry.Version
		}
		ok, err := semver.Satisfies(entry.Version, dep.Reference)
		return entry.Version == "" || err != nil || !ok
	}
	if (dep.Scheme == "http") != entry.IsInsecure {
		return true
	}
	if resolver.ClassifyReference(dep) == resolver.KindGitSemver {
		return dep.Reference != entry.Constraint
	}
	return dep.Reference != entry.ResolvedRef
}

// refUniqueKey and lockedUniqueKey are get_unique_key
// (models/dependency/identity.py:184-235) of a declared dependency that is
// not local and of a lock entry: a host other than github.com qualifies the
// key, unless the package comes through a registry proxy.
func refUniqueKey(dep *manifest.DependencyReference) string {
	return hostQualified(deploy.DepRefKey(dep), dep.Host, dep.ArtifactoryPrefix)
}

func lockedUniqueKey(entry *lockfile.LockedDep) string {
	if entry.Source == "local" && entry.LocalPath != "" {
		return entry.LocalPath
	}
	return hostQualified(entry.UniqueKey(), entry.Host, entry.RegistryPrefix)
}

func hostQualified(key, host, registryPrefix string) string {
	if registryPrefix != "" {
		return key
	}
	if h := strings.ToLower(strings.TrimSpace(host)); h != "" && h != "github.com" {
		return h + "/" + key
	}
	return key
}
