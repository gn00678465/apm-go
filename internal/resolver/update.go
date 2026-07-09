package resolver

import (
	"fmt"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

// PlanFullUpdate re-resolves every direct dependency against current manifest
// constraints (req-rs-011). Does NOT replay locked pins — all direct deps get fresh resolution.
func PlanFullUpdate(
	m *manifest.Manifest,
	lock *lockfile.Lockfile,
	tags TagLister,
	loader PackageLoader,
	cfg ResolverConfig,
) (*ResolutionResult, error) {
	// Full update = resolve from scratch, ignoring lock
	return Resolve(m, nil, tags, loader, cfg)
}

// PlanScopedUpdate re-resolves only the named package and its subtree,
// holding all other pins from the existing lockfile (req-rs-012).
func PlanScopedUpdate(
	m *manifest.Manifest,
	lock *lockfile.Lockfile,
	tags TagLister,
	loader PackageLoader,
	cfg ResolverConfig,
	packageName string,
	frozen bool,
) (*ResolutionResult, error) {
	if frozen {
		return nil, fmt.Errorf("cannot update in frozen install mode; use --no-frozen to override")
	}

	if lock == nil {
		return nil, fmt.Errorf("scoped update requires an existing lockfile")
	}

	// Verify the named package exists in the manifest -- regular or dev
	// (F3-adjacent: Python's `apm update <pkg>` resolves against
	// apm_deps + dev_apm_deps, so a devDependencies.apm entry is a valid
	// scoped-update target too).
	found := false
	for _, dep := range m.ParsedDeps {
		if depKey(dep) == packageName {
			found = true
			break
		}
	}
	if !found {
		for _, dep := range m.ParsedDevDeps {
			if depKey(dep) == packageName {
				found = true
				break
			}
		}
	}
	if !found {
		return nil, fmt.Errorf("package %q not found in manifest", packageName)
	}

	// Walk ResolvedBy transitively to collect the full subtree rooted at packageName
	exclude := map[string]bool{packageName: true}
	changed := true
	for changed {
		changed = false
		for _, dep := range lock.Dependencies {
			if exclude[dep.ResolvedBy] && !exclude[dep.UniqueKey()] {
				exclude[dep.UniqueKey()] = true
				changed = true
			}
		}
	}

	scopedLock := &lockfile.Lockfile{
		Version:     lock.Version,
		GeneratedAt: lock.GeneratedAt,
		APMVersion:  lock.APMVersion,
	}
	for _, dep := range lock.Dependencies {
		if exclude[dep.UniqueKey()] {
			continue
		}
		scopedLock.Dependencies = append(scopedLock.Dependencies, dep)
	}

	return Resolve(m, scopedLock, tags, loader, cfg)
}
