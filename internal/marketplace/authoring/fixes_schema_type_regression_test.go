package authoring

import (
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated-fixes §D, regression: string-tagged
// scalars stay accepted (SC-F19, including the D-1 deviation `name: yes`),
// existing messages for empty and missing values are unchanged, and numeric
// version/ref scalars keep their source text (SC-F10).

func loadPackages(t *testing.T, packagesYAML string) []PackageEntry {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", schemaTestHeader+packagesYAML)
	cfg, _, err := LoadAuthoringConfig(dir)
	if err != nil {
		t.Fatalf("LoadAuthoringConfig: %v\n%s", err, packagesYAML)
	}
	return cfg.Packages
}

func TestLoadAuthoringConfig_StringLikeScalars_Accepted(t *testing.T) {
	t.Run("QuotedDigits", func(t *testing.T) {
		pkgs := loadPackages(t, "    - name: \"123\"\n      source: owner/repo\n      ref: main\n")
		if pkgs[0].Name != "123" {
			t.Errorf("Name = %q, want 123", pkgs[0].Name)
		}
	})
	t.Run("YesIsAStringInYAML12", func(t *testing.T) {
		pkgs := loadPackages(t, "    - name: yes\n      source: owner/repo\n      ref: main\n")
		if pkgs[0].Name != "yes" {
			t.Errorf("Name = %q, want yes", pkgs[0].Name)
		}
	})
	t.Run("EmptySourceKeepsValidatorMessage", func(t *testing.T) {
		err := loadExpectingError(t, "    - name: tool\n      source: \"\"\n      ref: main\n")
		if !strings.Contains(err.Error(), "marketplace source is empty") {
			t.Errorf("error = %q, want the ValidateMarketplaceSource message", err)
		}
	})
	t.Run("EmptyName", func(t *testing.T) {
		err := loadExpectingError(t, "    - name: \"\"\n      source: owner/repo\n      ref: main\n")
		if got, want := err.Error(), "'packages[0].name' must be a non-empty string"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})
	t.Run("MissingName", func(t *testing.T) {
		err := loadExpectingError(t, "    - source: owner/repo\n      ref: main\n")
		if got, want := err.Error(), "'packages[0].name' is required"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})
}

func TestLoadAuthoringConfig_NumericVersionOrRef_Accepted(t *testing.T) {
	pkgs := loadPackages(t, "    - name: tool\n      source: owner/repo\n      version: 1.10\n      ref: 123\n")
	if pkgs[0].Version != "1.10" || pkgs[0].Ref != "123" {
		t.Errorf("Version = %q, Ref = %q; want 1.10 and 123 as written", pkgs[0].Version, pkgs[0].Ref)
	}
}
