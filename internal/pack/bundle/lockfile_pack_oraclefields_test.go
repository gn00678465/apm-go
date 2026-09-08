package bundle

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestEnrichLockfileForPack_AlwaysEmitsGeneratedAtAndDeployments pins ticket
// 27: the Oracle's LockFile.to_yaml (deps/lockfile.py:815-822) writes
// lockfile_version, generated_at, dependencies and deployments outside every
// `if` guard, so a bundle's embedded lockfile carries all four even when the
// source apm.lock.yaml declared none of them.
func TestEnrichLockfileForPack_AlwaysEmitsGeneratedAtAndDeployments(t *testing.T) {
	// Arrange: the minimal source lockfile -- no generated_at:, no deployments:.
	out := enrich(t, "lockfile_version: \"1\"\n")

	// Assert
	for _, want := range []string{"lockfile_version:", "generated_at:", "dependencies:", "deployments:"} {
		if !strings.Contains(out, want) {
			t.Errorf("embedded lockfile is missing %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "deployments: []") {
		t.Errorf("want an empty deployments list, got:\n%s", out)
	}
	// The Oracle's key order: generated_at directly after lockfile_version.
	if i, j := strings.Index(out, "lockfile_version:"), strings.Index(out, "generated_at:"); i < 0 || j < i {
		t.Errorf("generated_at must follow lockfile_version:\n%s", out)
	}
}

// TestEnrichLockfileForPack_ExistingDeploymentsNotDuplicated guards the
// failure mode this fix could easily introduce: "deployments" is absent from
// SerializeLockfile's knownTopKeys, so an existing key already survives via
// the unknown-key preservation loop. Appending unconditionally would emit it
// twice -- a duplicate mapping key, worse than the omission being fixed.
func TestEnrichLockfileForPack_ExistingDeploymentsNotDuplicated(t *testing.T) {
	// Arrange
	out := enrich(t, "lockfile_version: \"1\"\ndeployments:\n  - kind: skill\n    path: a/b.md\n")

	// Assert
	if got := strings.Count(out, "deployments:"); got != 1 {
		t.Errorf("deployments key appears %d times, want exactly 1:\n%s", got, out)
	}
	if !strings.Contains(out, "path: a/b.md") {
		t.Errorf("existing deployments rows were not preserved:\n%s", out)
	}
	if strings.Contains(out, "deployments: []") {
		t.Errorf("existing deployments rows were clobbered with an empty list:\n%s", out)
	}
}

// TestEnrichLockfileForPack_RealGeneratedAtPreserved checks the ticket-27
// default never overwrites a value the source lockfile actually had.
func TestEnrichLockfileForPack_RealGeneratedAtPreserved(t *testing.T) {
	// Arrange
	out := enrich(t, "lockfile_version: \"1\"\ngenerated_at: \"2026-01-02T03:04:05Z\"\n")

	// Assert
	if !strings.Contains(out, "2026-01-02T03:04:05Z") {
		t.Errorf("generated_at value was not preserved:\n%s", out)
	}
	if got := strings.Count(out, "generated_at:"); got != 1 {
		t.Errorf("generated_at appears %d times, want exactly 1:\n%s", got, out)
	}
}

// TestSerializeLockfile_TopLevelWriteUnaffected is the other half of ticket
// 27's scoping decision: the two unconditional keys belong to the pack-only
// wrapping layer, NOT to SerializeLockfile, because the top-level
// apm.lock.yaml path deliberately guarantees a byte-identical round-trip of
// an unchanged user file -- a property the Oracle does not have and that
// `install` must not break by silently adding keys.
func TestSerializeLockfile_TopLevelWriteUnaffected(t *testing.T) {
	// Arrange
	src := "lockfile_version: \"1\"\ndependencies: []\n"
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(doc)
	if err != nil {
		t.Fatal(err)
	}

	// Act
	b, err := lockfile.WriteLockfile(lf, doc)
	if err != nil {
		t.Fatal(err)
	}

	// Assert
	if string(b) != src {
		t.Errorf("top-level lockfile write is no longer a byte-identical round-trip:\nwant %q\ngot  %q", src, b)
	}
}

func enrich(t *testing.T, src string) string {
	t.Helper()
	doc, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad(%q): %v", src, err)
	}
	lf, err := lockfile.ParseLockfile(doc)
	if err != nil {
		t.Fatalf("ParseLockfile(%q): %v", src, err)
	}
	b, err := EnrichLockfileForPack(lf, PackMetadata{}, doc)
	if err != nil {
		t.Fatalf("EnrichLockfileForPack(%q): %v", src, err)
	}
	return string(b)
}
