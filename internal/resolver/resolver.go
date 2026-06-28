package resolver

import (
	"fmt"
	"strings"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

type queueEntry struct {
	ref   *manifest.DependencyReference
	depth int
	chain []string
}

// Resolve performs BFS dependency resolution with fixpoint re-expansion.
// When a diamond constraint narrows a pin, stale children are invalidated
// and the new version's children are expanded.
func Resolve(
	rootManifest *manifest.Manifest,
	lock *lockfile.Lockfile,
	tags TagLister,
	loader PackageLoader,
	cfg ResolverConfig,
) (*ResolutionResult, error) {
	if err := checkNestRejection(rootManifest); err != nil {
		return nil, err
	}

	maxDepth := cfg.maxDepth()
	result := &ResolutionResult{}

	constraints := map[string][]ConstraintEntry{}
	pins := map[string]semver.TagInfo{}
	pinRefs := map[string]string{}
	kinds := map[string]ReferenceKind{}
	depOrder := []string{}
	depDepth := map[string]int{}
	depRefs := map[string]*manifest.DependencyReference{}
	// Track which keys were contributed by which parent pin
	childrenOf := map[string][]string{} // parent key -> child keys added by current pin

	queue := []queueEntry{}
	for _, dep := range collectRootDeps(rootManifest) {
		queue = append(queue, queueEntry{
			ref:   dep,
			depth: 1,
			chain: []string{formatChainEntry(rootManifest.Name, dep)},
		})
	}

	processed := map[string]string{} // key -> pin name that was processed

	for len(queue) > 0 {
		entry := queue[0]
		queue = queue[1:]

		if entry.depth > maxDepth {
			return nil, fmt.Errorf("dependency depth limit (%d) exceeded at: %s",
				maxDepth, strings.Join(entry.chain, " -> "))
		}

		key := depKey(entry.ref)
		kind := ClassifyReference(entry.ref)

		ce := ConstraintEntry{
			Chain:      entry.chain,
			Constraint: entry.ref.Reference,
			Depth:      entry.depth,
		}
		constraints[key] = append(constraints[key], ce)

		if _, exists := kinds[key]; !exists {
			depOrder = append(depOrder, key)
			kinds[key] = kind
			depRefs[key] = entry.ref
		}

		if d, ok := depDepth[key]; !ok || entry.depth < d {
			depDepth[key] = entry.depth
		}

		// Resolve pin — check lock replay first (req-rs-004)
		var currentPin string
		switch kind {
		case KindGitSemver:
			// Lock replay: if lockfile has this dep with character-equal constraint,
			// reuse the locked tag without hitting the network (req-lk-009)
			if lock != nil && len(constraints[key]) == 1 {
				action := ReplayDecision(entry.ref, lock)
				if action == ReplayLocked {
					locked := lock.FindByKey(key)
					if locked != nil && locked.ResolvedTag != "" {
						pins[key] = semver.TagInfo{
							Name:   locked.ResolvedTag,
							Commit: locked.ResolvedCommit,
						}
						currentPin = locked.ResolvedTag
						break
					}
				}
			}

			// Fresh resolve: list tags and pick highest in intersection
			allTags, err := tags.ListTags(entry.ref.RepoURL)
			if err != nil {
				return nil, fmt.Errorf("listing tags for %s: %w", entry.ref.RepoURL, err)
			}
			winner, err := pickHighestInIntersection(constraints[key], allTags)
			if err != nil {
				return nil, fmt.Errorf("%s", formatConflictDiagnostic(key, constraints[key]))
			}
			pins[key] = winner
			currentPin = winner.Name

		case KindGitLiteral:
			ref, err := checkLiteralConflict(constraints[key])
			if err != nil {
				return nil, fmt.Errorf("%s", formatConflictDiagnostic(key, constraints[key]))
			}
			if ref != "" {
				pinRefs[key] = ref
			}
			currentPin = pinRefs[key]

		default:
			currentPin = entry.ref.Reference
		}

		// Check if this key was already processed with the same pin
		if prevPin, done := processed[key]; done && prevPin == currentPin {
			continue
		}

		// Pin changed (or first time): invalidate stale children
		if _, done := processed[key]; done {
			invalidateChildren(key, childrenOf, depOrder, kinds, constraints, pins, pinRefs, depDepth, depRefs, processed)
		}
		processed[key] = currentPin

		// Load sub-manifest
		resolvedRef := currentPin
		subManifest, err := loader.LoadPackage(entry.ref, resolvedRef)
		if err != nil || subManifest == nil {
			continue
		}

		// Track and enqueue children
		var newChildKeys []string
		for _, subDep := range collectRootDeps(subManifest) {
			childKey := depKey(subDep)
			newChildKeys = append(newChildKeys, childKey)

			subChain := make([]string, len(entry.chain))
			copy(subChain, entry.chain)
			subChain = append(subChain, formatChainEntry(key, subDep))

			queue = append(queue, queueEntry{
				ref:   subDep,
				depth: entry.depth + 1,
				chain: subChain,
			})
		}
		childrenOf[key] = newChildKeys
	}

	// Build result in deterministic first-seen order, excluding invalidated keys
	for _, key := range depOrder {
		if _, ok := processed[key]; !ok {
			continue // invalidated
		}

		dep := ResolvedDep{
			Key:     key,
			RepoURL: key,
			Kind:    kinds[key],
			Depth:   depDepth[key],
		}

		if ref := depRefs[key]; ref != nil {
			dep.VirtualPath = ref.VirtualPath
			if dep.VirtualPath != "" {
				dep.RepoURL = strings.TrimSuffix(key, "/"+dep.VirtualPath)
			}
		}

		cs := constraints[key]
		if len(cs) > 0 {
			dep.Constraint = cs[0].Constraint
			dep.ResolvedBy = findTightestParent(cs)
		}

		switch dep.Kind {
		case KindGitSemver:
			if pin, ok := pins[key]; ok {
				dep.ResolvedTag = pin.Name
				dep.Commit = pin.Commit
			}
		case KindGitLiteral:
			dep.ResolvedRef = pinRefs[key]
		}

		result.Deps = append(result.Deps, dep)
	}

	return result, nil
}

// invalidateChildren recursively removes stale children when a parent re-pins.
func invalidateChildren(
	parentKey string,
	childrenOf map[string][]string,
	depOrder []string,
	kinds map[string]ReferenceKind,
	constraints map[string][]ConstraintEntry,
	pins map[string]semver.TagInfo,
	pinRefs map[string]string,
	depDepth map[string]int,
	depRefs map[string]*manifest.DependencyReference,
	processed map[string]string,
) {
	children := childrenOf[parentKey]
	delete(childrenOf, parentKey)

	for _, childKey := range children {
		// Only invalidate if this child has no other parent contributing it
		// (For simplicity in v0.1: invalidate and let re-expansion re-add if needed)
		delete(processed, childKey)
		// Recursively invalidate grandchildren
		invalidateChildren(childKey, childrenOf, depOrder, kinds, constraints, pins, pinRefs, depDepth, depRefs, processed)
	}
}

func checkNestRejection(m *manifest.Manifest) error {
	if m.ConflictResolution == "nest" {
		return fmt.Errorf("conflict_resolution: nest is reserved for v0.2 (Section 7.2 clause 3)")
	}
	return nil
}

func collectRootDeps(m *manifest.Manifest) []*manifest.DependencyReference {
	if m == nil {
		return nil
	}
	return m.ParsedDeps
}

func depKey(ref *manifest.DependencyReference) string {
	if ref.IsLocal {
		return ref.LocalPath
	}
	key := ref.RepoURL
	if ref.VirtualPath != "" {
		key += "/" + ref.VirtualPath
	}
	return key
}

func formatChainEntry(parent string, dep *manifest.DependencyReference) string {
	key := depKey(dep)
	if dep.Reference != "" {
		return key + "@" + dep.Reference
	}
	return key
}

// findTightestParent returns the unique key of the parent that contributed
// the tightest (most restrictive) constraint. For semver ranges, "tightest"
// means the highest lower bound. Falls back to deepest constraint if
// lower bounds can't be compared.
// Returns "" for direct deps (depth 1, no parent).
func findTightestParent(constraints []ConstraintEntry) string {
	if len(constraints) == 0 {
		return ""
	}
	if len(constraints) == 1 {
		return extractParentKey(constraints[0])
	}

	best := constraints[0]
	for _, ce := range constraints[1:] {
		if isTighterConstraint(ce.Constraint, best.Constraint) {
			best = ce
		}
	}
	return extractParentKey(best)
}

// extractParentKey gets the parent's unique key from the chain.
// Chain format: ["acme/a@^1.0.0", "acme/b@^2.0.0"]
// The parent is the second-to-last entry; if chain has only 1 entry, it's a direct dep.
func extractParentKey(ce ConstraintEntry) string {
	if len(ce.Chain) < 2 {
		return "" // direct dep
	}
	parentEntry := ce.Chain[len(ce.Chain)-2]
	// Strip @constraint suffix to get bare key
	if idx := strings.Index(parentEntry, "@"); idx >= 0 {
		return parentEntry[:idx]
	}
	return parentEntry
}

// isTighterConstraint returns true if a is tighter than b.
// Uses semver lower bound comparison when possible.
func isTighterConstraint(a, b string) bool {
	if a == "" || b == "" {
		return a != ""
	}
	// Try to extract lower bounds from the constraints
	aLo := extractLowerBound(a)
	bLo := extractLowerBound(b)
	if aLo != "" && bLo != "" {
		cmp := semver.CompareVersions(aLo, bLo)
		return cmp > 0 // higher lower bound = tighter
	}
	// Fallback: lexicographic (deterministic)
	return a > b
}

// extractLowerBound attempts to get the lower bound version from a range.
// For "^1.5.0" returns "1.5.0", for "~1.2.3" returns "1.2.3",
// for ">=1.0.0 <2.0.0" returns "1.0.0".
func extractLowerBound(constraint string) string {
	c := strings.TrimSpace(constraint)
	if c == "" || c == "*" {
		return ""
	}
	// Strip prefix operators
	for _, prefix := range []string{"^", "~", ">=", ">", "="} {
		if strings.HasPrefix(c, prefix) {
			ver := strings.TrimSpace(c[len(prefix):])
			// Take first space-delimited token (for ">=1.0.0 <2.0.0")
			if idx := strings.IndexByte(ver, ' '); idx >= 0 {
				ver = ver[:idx]
			}
			return ver
		}
	}
	// Bare version
	return c
}
