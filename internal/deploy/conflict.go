package deploy

import "fmt"

type conflictKey struct {
	Name string
	Type PrimitiveType
}

// ResolvePrimitives applies req-pr-002 (local wins) and req-pr-003 (first-declared wins).
// Input must be ordered: locals first, then deps in manifest declaration order,
// then transitive in lockfile sorted order (repo_url, virtual_path).
func ResolvePrimitives(primitives []Primitive) ([]Primitive, []string) {
	winners := make(map[conflictKey]Primitive)
	var diags []string

	for _, p := range primitives {
		key := conflictKey{Name: p.Name, Type: p.Type}
		existing, exists := winners[key]
		if !exists {
			winners[key] = p
			continue
		}

		isLocalNew := p.Source == "local"
		isLocalExisting := existing.Source == "local"

		if isLocalNew && !isLocalExisting {
			// req-pr-002: local overrides dependency
			winners[key] = p
			diags = append(diags, fmt.Sprintf(
				"local %s %q overrides %s", p.Type, p.Name, existing.Source))
		} else if !isLocalNew && isLocalExisting {
			// dependency can't override local
			diags = append(diags, fmt.Sprintf(
				"%s %q from %s shadowed by local", p.Type, p.Name, p.Source))
		} else {
			// req-pr-003: same source class → first-declared wins
			diags = append(diags, fmt.Sprintf(
				"%s %q from %s shadowed by %s (first-declared wins)",
				p.Type, p.Name, p.Source, existing.Source))
		}
	}

	var result []Primitive
	for _, p := range winners {
		result = append(result, p)
	}
	return result, diags
}
