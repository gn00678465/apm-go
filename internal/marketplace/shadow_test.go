package marketplace

import (
	"context"
	"strings"
	"testing"
)

// TestDetectShadowWarnings_HitsOtherMarketplace covers mkt-034b's core
// scenario: the same plugin name registered in a SECOND marketplace produces
// a warning naming that other marketplace.
func TestDetectShadowWarnings_HitsOtherMarketplace(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	primaryDir := t.TempDir()
	writeManifest(t, primaryDir, `{"name": "acme", "plugins": [{"name": "widget", "source": "./p"}]}`)
	otherDir := t.TempDir()
	writeManifest(t, otherDir, `{"name": "shadow-mkt", "plugins": [{"name": "widget", "source": "./p"}]}`)
	if err := AddSource(MarketplaceSource{Name: "acme", URL: primaryDir}); err != nil {
		t.Fatalf("AddSource(acme): %v", err)
	}
	if err := AddSource(MarketplaceSource{Name: "shadow-mkt", URL: otherDir}); err != nil {
		t.Fatalf("AddSource(shadow-mkt): %v", err)
	}

	// Act
	warnings := detectShadowWarnings(context.Background(), "widget", "acme")

	// Assert
	if len(warnings) != 1 {
		t.Fatalf("detectShadowWarnings() = %#v, want exactly 1 warning", warnings)
	}
	for _, want := range []string{"widget", "shadow-mkt"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("detectShadowWarnings()[0] = %q, want it to mention %q", warnings[0], want)
		}
	}
}

// TestDetectShadowWarnings_ExcludesPrimaryMarketplace covers the negative
// case: the marketplace the user explicitly selected must never shadow
// itself, and the exclusion is case-insensitive (matching FindByName's own
// case-insensitive name comparison).
func TestDetectShadowWarnings_ExcludesPrimaryMarketplace(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	mktDir := t.TempDir()
	writeManifest(t, mktDir, `{"name": "acme", "plugins": [{"name": "widget", "source": "./p"}]}`)
	if err := AddSource(MarketplaceSource{Name: "ACME", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act -- query with different casing than the registered "ACME".
	warnings := detectShadowWarnings(context.Background(), "widget", "acme")

	// Assert
	if len(warnings) != 0 {
		t.Errorf("detectShadowWarnings() = %#v, want no warnings (only registered marketplace IS the primary one)", warnings)
	}
}

// TestDetectShadowWarnings_NoMatchReturnsEmpty covers the common case: no
// other marketplace has a plugin of this name.
func TestDetectShadowWarnings_NoMatchReturnsEmpty(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	primaryDir := t.TempDir()
	writeManifest(t, primaryDir, `{"name": "acme", "plugins": [{"name": "widget", "source": "./p"}]}`)
	otherDir := t.TempDir()
	writeManifest(t, otherDir, `{"name": "other", "plugins": [{"name": "gadget", "source": "./p"}]}`)
	if err := AddSource(MarketplaceSource{Name: "acme", URL: primaryDir}); err != nil {
		t.Fatalf("AddSource(acme): %v", err)
	}
	if err := AddSource(MarketplaceSource{Name: "other", URL: otherDir}); err != nil {
		t.Fatalf("AddSource(other): %v", err)
	}

	// Act
	warnings := detectShadowWarnings(context.Background(), "widget", "acme")

	// Assert
	if len(warnings) != 0 {
		t.Errorf("detectShadowWarnings() = %#v, want no warnings", warnings)
	}
}

// TestDetectShadowWarnings_FetchErrorSwallowed covers mkt-034b's
// fail-open/never-interrupts-install requirement directly: a candidate
// marketplace that cannot be fetched at all (its manifest is missing) must
// not produce an error or panic, AND must not stop the scan from still
// reporting a genuine hit found in a THIRD, healthy marketplace registered
// after the broken one.
func TestDetectShadowWarnings_FetchErrorSwallowed(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	primaryDir := t.TempDir()
	writeManifest(t, primaryDir, `{"name": "acme", "plugins": [{"name": "widget", "source": "./p"}]}`)
	brokenDir := t.TempDir() // no marketplace.json written -- Fetch will fail
	healthyDir := t.TempDir()
	writeManifest(t, healthyDir, `{"name": "healthy", "plugins": [{"name": "widget", "source": "./p"}]}`)

	if err := AddSource(MarketplaceSource{Name: "acme", URL: primaryDir}); err != nil {
		t.Fatalf("AddSource(acme): %v", err)
	}
	if err := AddSource(MarketplaceSource{Name: "broken", URL: brokenDir}); err != nil {
		t.Fatalf("AddSource(broken): %v", err)
	}
	if err := AddSource(MarketplaceSource{Name: "healthy", URL: healthyDir}); err != nil {
		t.Fatalf("AddSource(healthy): %v", err)
	}

	// Act -- must not panic.
	warnings := detectShadowWarnings(context.Background(), "widget", "acme")

	// Assert -- the broken marketplace produced no warning (and no crash),
	// but the healthy one after it was still reached and reported.
	if len(warnings) != 1 {
		t.Fatalf("detectShadowWarnings() = %#v, want exactly 1 warning (from the healthy marketplace, broken one swallowed)", warnings)
	}
	if !strings.Contains(warnings[0], "healthy") {
		t.Errorf("detectShadowWarnings()[0] = %q, want it to mention %q", warnings[0], "healthy")
	}
}

// TestDetectShadowWarnings_EmptyRegistryReturnsNil covers the trivial base
// case: no other marketplaces registered at all.
func TestDetectShadowWarnings_EmptyRegistryReturnsNil(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	mktDir := t.TempDir()
	writeManifest(t, mktDir, `{"name": "acme", "plugins": [{"name": "widget", "source": "./p"}]}`)
	if err := AddSource(MarketplaceSource{Name: "acme", URL: mktDir}); err != nil {
		t.Fatalf("AddSource(): %v", err)
	}

	// Act
	warnings := detectShadowWarnings(context.Background(), "widget", "acme")

	// Assert
	if len(warnings) != 0 {
		t.Errorf("detectShadowWarnings() = %#v, want no warnings (only the primary marketplace is registered)", warnings)
	}
}
