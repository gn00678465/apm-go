package manifest

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// ── ParseDepDict: mkt-033 dict-form marketplace dependencies ──

func TestParseDepDict_Marketplace_Valid(t *testing.T) {
	// Arrange
	entry := buildMappingNode(map[string]string{
		"name":        "awesome-skill",
		"marketplace": "acme-mkt",
	})

	// Act
	d, err := ParseDepDict(entry, 0)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Source != "marketplace" {
		t.Errorf("Source = %q, want %q", d.Source, "marketplace")
	}
	if d.MarketplacePluginName != "awesome-skill" {
		t.Errorf("MarketplacePluginName = %q, want %q", d.MarketplacePluginName, "awesome-skill")
	}
	if d.MarketplaceName != "acme-mkt" {
		t.Errorf("MarketplaceName = %q, want %q", d.MarketplaceName, "acme-mkt")
	}
	if d.MarketplaceVersionSpec != "" {
		t.Errorf("MarketplaceVersionSpec = %q, want empty", d.MarketplaceVersionSpec)
	}
	// RepoURL placeholder (mkt-033) gives the unresolved entry a stable
	// dedup identity distinct from any other unresolved marketplace entry.
	if want := "_marketplace/acme-mkt/awesome-skill"; d.RepoURL != want {
		t.Errorf("RepoURL = %q, want %q", d.RepoURL, want)
	}
}

// TestParseDepDict_Marketplace_NotShadowedByNameBranch locks the mkt-033
// branch-order requirement: the pre-existing `keys["name"]` branch a few
// lines below the marketplace branch would otherwise swallow this exact
// entry shape as a plain git-literal RepoURL=="awesome-skill" (losing the
// marketplace/version fields entirely and never setting Source). This is
// the negative test (f) in implement.md's Phase V list for this step.
func TestParseDepDict_Marketplace_NotShadowedByNameBranch(t *testing.T) {
	// Arrange
	entry := buildMappingNode(map[string]string{
		"name":        "awesome-skill",
		"marketplace": "acme-mkt",
	})

	// Act
	d, err := ParseDepDict(entry, 0)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.RepoURL == "awesome-skill" {
		t.Fatalf("entry was shadowed by the git-literal 'name' branch: RepoURL = %q, Source = %q", d.RepoURL, d.Source)
	}
	if d.Source != "marketplace" {
		t.Errorf("Source = %q, want %q (entry shadowed by a different branch)", d.Source, "marketplace")
	}
	if d.Alias != "" {
		t.Errorf("Alias = %q, want empty (only the name-branch sets Alias)", d.Alias)
	}
}

// TestParseDepDict_Marketplace_WithVersion covers negative test (b): a
// semver-range-shaped `version:` value parses without any format/range
// validation at parse time (range legality is deferred to resolve time).
func TestParseDepDict_Marketplace_WithVersion(t *testing.T) {
	// Arrange
	entry := buildMappingNode(map[string]string{
		"name":        "awesome-skill",
		"marketplace": "acme-mkt",
		"version":     "~1.2.0",
	})

	// Act
	d, err := ParseDepDict(entry, 0)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.MarketplaceVersionSpec != "~1.2.0" {
		t.Errorf("MarketplaceVersionSpec = %q, want %q", d.MarketplaceVersionSpec, "~1.2.0")
	}
}

func TestParseDepDict_Marketplace_CasePreserved(t *testing.T) {
	// Arrange
	entry := buildMappingNode(map[string]string{
		"name":        "AwesomeSkill",
		"marketplace": "AcmeMkt",
	})

	// Act
	d, err := ParseDepDict(entry, 0)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.MarketplacePluginName != "AwesomeSkill" {
		t.Errorf("MarketplacePluginName = %q, want case preserved %q", d.MarketplacePluginName, "AwesomeSkill")
	}
	if d.MarketplaceName != "AcmeMkt" {
		t.Errorf("MarketplaceName = %q, want case preserved %q", d.MarketplaceName, "AcmeMkt")
	}
	if want := "_marketplace/AcmeMkt/AwesomeSkill"; d.RepoURL != want {
		t.Errorf("RepoURL = %q, want %q", d.RepoURL, want)
	}
}

// TestParseDepDict_Marketplace_AmbiguousKeys covers negative test (c):
// `marketplace` combined with any of git/path/registry/id is rejected.
func TestParseDepDict_Marketplace_AmbiguousKeys(t *testing.T) {
	tests := []struct {
		name  string
		extra map[string]string
	}{
		{"git", map[string]string{"git": "acme/other"}},
		{"path", map[string]string{"path": "./local"}},
		{"registry", map[string]string{"registry": "my-registry"}},
		{"id", map[string]string{"id": "acme/other"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			kv := map[string]string{"name": "awesome-skill", "marketplace": "acme-mkt"}
			for k, v := range tt.extra {
				kv[k] = v
			}
			entry := buildMappingNode(kv)

			// Act
			_, err := ParseDepDict(entry, 0)

			// Assert
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "Ambiguous") {
				t.Errorf("error %q should mention Ambiguous dependency", err.Error())
			}
			if !strings.Contains(err.Error(), "marketplace") {
				t.Errorf("error %q should mention marketplace", err.Error())
			}
		})
	}
}

// TestParseDepDict_Marketplace_UnknownKey covers negative test (d): the
// allowed key set for a marketplace dict entry is exactly
// {name, marketplace, version} -- anything else (including `alias`, which
// IS allowed on the git/path/id branches) is rejected.
func TestParseDepDict_Marketplace_UnknownKey(t *testing.T) {
	tests := []struct {
		name       string
		extraKey   string
		extraValue string
	}{
		{"alias not allowed here", "alias", "my-alias"},
		{"arbitrary unknown key", "foo", "bar"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			entry := buildMappingNode(map[string]string{
				"name":        "awesome-skill",
				"marketplace": "acme-mkt",
				tt.extraKey:   tt.extraValue,
			})

			// Act
			_, err := ParseDepDict(entry, 0)

			// Assert
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "unknown key") {
				t.Errorf("error %q should mention unknown key", err.Error())
			}
			if !strings.Contains(err.Error(), tt.extraKey) {
				t.Errorf("error %q should name the offending key %q", err.Error(), tt.extraKey)
			}
		})
	}
}

// TestParseDepDict_Marketplace_NameMissingOrEmpty covers negative test (g):
// `name` is required and gets a dedicated error message, checked before the
// character-set regex validation.
func TestParseDepDict_Marketplace_NameMissingOrEmpty(t *testing.T) {
	tests := []struct {
		name string
		kv   map[string]string
	}{
		{"name key absent", map[string]string{"marketplace": "acme-mkt"}},
		{"name empty string", map[string]string{"name": "", "marketplace": "acme-mkt"}},
		{"name all whitespace", map[string]string{"name": "   ", "marketplace": "acme-mkt"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			entry := buildMappingNode(tt.kv)

			// Act
			_, err := ParseDepDict(entry, 0)

			// Assert
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "Marketplace dependency must have a non-empty 'name' field") {
				t.Errorf("error %q should carry the dedicated name-required message", err.Error())
			}
		})
	}
}

func TestParseDepDict_Marketplace_InvalidNameOrMarketplaceChars(t *testing.T) {
	tests := []struct {
		name    string
		kv      map[string]string
		wantSub string
	}{
		{"invalid name chars", map[string]string{"name": "foo bar", "marketplace": "acme-mkt"}, "invalid marketplace plugin name"},
		{"invalid marketplace chars", map[string]string{"name": "foo", "marketplace": "acme mkt"}, "invalid marketplace name"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			entry := buildMappingNode(tt.kv)

			// Act
			_, err := ParseDepDict(entry, 0)

			// Assert
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q should contain %q", err.Error(), tt.wantSub)
			}
		})
	}
}

func TestParseDepDict_Marketplace_EmptyVersionRejected(t *testing.T) {
	// Arrange
	entry := buildMappingNode(map[string]string{
		"name":        "awesome-skill",
		"marketplace": "acme-mkt",
		"version":     "   ",
	})

	// Act
	_, err := ParseDepDict(entry, 0)

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "non-empty string") {
		t.Errorf("error %q should mention non-empty string", err.Error())
	}
}

// ── mkt-030: serialization guard ──

func TestValidateResolved(t *testing.T) {
	tests := []struct {
		name    string
		dep     DependencyReference
		wantErr bool
	}{
		{
			"unresolved marketplace ref rejected",
			DependencyReference{Source: "marketplace", MarketplaceName: "acme-mkt", MarketplacePluginName: "awesome-skill"},
			true,
		},
		{"resolved git ref allowed", DependencyReference{Source: "git", RepoURL: "owner/repo"}, false},
		{"local ref allowed", DependencyReference{Source: "local", IsLocal: true, LocalPath: "./foo"}, false},
		{"registry ref allowed", DependencyReference{Source: "registry", RepoURL: "acme/foo"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			err := tt.dep.ValidateResolved()

			// Assert
			if tt.wantErr && err == nil {
				t.Fatal("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantErr && !strings.Contains(err.Error(), "unresolved marketplace dependency") {
				t.Errorf("error %q should mention unresolved marketplace dependency", err.Error())
			}
		})
	}
}

// ── Phase V fixture: "already existing, hand-formatted" apm.yml (舊坑 1) ──

// handAuthoredApmYMLWithMarketplaceDep mirrors the project's established
// "舊坑 1" fixture convention (see cmd/apm-go/marketplace_authoring_test.go's
// handAuthoredApmYML): unusual spacing, an inline comment on the very line
// under test, and unrelated surrounding content -- proving the parser
// handles a dict marketplace entry embedded in a realistic, not
// freshly-generated file, not just a synthetic in-memory yaml.Node.
const handAuthoredApmYMLWithMarketplaceDep = "# Hand-authored project manifest\n" +
	"name:    demo-project\n" +
	"version: \"1.0.0\"\n" +
	"description: >-\n" +
	"  A project with a marketplace dependency mixed in\n" +
	"  among ordinary git dependencies.\n" +
	"dependencies:\n" +
	"  apm:\n" +
	"    - acme/other-pkg\n" +
	"    - name: awesome-skill\n" +
	"      marketplace: acme-mkt\n" +
	"      version: \"~1.2.0\"   # inline comment kept exactly\n" +
	"scripts:\n" +
	"  build: echo hi   # keep me\n"

func TestParseManifest_HandFormattedApmYMLWithMarketplaceDictEntry(t *testing.T) {
	// Arrange
	node, err := yamlcore.SafeLoad([]byte(handAuthoredApmYMLWithMarketplaceDep))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	// Act
	m, diags, err := ParseManifest(node)

	// Assert
	if err != nil {
		t.Fatalf("ParseManifest returned error: %v", err)
	}
	for _, d := range diags {
		if d.Level == LevelError {
			t.Errorf("unexpected error diagnostic: %s", d.Message)
		}
	}
	if len(m.ParsedDeps) != 2 {
		t.Fatalf("ParsedDeps = %d entries, want 2: %+v", len(m.ParsedDeps), m.ParsedDeps)
	}

	gitDep := m.ParsedDeps[0]
	if gitDep.Source != "git" || gitDep.RepoURL != "acme/other-pkg" {
		t.Errorf("ParsedDeps[0] = %+v, want git acme/other-pkg", gitDep)
	}

	mktDep := m.ParsedDeps[1]
	if mktDep.Source != "marketplace" {
		t.Fatalf("ParsedDeps[1].Source = %q, want %q (entry shadowed by a different branch): %+v", mktDep.Source, "marketplace", mktDep)
	}
	if mktDep.MarketplacePluginName != "awesome-skill" {
		t.Errorf("MarketplacePluginName = %q, want %q", mktDep.MarketplacePluginName, "awesome-skill")
	}
	if mktDep.MarketplaceName != "acme-mkt" {
		t.Errorf("MarketplaceName = %q, want %q", mktDep.MarketplaceName, "acme-mkt")
	}
	if mktDep.MarketplaceVersionSpec != "~1.2.0" {
		t.Errorf("MarketplaceVersionSpec = %q, want %q", mktDep.MarketplaceVersionSpec, "~1.2.0")
	}
	if want := "_marketplace/acme-mkt/awesome-skill"; mktDep.RepoURL != want {
		t.Errorf("RepoURL = %q, want %q", mktDep.RepoURL, want)
	}

	// mkt-030: this parsed-but-unresolved entry must still be rejected by
	// the serialization guard -- ParseDepDict alone never resolves it.
	if err := mktDep.ValidateResolved(); err == nil {
		t.Error("expected ValidateResolved to reject the still-unresolved marketplace entry")
	}
}
