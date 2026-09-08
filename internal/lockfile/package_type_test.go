package lockfile

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// TestParseLockedDep_PackageType covers R9.4/AC45's parse-side case
// (parse.go's switch): package_type round-trips through ParseLockfile.
func TestParseLockedDep_PackageType(t *testing.T) {
	// Arrange
	yamlSrc := "lockfile_version: \"1\"\n" +
		"dependencies:\n" +
		"  - repo_url: github.com/acme/foo\n" +
		"    source: git\n" +
		"    resolved_ref: v1.0.0\n" +
		"    package_type: marketplace_plugin\n"
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
	if got := lf.Dependencies[0].PackageType; got != PackageTypeMarketplacePlugin {
		t.Errorf("PackageType = %q, want %q", got, PackageTypeMarketplacePlugin)
	}
}

// TestSerializeEntry_PackageType covers R9.4/AC45's write-side lists
// (write.go's entryFieldOrder + serializeEntry fields map): a fresh
// LockedDep with PackageType set serializes the key; an entry with it empty
// omits it (purely additive, never forced to appear).
func TestSerializeEntry_PackageType(t *testing.T) {
	// Arrange
	withType := &LockedDep{
		RepoURL:     "github.com/acme/foo",
		Source:      "git",
		ResolvedRef: "v1.0.0",
		PackageType: PackageTypeMarketplacePlugin,
	}
	withoutType := &LockedDep{
		RepoURL:     "github.com/acme/bar",
		Source:      "git",
		ResolvedRef: "v2.0.0",
	}

	// Act
	nodeWith := serializeEntry(withType, nil)
	nodeWithout := serializeEntry(withoutType, nil)

	// Assert
	if v := entryValue(nodeWith, "package_type"); v != PackageTypeMarketplacePlugin {
		t.Errorf("package_type = %q, want %q", v, PackageTypeMarketplacePlugin)
	}
	if gotWithout := entryKeys(nodeWithout); gotWithout["package_type"] {
		t.Errorf("serializeEntry(without PackageType) unexpectedly has key %q", "package_type")
	}
}

// TestWriteLockfile_RoundTrip_PackageTypeNoDoubleEmit is the adversarial
// regression the marketplace-provenance sibling test guards against, applied
// to package_type: knownEntryFields (write.go) is a separate explicit list
// from entryFieldOrder/the serializeEntry fields map -- omitting
// package_type from knownEntryFields would make the passthrough "preserve
// unknown fields from original entry" loop treat an already-known,
// already-emitted field as an unrecognized x-* key and copy it a SECOND
// time.
func TestWriteLockfile_RoundTrip_PackageTypeNoDoubleEmit(t *testing.T) {
	// Arrange
	yamlSrc := "lockfile_version: \"1\"\n" +
		"dependencies:\n" +
		"  - repo_url: github.com/acme/foo\n" +
		"    source: git\n" +
		"    resolved_ref: v1.0.0\n" +
		"    package_type: marketplace_plugin\n"
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

	// Assert -- byte-identical round trip, AND package_type appears exactly
	// once, not twice.
	if string(out) != yamlSrc {
		t.Errorf("round trip is NOT byte-equal.\nOriginal:\n%s\nOutput:\n%s", yamlSrc, string(out))
	}
	if n := strings.Count(string(out), "package_type:"); n != 1 {
		t.Errorf("package_type: appears %d times in output, want exactly 1 (double-emit bug):\n%s", n, string(out))
	}
}

// TestIsSemanticEqual_PackageTypeDiffers locks down that PackageType is
// content, not advisory (R9.4/AC45): two otherwise-identical lockfiles
// differing only in one dependency's package_type are NOT semantically
// equal, so a dependency moving from dependencies.apm to devDependencies.apm
// (or vice versa) is never masked as a no-op.
func TestIsSemanticEqual_PackageTypeDiffers(t *testing.T) {
	// Arrange
	withoutType := &Lockfile{Version: "1", Dependencies: []LockedDep{
		{RepoURL: "github.com/acme/foo", Source: "git", ResolvedRef: "v1.0.0"},
	}}
	withType := &Lockfile{Version: "1", Dependencies: []LockedDep{
		{RepoURL: "github.com/acme/foo", Source: "git", ResolvedRef: "v1.0.0", PackageType: PackageTypeMarketplacePlugin},
	}}

	// Act
	equal := IsSemanticEqual(withoutType, withType)

	// Assert
	if equal {
		t.Error("IsSemanticEqual() = true for lockfiles differing only in package_type, want false")
	}

	// A package_type-identical pair IS semantically equal.
	identical := &Lockfile{Version: "1", Dependencies: []LockedDep{
		{RepoURL: "github.com/acme/foo", Source: "git", ResolvedRef: "v1.0.0", PackageType: PackageTypeMarketplacePlugin},
	}}
	if !IsSemanticEqual(withType, identical) {
		t.Error("IsSemanticEqual() = false for package_type-identical lockfiles, want true")
	}
}
