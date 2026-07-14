package marketplace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPinKey covers mkt-034a's key format (design.md gaps A7): lowercase
// throughout, and the version segment omitted ENTIRELY (not left as a
// trailing "/") when version is empty.
func TestPinKey(t *testing.T) {
	tests := []struct {
		name    string
		mkt     string
		plugin  string
		version string
		want    string
	}{
		{"no version", "Acme", "MyPlugin", "", "acme/myplugin"},
		{"with version", "Acme", "MyPlugin", "1.0.0", "acme/myplugin/1.0.0"},
		{"version already mixed case", "acme", "my-plugin", "V2.0.0", "acme/my-plugin/v2.0.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := pinKey(tt.mkt, tt.plugin, tt.version); got != tt.want {
				t.Errorf("pinKey(%q, %q, %q) = %q, want %q", tt.mkt, tt.plugin, tt.version, got, tt.want)
			}
		})
	}
}

// TestCheckAndRecordRefPin_FirstTimeNoWarning covers the base case: a
// (marketplace, plugin, version) triple never seen before never warns, even
// though it still gets recorded.
func TestCheckAndRecordRefPin_FirstTimeNoWarning(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	// Act
	warning := checkAndRecordRefPin("acme", "p", "1.0.0", "v1.0.0")

	// Assert
	if warning != "" {
		t.Errorf("checkAndRecordRefPin() first call = %q, want no warning", warning)
	}
	pins := loadRefPins()
	if pins["acme/p/1.0.0"] != "v1.0.0" {
		t.Errorf("loadRefPins() after first call = %#v, want the ref recorded under acme/p/1.0.0", pins)
	}
}

// TestCheckAndRecordRefPin_ChangeTriggersWarning covers mkt-034a's core
// scenario: the SAME (marketplace, plugin, version) triple resolving to a
// DIFFERENT ref on a second call must warn -- "may indicate a ref swap
// attack" -- and the new ref must be persisted (overwriting the old pin).
func TestCheckAndRecordRefPin_ChangeTriggersWarning(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	if warning := checkAndRecordRefPin("acme", "p", "1.0.0", "v1.0.0"); warning != "" {
		t.Fatalf("checkAndRecordRefPin() first call = %q, want no warning", warning)
	}

	// Act
	warning := checkAndRecordRefPin("acme", "p", "1.0.0", "v1.0.0-evil-sha")

	// Assert
	if warning == "" {
		t.Fatal("checkAndRecordRefPin() second call with a changed ref = \"\", want a ref-swap warning")
	}
	for _, want := range []string{"p@acme", "v1.0.0", "v1.0.0-evil-sha", "ref swap"} {
		if !strings.Contains(warning, want) {
			t.Errorf("checkAndRecordRefPin() warning = %q, want it to mention %q", warning, want)
		}
	}
	pins := loadRefPins()
	if pins["acme/p/1.0.0"] != "v1.0.0-evil-sha" {
		t.Errorf("loadRefPins() after swap = %#v, want the NEW ref persisted", pins)
	}
}

// TestCheckAndRecordRefPin_SameRefNoWarning covers the non-change case: an
// unchanged ref on a second call must never warn.
func TestCheckAndRecordRefPin_SameRefNoWarning(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	checkAndRecordRefPin("acme", "p", "1.0.0", "v1.0.0")

	// Act
	warning := checkAndRecordRefPin("acme", "p", "1.0.0", "v1.0.0")

	// Assert
	if warning != "" {
		t.Errorf("checkAndRecordRefPin() with an unchanged ref = %q, want no warning", warning)
	}
}

// TestCheckAndRecordRefPin_VersionChangeNoFalsePositive covers the
// version-scoping design point directly: a legitimate version bump (a new
// "version" value) is a DIFFERENT pin key entirely, so it must never be
// misreported as a ref swap even though the ref also changed (the expected,
// legitimate shape of a version bump).
func TestCheckAndRecordRefPin_VersionChangeNoFalsePositive(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	checkAndRecordRefPin("acme", "p", "1.0.0", "v1.0.0")

	// Act -- same plugin/marketplace, NEW version, NEW ref.
	warning := checkAndRecordRefPin("acme", "p", "2.0.0", "v2.0.0")

	// Assert
	if warning != "" {
		t.Errorf("checkAndRecordRefPin() on a version bump = %q, want no warning (different pin key)", warning)
	}
	pins := loadRefPins()
	if pins["acme/p/1.0.0"] != "v1.0.0" {
		t.Errorf("loadRefPins()[acme/p/1.0.0] = %q, want the old version's pin untouched", pins["acme/p/1.0.0"])
	}
	if pins["acme/p/2.0.0"] != "v2.0.0" {
		t.Errorf("loadRefPins()[acme/p/2.0.0] = %q, want the new version's pin recorded", pins["acme/p/2.0.0"])
	}
}

// TestLoadRefPins_MissingFileFailsOpen covers the no-file-yet case: an
// APM_CONFIG_DIR with no cache/marketplace/version-pins.json at all must
// return an empty map, not an error.
func TestLoadRefPins_MissingFileFailsOpen(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	// Act
	pins := loadRefPins()

	// Assert
	if len(pins) != 0 {
		t.Errorf("loadRefPins() on a missing file = %#v, want an empty map", pins)
	}
}

// TestLoadRefPins_CorruptFileFailsOpen covers the pin-file-damaged case
// implement.md's step 6 calls out explicitly: malformed JSON on disk must
// degrade to "no pins recorded" rather than erroring or panicking, and a
// subsequent checkAndRecordRefPin call must still succeed (self-healing the
// file on next write) instead of being blocked by the corruption.
func TestLoadRefPins_CorruptFileFailsOpen(t *testing.T) {
	// Arrange
	configDir := t.TempDir()
	t.Setenv("APM_CONFIG_DIR", configDir)
	pinsDir := filepath.Join(configDir, "cache", "marketplace")
	if err := os.MkdirAll(pinsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", pinsDir, err)
	}
	if err := os.WriteFile(filepath.Join(pinsDir, pinsFileName), []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("WriteFile(corrupt pins): %v", err)
	}

	// Act
	pins := loadRefPins()

	// Assert
	if len(pins) != 0 {
		t.Errorf("loadRefPins() on a corrupt file = %#v, want an empty map (fail-open)", pins)
	}

	// Act again -- checkAndRecordRefPin must still work (fail-open, not
	// fail-closed) despite the corrupt file it started from.
	warning := checkAndRecordRefPin("acme", "p", "", "v1.0.0")
	if warning != "" {
		t.Errorf("checkAndRecordRefPin() after a corrupt pins file = %q, want no warning (first-time key)", warning)
	}
	healed := loadRefPins()
	if healed["acme/p"] != "v1.0.0" {
		t.Errorf("loadRefPins() after healing write = %#v, want acme/p -> v1.0.0", healed)
	}
}

// TestLoadRefPins_NonObjectJSONFailsOpen covers valid JSON that is not a
// flat string-to-string object (e.g. a JSON array) -- also fail-open.
func TestLoadRefPins_NonObjectJSONFailsOpen(t *testing.T) {
	// Arrange
	configDir := t.TempDir()
	t.Setenv("APM_CONFIG_DIR", configDir)
	pinsDir := filepath.Join(configDir, "cache", "marketplace")
	if err := os.MkdirAll(pinsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", pinsDir, err)
	}
	if err := os.WriteFile(filepath.Join(pinsDir, pinsFileName), []byte(`["not", "an", "object"]`), 0o644); err != nil {
		t.Fatalf("WriteFile(array pins): %v", err)
	}

	// Act
	pins := loadRefPins()

	// Assert
	if len(pins) != 0 {
		t.Errorf("loadRefPins() on a JSON array = %#v, want an empty map (fail-open)", pins)
	}
}

// TestSaveRefPins_RoundTrip covers the atomic-write path itself, mirroring
// registry_test.go's TestSaveRegistry_RoundTrip: what saveRefPins writes,
// loadRefPins must read back unchanged, including a fresh $APM_CONFIG_DIR
// whose cache/marketplace directory does not exist yet.
func TestSaveRefPins_RoundTrip(t *testing.T) {
	// Arrange
	base := t.TempDir()
	configDir := filepath.Join(base, "not-yet-created", ".apm")
	t.Setenv("APM_CONFIG_DIR", configDir)
	want := map[string]string{"acme/p": "v1.0.0", "acme/p/2.0.0": "v2.0.0"}

	// Act
	saveRefPins(want)
	got := loadRefPins()

	// Assert
	if len(got) != len(want) {
		t.Fatalf("loadRefPins() = %#v, want %#v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("loadRefPins()[%q] = %q, want %q", k, got[k], v)
		}
	}
	// No stray .tmp file left behind.
	entries, err := os.ReadDir(filepath.Join(configDir, "cache", "marketplace"))
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if e.Name() != pinsFileName {
			t.Errorf("cache/marketplace dir contains unexpected entry %q, want only %q", e.Name(), pinsFileName)
		}
	}
}
