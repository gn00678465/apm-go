package deploy

import (
	"strings"
	"testing"
)

func TestResolvePrimitives_LocalOverridesDep(t *testing.T) {
	// req-pr-002: local primitive overrides dependency of same (name, type)
	prims := []Primitive{
		{Name: "demo", Type: TypeInstructions, Source: "dependency:acme/foo", DepKey: "acme/foo"},
		{Name: "demo", Type: TypeInstructions, Source: "local", DepKey: ""},
	}

	winners, diags := ResolvePrimitives(prims)

	if len(winners) != 1 {
		t.Fatalf("expected 1 winner, got %d", len(winners))
	}
	if winners[0].Source != "local" {
		t.Errorf("expected local to win, got %q", winners[0].Source)
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if !strings.Contains(diags[0], "overrides") {
		t.Errorf("diagnostic should mention override: %q", diags[0])
	}
}

func TestResolvePrimitives_FirstDeclaredWins(t *testing.T) {
	// req-pr-003: first-declared dependency wins
	prims := []Primitive{
		{Name: "helper", Type: TypeAgents, Source: "dependency:acme/foo", DepKey: "acme/foo"},
		{Name: "helper", Type: TypeAgents, Source: "dependency:acme/bar", DepKey: "acme/bar"},
	}

	winners, diags := ResolvePrimitives(prims)

	if len(winners) != 1 {
		t.Fatalf("expected 1, got %d", len(winners))
	}
	if winners[0].DepKey != "acme/foo" {
		t.Errorf("expected first dep to win, got %q", winners[0].DepKey)
	}
	if len(diags) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diags))
	}
	if !strings.Contains(diags[0], "shadowed") {
		t.Errorf("diagnostic should mention shadow: %q", diags[0])
	}
}

func TestResolvePrimitives_DifferentTypes_NoCOnflict(t *testing.T) {
	prims := []Primitive{
		{Name: "demo", Type: TypeInstructions, Source: "local"},
		{Name: "demo", Type: TypeSkills, Source: "dependency:acme/foo", DepKey: "acme/foo"},
	}

	winners, diags := ResolvePrimitives(prims)

	if len(winners) != 2 {
		t.Fatalf("expected 2 (different types), got %d", len(winners))
	}
	if len(diags) != 0 {
		t.Errorf("expected no conflicts, got %v", diags)
	}
}

func TestResolvePrimitives_LocalFirst_DepShadowed(t *testing.T) {
	// Local already in map, dependency comes later → shadowed
	prims := []Primitive{
		{Name: "demo", Type: TypeSkills, Source: "local"},
		{Name: "demo", Type: TypeSkills, Source: "dependency:acme/foo", DepKey: "acme/foo"},
	}

	winners, diags := ResolvePrimitives(prims)

	if len(winners) != 1 {
		t.Fatalf("expected 1, got %d", len(winners))
	}
	if winners[0].Source != "local" {
		t.Errorf("local should win")
	}
	if len(diags) != 1 || !strings.Contains(diags[0], "shadowed by local") {
		t.Errorf("expected shadow diagnostic, got %v", diags)
	}
}

func TestResolvePrimitives_NoConflicts(t *testing.T) {
	prims := []Primitive{
		{Name: "a", Type: TypeInstructions, Source: "local"},
		{Name: "b", Type: TypeAgents, Source: "dependency:x/y", DepKey: "x/y"},
		{Name: "c", Type: TypeSkills, Source: "dependency:x/z", DepKey: "x/z"},
	}

	winners, diags := ResolvePrimitives(prims)

	if len(winners) != 3 {
		t.Fatalf("expected 3, got %d", len(winners))
	}
	if len(diags) != 0 {
		t.Errorf("expected no conflicts, got %v", diags)
	}
}
