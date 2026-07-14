package bundle

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

func TestEnrichLockfileForPack_BareHexNotEnvelopePrefixed(t *testing.T) {
	lf := &lockfile.Lockfile{Version: "1"}
	meta := PackMetadata{
		Format:      "plugin",
		Target:      "all",
		PackedAt:    "2026-07-12T00:00:00+00:00",
		BundleFiles: map[string]string{"plugin.json": "abc123"},
	}
	out, err := EnrichLockfileForPack(lf, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.Contains(text, "abc123") {
		t.Fatalf("output = %s, want bare hex digest present", text)
	}
	if strings.Contains(text, "sha256:abc123") {
		t.Errorf("output = %s, must NOT use the sha256: envelope prefix (bare hex required for Python interop)", text)
	}
}

func TestEnrichLockfileForPack_BundleFilesKeySorted(t *testing.T) {
	lf := &lockfile.Lockfile{Version: "1"}
	meta := PackMetadata{
		Format:   "plugin",
		Target:   "all",
		PackedAt: "2026-07-12T00:00:00+00:00",
		BundleFiles: map[string]string{
			"zebra.md": "z",
			"apple.md": "a",
		},
	}
	out, err := EnrichLockfileForPack(lf, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	appleIdx := strings.Index(text, "apple.md")
	zebraIdx := strings.Index(text, "zebra.md")
	if appleIdx < 0 || zebraIdx < 0 || appleIdx > zebraIdx {
		t.Errorf("output = %s, want apple.md before zebra.md (sorted keys)", text)
	}
}

func TestEnrichLockfileForPack_OmitsBundleFilesWhenEmpty(t *testing.T) {
	lf := &lockfile.Lockfile{Version: "1"}
	meta := PackMetadata{Format: "plugin", Target: "all", PackedAt: "2026-07-12T00:00:00+00:00"}
	out, err := EnrichLockfileForPack(lf, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "bundle_files") {
		t.Errorf("output = %s, must not emit bundle_files when empty", out)
	}
}

func TestEnrichLockfileForPack_StripsLocalDeployedFields(t *testing.T) {
	lf := &lockfile.Lockfile{
		Version:             "1",
		LocalDeployedFiles:  []string{".claude/skills/x/SKILL.md"},
		LocalDeployedHashes: map[string]string{".claude/skills/x/SKILL.md": "sha256:deadbeef"},
	}
	out, err := EnrichLockfileForPack(lf, PackMetadata{Format: "plugin", Target: "all", PackedAt: "t"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if strings.Contains(text, "local_deployed_files") || strings.Contains(text, "local_deployed_file_hashes") {
		t.Errorf("output = %s, must strip local_deployed_files/local_deployed_file_hashes from the bundle lockfile", text)
	}
	// The original lockfile object passed in must not be mutated by the
	// stripping (findings: "Does NOT mutate the original lockfile object").
	if len(lf.LocalDeployedFiles) == 0 || len(lf.LocalDeployedHashes) == 0 {
		t.Error("EnrichLockfileForPack must not mutate its input lockfile")
	}
}

func TestEnrichLockfileForPack_FieldOrderAndPackSectionFirst(t *testing.T) {
	lf := &lockfile.Lockfile{Version: "1"}
	meta := PackMetadata{Format: "plugin", Target: "claude", PackedAt: "2026-07-12T00:00:00+00:00"}
	out, err := EnrichLockfileForPack(lf, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if !strings.HasPrefix(text, "pack:") {
		t.Fatalf("output = %s, want it to start with the pack: section", text)
	}
	formatIdx := strings.Index(text, "format:")
	targetIdx := strings.Index(text, "target:")
	packedIdx := strings.Index(text, "packed_at:")
	lockVerIdx := strings.Index(text, "lockfile_version:")
	if !(formatIdx >= 0 && formatIdx < targetIdx && targetIdx < packedIdx && packedIdx < lockVerIdx) {
		t.Errorf("output = %s, want format < target < packed_at < lockfile_version ordering", text)
	}
}

func TestParsePackMetadata_RoundTripsThroughEnrich(t *testing.T) {
	lf := &lockfile.Lockfile{Version: "1"}
	meta := PackMetadata{
		Format:      "plugin",
		Target:      "claude,copilot",
		PackedAt:    "2026-07-12T00:00:00+00:00",
		BundleFiles: map[string]string{"plugin.json": "abc123", "agents/foo.md": "def456"},
	}
	out, err := EnrichLockfileForPack(lf, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParsePackMetadata(doc.Content[0])
	if !ok {
		t.Fatal("ParsePackMetadata: ok = false, want true")
	}
	if got.Format != meta.Format || got.Target != meta.Target || got.PackedAt != meta.PackedAt {
		t.Errorf("got = %+v, want %+v", got, meta)
	}
	if len(got.BundleFiles) != 2 || got.BundleFiles["plugin.json"] != "abc123" || got.BundleFiles["agents/foo.md"] != "def456" {
		t.Errorf("BundleFiles = %v, want round-tripped bare hex map", got.BundleFiles)
	}
}

func TestParsePackMetadata_NoPackSection_ReturnsNotOK(t *testing.T) {
	doc, err := yamlcore.SafeLoad([]byte("lockfile_version: \"1\"\ndependencies: []\n"))
	if err != nil {
		t.Fatal(err)
	}
	_, ok := ParsePackMetadata(doc.Content[0])
	if ok {
		t.Error("ParsePackMetadata: ok = true, want false for a lockfile with no pack: section")
	}
}

func TestParsePackMetadata_EmptyBundleFiles_OmittedKeyYieldsNilMap(t *testing.T) {
	lf := &lockfile.Lockfile{Version: "1"}
	meta := PackMetadata{Format: "plugin", Target: "all", PackedAt: "t"}
	out, err := EnrichLockfileForPack(lf, meta, nil)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(out)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := ParsePackMetadata(doc.Content[0])
	if !ok {
		t.Fatal("ParsePackMetadata: ok = false, want true")
	}
	if len(got.BundleFiles) != 0 {
		t.Errorf("BundleFiles = %v, want empty when bundle_files was omitted", got.BundleFiles)
	}
}
