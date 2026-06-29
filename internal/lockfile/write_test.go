package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func TestWriteLockfile_RoundTrip_V1(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "v1-git-only.yml"))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}

	origNode, _ := yamlcore.SafeLoad(data)
	out, err := WriteLockfile(lf, origNode)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	// Byte-equal round-trip check
	if string(data) != string(out) {
		t.Errorf("v1-git-only round-trip is NOT byte-equal.\nOriginal:\n%s\nOutput:\n%s", string(data), string(out))
	}
}

func TestWriteLockfile_RoundTrip_V2(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "v2-with-registry.yml"))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}

	origNode, _ := yamlcore.SafeLoad(data)
	out, err := WriteLockfile(lf, origNode)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	// Byte-equal round-trip check
	if string(data) != string(out) {
		t.Errorf("v2-with-registry round-trip is NOT byte-equal.\nOriginal:\n%s\nOutput:\n%s", string(data), string(out))
	}
}

func TestWriteLockfile_RoundTrip_UnknownFields(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "round-trip-unknown-fields.yml"))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}

	out, err := WriteLockfile(lf, node)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	outStr := string(out)
	// Top-level x-acme-top must be preserved
	if !strings.Contains(outStr, "x-acme-top") {
		t.Error("top-level x-acme-top should be preserved")
	}
	// Entry-level unknown fields must be preserved
	if !strings.Contains(outStr, "future_unknown_field") {
		t.Error("entry-level future_unknown_field should be preserved")
	}
	if !strings.Contains(outStr, "x-acme-pin") {
		t.Error("entry-level x-acme-pin should be preserved")
	}
}

func TestDetermineVersion(t *testing.T) {
	tests := []struct {
		name     string
		deps     []LockedDep
		existing string
		want     string
	}{
		{"no registry, no existing", []LockedDep{{Source: ""}}, "", "1"},
		{"registry present", []LockedDep{{Source: "registry"}}, "", "2"},
		{"monotonicity: existing 2, no registry", []LockedDep{{Source: ""}}, "2", "2"},
		{"existing 1, no registry", []LockedDep{{Source: ""}}, "1", "1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineVersion(tt.deps, tt.existing)
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSortDependencies(t *testing.T) {
	deps := []LockedDep{
		{RepoURL: "github.com/z/z"},
		{RepoURL: "github.com/a/a", VirtualPath: "sub"},
		{RepoURL: "github.com/a/a"},
	}
	SortDependencies(deps)
	if deps[0].RepoURL != "github.com/a/a" || deps[0].VirtualPath != "" {
		t.Errorf("first should be a/a (no vp), got %s/%s", deps[0].RepoURL, deps[0].VirtualPath)
	}
	if deps[1].RepoURL != "github.com/a/a" || deps[1].VirtualPath != "sub" {
		t.Errorf("second should be a/a/sub, got %s/%s", deps[1].RepoURL, deps[1].VirtualPath)
	}
	if deps[2].RepoURL != "github.com/z/z" {
		t.Errorf("third should be z/z, got %s", deps[2].RepoURL)
	}
}

func TestIsSemanticEqual_OnlyAdvisoryDiffers(t *testing.T) {
	a := &Lockfile{
		Version:     "1",
		GeneratedAt: "2026-01-01",
		APMVersion:  "1.0.0",
		Dependencies: []LockedDep{
			{RepoURL: "a/b", ResolvedCommit: "abc", ResolvedAt: "2026-01-01T00:00:00Z"},
		},
	}
	b := &Lockfile{
		Version:     "1",
		GeneratedAt: "2026-06-29",
		APMVersion:  "2.0.0",
		Dependencies: []LockedDep{
			{RepoURL: "a/b", ResolvedCommit: "abc", ResolvedAt: "2026-06-29T00:00:00Z"},
		},
	}
	if !IsSemanticEqual(a, b) {
		t.Error("lockfiles differing only in generated_at/apm_version/resolved_at should be equal")
	}
}

func TestIsSemanticEqual_ContentDiffers(t *testing.T) {
	a := &Lockfile{Version: "1", Dependencies: []LockedDep{{RepoURL: "a/b", ResolvedCommit: "abc"}}}
	b := &Lockfile{Version: "1", Dependencies: []LockedDep{{RepoURL: "a/b", ResolvedCommit: "def"}}}
	if IsSemanticEqual(a, b) {
		t.Error("lockfiles with different commits should not be equal")
	}
}

func TestWriteLockfile_RoundTrip_ByteEqual(t *testing.T) {
	p := oraclePath(t, filepath.Join("lockfile", "round-trip-unknown-fields.yml"))
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}

	// Re-load fresh node for the original (SafeLoad may mutate)
	origNode, _ := yamlcore.SafeLoad(data)

	out, err := WriteLockfile(lf, origNode)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}

	if string(data) != string(out) {
		t.Errorf("round-trip is NOT byte-equal.\nOriginal:\n%s\nOutput:\n%s", string(data), string(out))
	}
}

func TestWriteLockfile_OmitsEmptyFields(t *testing.T) {
	lf := &Lockfile{
		Version: "1",
		Dependencies: []LockedDep{
			{RepoURL: "acme/foo", ResolvedCommit: "abc123", Depth: 1},
		},
	}
	out, err := WriteLockfile(lf, nil)
	if err != nil {
		t.Fatal(err)
	}
	outStr := string(out)
	if strings.Contains(outStr, "null") {
		t.Error("output should not contain null")
	}
	if strings.Contains(outStr, "resolved_tag") {
		t.Error("empty resolved_tag should be omitted")
	}
	if strings.Contains(outStr, "source:") {
		t.Error("empty source should be omitted")
	}
}
