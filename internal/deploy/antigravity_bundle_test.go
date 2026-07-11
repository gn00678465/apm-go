package deploy

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// TestRun_AntigravityBundlePaths locks the dependency-primitive bundle
// contract (design.md §4.1, PRD AC1): all four antigravity-supported
// primitive types for a dependency land under
// .agents/plugins/<pkg>/... instead of the flat paths local primitives use.
func TestRun_AntigravityBundlePaths(t *testing.T) {
	dir := t.TempDir()
	const (
		ruleContent   = "a-rule\n"
		agentContent  = "a-agent\n"
		skillContent  = "a-skill\n"
		helperContent = "a-helper\n"
		hookContent   = `{"a-hook":{"Stop":[]}}`
	)
	depDir := filepath.Join(dir, "apm_modules", "acme", "tool")
	mkFile(t, depDir, ".apm/instructions/a.instructions.md", ruleContent)
	mkFile(t, depDir, ".apm/agents/a.agent.md", agentContent)
	mkFile(t, depDir, ".apm/skills/a-skill/SKILL.md", skillContent)
	mkFile(t, depDir, ".apm/skills/a-skill/scripts/helper.txt", helperContent)
	mkFile(t, depDir, ".apm/hooks/a.json", hookContent)

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/tool", Owner: "acme", Repo: "tool", Source: "git"},
		},
	}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	pairs := map[string]string{
		".agents/plugins/tool/rules/a.md":                        ruleContent,
		".agents/plugins/tool/agents/a/agent.md":                 agentContent,
		".agents/plugins/tool/skills/a-skill/SKILL.md":           skillContent,
		".agents/plugins/tool/skills/a-skill/scripts/helper.txt": helperContent,
		".agents/plugins/tool/hooks.json":                        hookContent,
	}
	for rel, want := range pairs {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Errorf("expected bundle file %s: %v", rel, err)
			continue
		}
		if string(got) != want {
			t.Errorf("%s content = %q, want %q", rel, got, want)
		}
	}

	dr := result.PerDep["acme/tool"]
	if dr == nil {
		t.Fatal("expected PerDep entry for acme/tool")
	}
	for rel := range pairs {
		if !slices.Contains(dr.Files, rel) {
			t.Errorf("expected %s recorded under acme/tool, got %+v", rel, dr.Files)
		}
	}
}

// TestRun_AntigravityLocalPathsUnchanged locks design.md D1: local (project-
// owned) primitives keep the pre-existing flat antigravity paths and never
// produce a .agents/plugins/ directory.
func TestRun_AntigravityLocalPathsUnchanged(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, dir, ".apm/instructions/local.instructions.md", "local-rule\n")
	mkFile(t, dir, ".apm/agents/local.agent.md", "local-agent\n")
	mkFile(t, dir, ".apm/skills/local-skill/SKILL.md", "local-skill\n")
	mkFile(t, dir, ".apm/hooks/local.json", `{"local-hook":{"Stop":[]}}`)

	m := &manifest.Manifest{Name: "test", Version: "1.0.0"}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	for _, rel := range []string{
		".agents/rules/local.md",
		".agents/agents/local/agent.md",
		".agents/skills/local-skill/SKILL.md",
		".agents/hooks.json",
	} {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
			t.Errorf("expected local flat path %s: %v", rel, err)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins")); !os.IsNotExist(err) {
		t.Errorf("expected no .agents/plugins for a local-only install, stat err=%v", err)
	}

	if dr := result.PerDep[""]; dr == nil {
		t.Fatal("expected local PerDep entry")
	}
}

// TestRun_AntigravityPluginManifestProvenance locks design.md D5/R3: the
// minimal, byte-deterministic plugin.json manifest is written for a bundled
// dependency and recorded in that dependency's PerDep Files/Hashes.
func TestRun_AntigravityPluginManifestProvenance(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "tool"), ".apm/hooks/a.json", `{"a-hook":{"Stop":[]}}`)

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/tool", Owner: "acme", Repo: "tool", Source: "git"},
		},
	}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	manifestRel := ".agents/plugins/tool/plugin.json"
	body, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(manifestRel)))
	if err != nil {
		t.Fatalf("expected plugin manifest: %v", err)
	}
	if want := "{\"name\": \"tool\"}\n"; string(body) != want {
		t.Errorf("plugin.json = %q, want %q", body, want)
	}

	dr := result.PerDep["acme/tool"]
	if dr == nil || !slices.Contains(dr.Files, manifestRel) {
		t.Errorf("expected %s recorded under acme/tool, got %+v", manifestRel, dr)
	}
	if dr == nil || dr.Hashes[manifestRel] == "" {
		t.Errorf("expected a hash recorded for %s", manifestRel)
	}
}

// TestRun_AntigravityPluginManifestReinstall locks design.md R3: a second
// Run() (simulating a re-install) rewrites and re-reports the same
// plugin.json bytes, so provenance never drops it.
func TestRun_AntigravityPluginManifestReinstall(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "tool"), ".apm/hooks/a.json", `{"a-hook":{"Stop":[]}}`)

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/tool", Owner: "acme", Repo: "tool", Source: "git"},
		},
	}

	manifestRel := ".agents/plugins/tool/plugin.json"

	first, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("first Run: %v", err)
	}
	firstHash := first.PerDep["acme/tool"].Hashes[manifestRel]
	if firstHash == "" {
		t.Fatal("expected manifest hash after first Run")
	}

	second, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("second Run: %v", err)
	}
	dr := second.PerDep["acme/tool"]
	if dr == nil || !slices.Contains(dr.Files, manifestRel) {
		t.Fatalf("expected %s still recorded after re-install, got %+v", manifestRel, dr)
	}
	if dr.Hashes[manifestRel] != firstHash {
		t.Errorf("expected manifest bytes stable across re-install: first=%s second=%s", firstHash, dr.Hashes[manifestRel])
	}
}

// TestBundleNameFromDepKey locks the DepKey -> bundle-name sanitization
// contract (design.md D4): only the last "/"-segment is used, and the
// result can never be empty, ".", "..", hidden, or contain a path
// separator -- archive.ContainedKey's containment guarantee must survive
// composition with this function.
func TestBundleNameFromDepKey(t *testing.T) {
	tests := []struct {
		name   string
		depKey string
		want   string
	}{
		{"repo-owner slash form", "acme/tool", "tool"},
		{"no slash", "tool", "tool"},
		{"materialized local-path dep", "_local/dep-a-1a2b3c4d", "dep-a-1a2b3c4d"},
		{"empty", "", "pkg-"},
		{"dot", ".", "pkg-"},
		{"dotdot", "..", "pkg-"},
		{"trailing slash", "acme/", "pkg-"},
		{"root slash only", "/", "pkg-"},
		{"embedded dotdot segment", "acme/..", "pkg-"},
		{"backslash only", `\`, "-"},
		{"drive letter colon", "C:", "C-"},
		{"unsafe chars", "acme/tool name!", "tool-name-"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bundleNameFromDepKey(tt.depKey)
			if got != tt.want {
				t.Errorf("bundleNameFromDepKey(%q) = %q, want %q", tt.depKey, got, tt.want)
			}
			if got == "" || got == "." || got == ".." || strings.HasPrefix(got, ".") {
				t.Errorf("bundleNameFromDepKey(%q) = %q is not a safe single path segment", tt.depKey, got)
			}
			if strings.ContainsAny(got, "/\\") {
				t.Errorf("bundleNameFromDepKey(%q) = %q contains a path separator", tt.depKey, got)
			}
		})
	}
}

// TestAntigravityBundleDir locks that every bundle directory stays a single
// segment rooted at .agents/plugins/, for both a normal and a pathological
// DepKey.
func TestAntigravityBundleDir(t *testing.T) {
	tests := []struct {
		depKey string
		want   string
	}{
		{"acme/tool", ".agents/plugins/tool"},
		{"", ".agents/plugins/pkg-"},
		{"acme/../../etc", ".agents/plugins/etc"},
	}
	for _, tt := range tests {
		got := antigravityBundleDir(tt.depKey)
		if got != tt.want {
			t.Errorf("antigravityBundleDir(%q) = %q, want %q", tt.depKey, got, tt.want)
		}
		if !strings.HasPrefix(got, antigravityBundleRoot+"/") {
			t.Errorf("antigravityBundleDir(%q) = %q escapes %s", tt.depKey, got, antigravityBundleRoot)
		}
		if strings.Contains(got, "..") {
			t.Errorf("antigravityBundleDir(%q) = %q contains a traversal segment", tt.depKey, got)
		}
	}
}

// TestRun_AntigravityBundleNameCollision locks design.md R4's fail-closed
// resolution: two dependencies whose DepKey sanitizes to the same bundle
// name must abort Run() with nothing written for either, rather than mix
// their files into one directory.
func TestRun_AntigravityBundleNameCollision(t *testing.T) {
	dir := t.TempDir()
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "tool"), ".apm/hooks/a.json", `{"a-hook":{"Stop":[]}}`)
	mkFile(t, filepath.Join(dir, "apm_modules", "other-org", "tool"), ".apm/hooks/b.json", `{"b-hook":{"Stop":[]}}`)

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/tool", Owner: "acme", Repo: "tool", Source: "git"},
			{RepoURL: "other-org/tool", Owner: "other-org", Repo: "tool", Source: "git"},
		},
	}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err == nil {
		t.Fatal("expected a bundle name collision error")
	}
	if result != nil {
		t.Errorf("expected nil result on fail-closed collision, got %+v", result)
	}
	if !strings.Contains(err.Error(), "acme/tool") || !strings.Contains(err.Error(), "other-org/tool") {
		t.Errorf("expected error to name both colliding dependencies, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".agents", "plugins")); !os.IsNotExist(statErr) {
		t.Errorf("expected no bundle files written before the collision was caught, stat err=%v", statErr)
	}
}

// TestRun_AntigravityTwoDependencyHooksIsolated locks PRD AC2: two
// dependencies each shipping a hook file get two separate hooks.json files,
// neither overwriting the other.
func TestRun_AntigravityTwoDependencyHooksIsolated(t *testing.T) {
	dir := t.TempDir()
	const aHook = `{"a-hook":{"Stop":[]}}`
	const bHook = `{"b-hook":{"Stop":[]}}`
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "dep-a"), ".apm/hooks/a.json", aHook)
	mkFile(t, filepath.Join(dir, "apm_modules", "acme", "dep-b"), ".apm/hooks/b.json", bHook)

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/dep-a", Owner: "acme", Repo: "dep-a", Source: "git"},
			{RepoURL: "acme/dep-b", Owner: "acme", Repo: "dep-b", Source: "git"},
		},
	}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	aBody, err := os.ReadFile(filepath.Join(dir, ".agents", "plugins", "dep-a", "hooks.json"))
	if err != nil {
		t.Fatalf("expected dep-a hooks.json: %v", err)
	}
	if string(aBody) != aHook {
		t.Errorf("dep-a hooks.json = %q, want %q", aBody, aHook)
	}
	bBody, err := os.ReadFile(filepath.Join(dir, ".agents", "plugins", "dep-b", "hooks.json"))
	if err != nil {
		t.Fatalf("expected dep-b hooks.json: %v", err)
	}
	if string(bBody) != bHook {
		t.Errorf("dep-b hooks.json = %q, want %q", bBody, bHook)
	}

	for _, d := range result.Diags {
		if strings.Contains(d, "overwrites") {
			t.Errorf("expected no cross-dependency overwrite diagnostic, got %v", result.Diags)
		}
	}
}

// TestRun_AntigravitySameDependencyHooksOverwriteDiagnostic locks design.md
// D6: two hook files WITHIN the same dependency still collapse to that
// dependency's single bundle hooks.json, with the pre-existing overwrite
// diagnostic -- the residual "same-package" gap the plugin bundle route
// does not (and per PRD AC2's cross-package scope, need not) eliminate.
func TestRun_AntigravitySameDependencyHooksOverwriteDiagnostic(t *testing.T) {
	dir := t.TempDir()
	depDir := filepath.Join(dir, "apm_modules", "acme", "dep-a")
	mkFile(t, depDir, ".apm/hooks/pre.json", `{"event":"pre"}`)
	mkFile(t, depDir, ".apm/hooks/post.json", `{"event":"post"}`)

	m := &manifest.Manifest{
		Name:    "test",
		Version: "1.0.0",
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/dep-a", Owner: "acme", Repo: "dep-a", Source: "git"},
		},
	}

	result, err := Run([]string{"antigravity"}, dir, m, nil, nil)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	hooksPath := filepath.Join(dir, ".agents", "plugins", "dep-a", "hooks.json")
	if _, err := os.Stat(hooksPath); err != nil {
		t.Fatalf("expected a single hooks.json for dep-a: %v", err)
	}

	found := false
	for _, d := range result.Diags {
		if strings.Contains(d, "overwrites") && strings.Contains(d, ".agents/plugins/dep-a/hooks.json") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected overwrite diagnostic for dep-a's colliding hooks, got %v", result.Diags)
	}
}
