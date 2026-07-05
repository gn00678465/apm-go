package lockfile

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// TestParseLockedDep_MarketplaceProvenanceFields covers mkt-031's parse-side
// list (parse.go's switch): all four provenance fields round-trip through
// ParseLockfile.
func TestParseLockedDep_MarketplaceProvenanceFields(t *testing.T) {
	// Arrange
	yamlSrc := "lockfile_version: \"1\"\n" +
		"dependencies:\n" +
		"  - repo_url: github.com/acme/foo\n" +
		"    source: git\n" +
		"    resolved_ref: v1.0.0\n" +
		"    discovered_via: acme-marketplace\n" +
		"    marketplace_plugin_name: Foo-Plugin\n" +
		"    source_url: https://example.com/marketplace.json\n" +
		"    source_digest: sha256:deadbeef\n"
	node, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}

	// Act
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}

	// Assert
	if len(lf.Dependencies) != 1 {
		t.Fatalf("deps count = %d, want 1", len(lf.Dependencies))
	}
	dep := lf.Dependencies[0]
	if dep.DiscoveredVia != "acme-marketplace" {
		t.Errorf("DiscoveredVia = %q, want %q", dep.DiscoveredVia, "acme-marketplace")
	}
	if dep.MarketplacePluginName != "Foo-Plugin" {
		t.Errorf("MarketplacePluginName = %q, want %q", dep.MarketplacePluginName, "Foo-Plugin")
	}
	if dep.SourceURL != "https://example.com/marketplace.json" {
		t.Errorf("SourceURL = %q, want %q", dep.SourceURL, "https://example.com/marketplace.json")
	}
	if dep.SourceDigest != "sha256:deadbeef" {
		t.Errorf("SourceDigest = %q, want %q", dep.SourceDigest, "sha256:deadbeef")
	}
}

// entryKeys/entryValue read a serialized mapping-node dependency entry's
// keys/values for assertion purposes.
func entryKeys(node *yaml.Node) map[string]bool {
	keys := make(map[string]bool)
	if node == nil || node.Kind != yaml.MappingNode {
		return keys
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		keys[node.Content[i].Value] = true
	}
	return keys
}

func entryValue(node *yaml.Node, key string) string {
	if node == nil || node.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1].Value
		}
	}
	return ""
}

// TestSerializeEntry_MarketplaceProvenanceFields covers mkt-031's write-side
// lists (write.go's entryFieldOrder + serializeEntry fields map): a fresh
// LockedDep with all four provenance fields set serializes every one of
// them, and an entry with them all empty omits every one (purely additive,
// never forces the keys to appear).
func TestSerializeEntry_MarketplaceProvenanceFields(t *testing.T) {
	// Arrange
	withProvenance := &LockedDep{
		RepoURL:               "github.com/acme/foo",
		Source:                "git",
		ResolvedRef:           "v1.0.0",
		DiscoveredVia:         "acme-marketplace",
		MarketplacePluginName: "Foo-Plugin",
		SourceURL:             "https://example.com/marketplace.json",
		SourceDigest:          "sha256:deadbeef",
	}
	withoutProvenance := &LockedDep{
		RepoURL:     "github.com/acme/bar",
		Source:      "git",
		ResolvedRef: "v2.0.0",
	}

	// Act
	nodeWith := serializeEntry(withProvenance, nil)
	nodeWithout := serializeEntry(withoutProvenance, nil)

	// Assert
	gotWith := entryKeys(nodeWith)
	for _, key := range []string{"discovered_via", "marketplace_plugin_name", "source_url", "source_digest"} {
		if !gotWith[key] {
			t.Errorf("serializeEntry(with provenance) missing key %q; got keys %v", key, gotWith)
		}
	}
	if v := entryValue(nodeWith, "discovered_via"); v != "acme-marketplace" {
		t.Errorf("discovered_via = %q, want %q", v, "acme-marketplace")
	}
	if v := entryValue(nodeWith, "marketplace_plugin_name"); v != "Foo-Plugin" {
		t.Errorf("marketplace_plugin_name = %q, want %q", v, "Foo-Plugin")
	}
	if v := entryValue(nodeWith, "source_url"); v != "https://example.com/marketplace.json" {
		t.Errorf("source_url = %q, want %q", v, "https://example.com/marketplace.json")
	}
	if v := entryValue(nodeWith, "source_digest"); v != "sha256:deadbeef" {
		t.Errorf("source_digest = %q, want %q", v, "sha256:deadbeef")
	}

	gotWithout := entryKeys(nodeWithout)
	for _, key := range []string{"discovered_via", "marketplace_plugin_name", "source_url", "source_digest"} {
		if gotWithout[key] {
			t.Errorf("serializeEntry(without provenance) unexpectedly has key %q", key)
		}
	}
}

// TestWriteLockfile_RoundTrip_ProvenanceNoDoubleEmit is the adversarial
// regression the design doc calls out by name: knownEntryFields (write.go)
// is a fifth, separate explicit list from entryFieldOrder/the serializeEntry
// fields map -- omitting a provenance key from knownEntryFields would make
// the passthrough "preserve unknown fields from original entry" loop treat
// an already-known, already-emitted provenance field as if it were an
// unrecognized x-* key and copy it onto the node a SECOND time. The fixture
// below stands in for "already exists, hand-formatted" content (jotted
// comments + an unrelated second dependency) rather than a freshly generated
// one, per this task's fixture-diversity requirement.
func TestWriteLockfile_RoundTrip_ProvenanceNoDoubleEmit(t *testing.T) {
	// Arrange
	yamlSrc := "# hand-maintained lockfile, do not delete this comment\n" +
		"lockfile_version: \"1\"\n" +
		"generated_at: \"2026-01-01T00:00:00Z\"\n" +
		"dependencies:\n" +
		"  - repo_url: github.com/acme/bar\n" +
		"    source: git\n" +
		"    resolved_ref: v2.0.0\n" +
		"  - repo_url: github.com/acme/foo\n" +
		"    source: git\n" +
		"    resolved_ref: v1.0.0\n" +
		"    discovered_via: acme-marketplace\n" +
		"    marketplace_plugin_name: Foo-Plugin\n" +
		"    source_url: https://example.com/marketplace.json\n" +
		"    source_digest: sha256:deadbeef\n"
	node, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatalf("ParseLockfile: %v", err)
	}

	// Act -- re-serialize against the ORIGINAL node, exactly like
	// deployAndFinalize's WriteLockfile(newLock, existingNode) call.
	origNode, err := yamlcore.SafeLoad([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("SafeLoad (orig): %v", err)
	}
	out, err := WriteLockfile(lf, origNode)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	// Assert -- byte-identical round trip (no rewrite at all for unchanged
	// data), AND each provenance key appears exactly once, not twice.
	if string(out) != yamlSrc {
		t.Errorf("round trip is NOT byte-equal.\nOriginal:\n%s\nOutput:\n%s", yamlSrc, string(out))
	}
	outStr := string(out)
	for _, key := range []string{"discovered_via:", "marketplace_plugin_name:", "source_url:", "source_digest:"} {
		if n := strings.Count(outStr, key); n != 1 {
			t.Errorf("key %q appears %d times in output, want exactly 1 (double-emit bug):\n%s", key, n, outStr)
		}
	}
}

// TestLockedDep_UniqueKey_ExcludesProvenance locks down mkt-031's "purely
// additive" invariant: two entries that differ ONLY in their marketplace
// provenance fields are the SAME dependency identity.
func TestLockedDep_UniqueKey_ExcludesProvenance(t *testing.T) {
	// Arrange
	a := &LockedDep{RepoURL: "github.com/acme/foo", DiscoveredVia: "acme-marketplace", MarketplacePluginName: "Foo"}
	b := &LockedDep{RepoURL: "github.com/acme/foo"}

	// Act
	keyA := a.UniqueKey()
	keyB := b.UniqueKey()

	// Assert
	if keyA != keyB {
		t.Errorf("UniqueKey() differs by provenance alone: %q vs %q, want equal", keyA, keyB)
	}
}

// TestIsSemanticEqual_ProvenanceParticipates locks down the design's chosen
// answer for whether provenance participates in IsSemanticEqual: two
// otherwise-identical lockfiles that differ only in one dependency's
// provenance fields are NOT semantically equal (mirrors the Python
// original's is_semantically_equivalent, per design.md's mkt-032 section).
func TestIsSemanticEqual_ProvenanceParticipates(t *testing.T) {
	// Arrange
	base := &Lockfile{Version: "1", Dependencies: []LockedDep{
		{RepoURL: "github.com/acme/foo", Source: "git", ResolvedRef: "v1.0.0"},
	}}
	withProvenance := &Lockfile{Version: "1", Dependencies: []LockedDep{
		{RepoURL: "github.com/acme/foo", Source: "git", ResolvedRef: "v1.0.0",
			DiscoveredVia: "acme-marketplace", MarketplacePluginName: "Foo"},
	}}

	// Act
	equal := IsSemanticEqual(base, withProvenance)

	// Assert
	if equal {
		t.Error("IsSemanticEqual() = true for lockfiles differing only in provenance, want false")
	}

	// A provenance-identical pair (e.g. carry-forward already applied) IS
	// semantically equal.
	identical := &Lockfile{Version: "1", Dependencies: []LockedDep{
		{RepoURL: "github.com/acme/foo", Source: "git", ResolvedRef: "v1.0.0",
			DiscoveredVia: "acme-marketplace", MarketplacePluginName: "Foo"},
	}}
	if !IsSemanticEqual(withProvenance, identical) {
		t.Error("IsSemanticEqual() = false for provenance-identical lockfiles, want true")
	}
}
