package authoring

import (
	"testing"
)

func TestAsConfigValidationError_NilStaysNil(t *testing.T) {
	if err := asConfigValidationError(nil); err != nil {
		t.Errorf("asConfigValidationError(nil) = %v, want nil", err)
	}
}

// A legacy marketplace.yml is loaded through the same strict rules
// (doctor.py:245-252 reports its MarketplaceYmlError as
// "marketplace.yml has errors: ...").
func TestLoadAuthoringConfig_LegacyValidationError_IsConfigValidationError(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "marketplace.yml", "name: m\npackages:\n  - source: owner/a\n    ref: main\n")

	_, src, err := LoadAuthoringConfig(dir)

	if err == nil || !IsConfigValidationError(err) {
		t.Fatalf("err = %v, want a config validation error", err)
	}
	if src != ConfigSourceLegacy {
		t.Errorf("source = %v, want ConfigSourceLegacy so doctor attributes the error to marketplace.yml", src)
	}
	if got, want := err.Error(), "'packages[0].name' is required"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}
