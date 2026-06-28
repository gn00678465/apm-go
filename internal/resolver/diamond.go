package resolver

import (
	"fmt"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/semver"
)

// pickHighestInIntersection evaluates all semver constraints for a package
// and returns the highest tag satisfying ALL of them.
// Returns empty TagInfo and an error diagnostic if the intersection is empty.
func pickHighestInIntersection(constraints []ConstraintEntry, tags []semver.TagInfo) (semver.TagInfo, error) {
	if len(constraints) == 0 {
		return semver.TagInfo{}, fmt.Errorf("no constraints")
	}

	// Filter tags: must satisfy ALL constraints
	var candidates []semver.TagInfo
	for _, tag := range tags {
		ver := semver.StripVPrefix(tag.Name)
		matchesAll := true
		for _, ce := range constraints {
			ok, err := semver.Satisfies(ver, ce.Constraint)
			if err != nil || !ok {
				matchesAll = false
				break
			}
		}
		if matchesAll {
			candidates = append(candidates, tag)
		}
	}

	if len(candidates) == 0 {
		return semver.TagInfo{}, fmt.Errorf("empty intersection")
	}

	// Sort ascending, pick last (highest)
	sort.Slice(candidates, func(i, j int) bool {
		vi := semver.StripVPrefix(candidates[i].Name)
		vj := semver.StripVPrefix(candidates[j].Name)
		cmp := semver.CompareVersions(vi, vj)
		if cmp != 0 {
			return cmp < 0
		}
		// req-rs-014: build-metadata tie-break
		return candidates[i].Name < candidates[j].Name
	})

	return candidates[len(candidates)-1], nil
}

// checkLiteralConflict checks if all literal constraints for a package agree.
// For git-literal deps, all refs must be identical strings.
func checkLiteralConflict(constraints []ConstraintEntry) (string, error) {
	if len(constraints) == 0 {
		return "", nil
	}
	ref := constraints[0].Constraint
	for _, ce := range constraints[1:] {
		if ce.Constraint != ref {
			return "", fmt.Errorf("conflicting literal refs: %q vs %q", ref, ce.Constraint)
		}
	}
	return ref, nil
}

// formatConflictDiagnostic formats the empty-intersection diagnostic per req-rs-010.
// Each chain: <owner>/<repo>@<constraint> -> <owner>/<repo>@<constraint>
func formatConflictDiagnostic(key string, constraints []ConstraintEntry) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("dependency conflict for %s: no version satisfies all constraints\n", key))

	// Sort chains deterministically for output
	sorted := make([]ConstraintEntry, len(constraints))
	copy(sorted, constraints)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.Join(sorted[i].Chain, " -> ") < strings.Join(sorted[j].Chain, " -> ")
	})

	for i, ce := range sorted {
		sb.WriteString(fmt.Sprintf("  chain %d: %s", i+1, strings.Join(ce.Chain, " -> ")))
		if i < len(sorted)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

