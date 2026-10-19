package deploy

import "fmt"

type conflictKey struct {
	Name string
	Type PrimitiveType
}

func nameKey(p Primitive) conflictKey { return conflictKey{Name: p.Name, Type: p.Type} }

// targetKey is the conflict key of target: the name, but for a copilot hook
// the file it writes, <package>-<stem>.json (integration/hook_integrator.py:
// 1065-1067), so hooks of one name from two packages do not compete there.
func targetKey(target string) func(Primitive) conflictKey {
	if target != "copilot" {
		return nameKey
	}
	return func(p Primitive) conflictKey {
		if p.Type == TypeHooks {
			return conflictKey{Name: copilotHookFile(p), Type: p.Type}
		}
		return nameKey(p)
	}
}

// copilotOnlyHookTarget reports whether copilot is the one target of targets
// that deploys hooks.
func copilotOnlyHookTarget(targets []string) bool {
	copilot := false
	for _, t := range targets {
		if a, ok := Adapters[t]; ok && adapterSupports(a, TypeHooks) {
			if t != "copilot" {
				return false
			}
			copilot = true
		}
	}
	return copilot
}

// ResolvePrimitives applies req-pr-002 (local wins) and req-pr-003 (first-declared wins).
// Input must be ordered: locals first, then deps in manifest declaration order,
// then transitive in lockfile sorted order (repo_url, virtual_path).
func ResolvePrimitives(primitives []Primitive) ([]Primitive, []string) {
	return resolvePrimitivesBy(primitives, nameKey)
}

func resolvePrimitivesBy(primitives []Primitive, keyOf func(Primitive) conflictKey) ([]Primitive, []string) {
	winners := make(map[conflictKey]Primitive)
	var order []conflictKey
	var diags []string

	for _, p := range primitives {
		key := keyOf(p)
		existing, exists := winners[key]
		if !exists {
			winners[key] = p
			order = append(order, key)
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

	result := make([]Primitive, 0, len(order))
	for _, key := range order {
		result = append(result, winners[key])
	}
	return result, diags
}
