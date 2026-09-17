package authoring

import (
	"errors"
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated §A: the four yml_schema.py rules the M3
// port skipped. Expected strings are the Oracle's own (yml_schema.py:453,
// 866-872, 1307-1309), never recomputed from this implementation.

const schemaTestHeader = "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: Acme\n  packages:\n"

func loadExpectingError(t *testing.T, packagesYAML string) error {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", schemaTestHeader+packagesYAML)
	_, _, err := LoadAuthoringConfig(dir)
	if err == nil {
		t.Fatalf("LoadAuthoringConfig accepted an invalid config:\n%s", packagesYAML)
	}
	return err
}

// SC-A1
func TestLoadAuthoringConfig_MissingName_Rejected(t *testing.T) {
	err := loadExpectingError(t, "    - source: owner/repo\n      ref: main\n")
	if got, want := err.Error(), "'packages[0].name' is required"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}

	t.Run("EmptyString", func(t *testing.T) {
		err := loadExpectingError(t, "    - name: \"\"\n      source: owner/repo\n      ref: main\n")
		if got, want := err.Error(), "'packages[0].name' must be a non-empty string"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})
}

// SC-A2
func TestLoadAuthoringConfig_MissingSource_Rejected(t *testing.T) {
	err := loadExpectingError(t, "    - name: nosrc\n      ref: main\n")
	if got, want := err.Error(), "'packages[0].source' is required"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// SC-A3
func TestLoadAuthoringConfig_RemoteWithoutVersionOrRef_Rejected(t *testing.T) {
	err := loadExpectingError(t, "    - name: bare\n      source: owner/repo\n")
	want := "packages[0] ('bare'): remote packages require at least one of 'version' or 'ref'"
	if got := err.Error(); got != want {
		t.Errorf("error = %q, want %q", got, want)
	}

	t.Run("LocalPackageExempt", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", schemaTestHeader+"    - name: local\n      source: ./x\n")
		if _, _, err := LoadAuthoringConfig(dir); err != nil {
			t.Fatalf("a local package needs neither version nor ref, got error: %v", err)
		}
	})
}

// SC-A4
func TestLoadAuthoringConfig_DuplicateNameCaseInsensitive_Rejected(t *testing.T) {
	err := loadExpectingError(t,
		"    - name: Dup\n      source: owner/a\n      ref: main\n"+
			"    - name: dup\n      source: owner/b\n      ref: main\n")
	if got, want := err.Error(), "Duplicate package name 'dup' (packages[0] and packages[1])"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}
}

// SC-A8 (yml_schema.py:853-862): a present-but-blank version or ref is an
// error, an absent one is "not set", and a padded value is stripped.
func TestLoadAuthoringConfig_EmptyVersionOrRef_Rejected(t *testing.T) {
	err := loadExpectingError(t, "    - name: tool\n      source: owner/repo\n      version: \"\"\n      ref: main\n")
	if got, want := err.Error(), "'packages[0].version' must be a non-empty string"; got != want {
		t.Errorf("error = %q, want %q", got, want)
	}

	t.Run("BlankRef", func(t *testing.T) {
		err := loadExpectingError(t, "    - name: tool\n      source: owner/repo\n      version: \"1.0.0\"\n      ref: \"  \"\n")
		if got, want := err.Error(), "'packages[0].ref' must be a non-empty string"; got != want {
			t.Errorf("error = %q, want %q", got, want)
		}
	})

	t.Run("PaddedValuesStripped", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", schemaTestHeader+"    - name: tool\n      source: owner/repo\n      version: \" 1.0.0 \"\n      ref: \" main \"\n")
		cfg, _, err := LoadAuthoringConfig(dir)
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Packages[0].Version != "1.0.0" || cfg.Packages[0].Ref != "main" {
			t.Errorf("version=%q ref=%q, want both stripped", cfg.Packages[0].Version, cfg.Packages[0].Ref)
		}
	})

	t.Run("NullIsUnset", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "apm.yml", schemaTestHeader+"    - name: tool\n      source: owner/repo\n      version: null\n      ref: main\n")
		cfg, _, err := LoadAuthoringConfig(dir)
		if err != nil || cfg.Packages[0].Version != "" {
			t.Errorf("(%v, version=%q), want a null version treated as unset", err, cfg.Packages[0].Version)
		}
	})
}

// SC-A6
func TestLoadAuthoringConfig_IsConfigValidationError(t *testing.T) {
	cases := map[string]string{
		"missing name":       "    - source: owner/repo\n      ref: main\n",
		"missing source":     "    - name: nosrc\n      ref: main\n",
		"no version nor ref": "    - name: bare\n      source: owner/repo\n",
		"duplicate":          "    - name: a\n      source: owner/a\n      ref: main\n    - name: A\n      source: owner/b\n      ref: main\n",
		"bad tag_pattern":    "    - name: bp\n      source: owner/repo\n      version: \"^1.0.0\"\n      tag_pattern: \"release-{name}\"\n",
	}
	for name, yml := range cases {
		t.Run(name, func(t *testing.T) {
			err := loadExpectingError(t, yml)
			if !IsConfigValidationError(err) {
				t.Errorf("IsConfigValidationError(%q) = false, want true", err)
			}
		})
	}

	t.Run("NoConfigIsNotValidation", func(t *testing.T) {
		_, _, err := LoadAuthoringConfig(t.TempDir())
		if err == nil || !IsNoConfigError(err) {
			t.Fatalf("expected the no-config sentinel, got %v", err)
		}
		if IsConfigValidationError(err) {
			t.Error("IsConfigValidationError(no-config) = true, want false")
		}
		if IsConfigValidationError(nil) || IsConfigValidationError(errors.New("x")) {
			t.Error("IsConfigValidationError must be false for nil and unrelated errors")
		}
	})

	t.Run("MessageIsBare", func(t *testing.T) {
		// The loader returns the Oracle's bare text; the command layer adds
		// the "marketplace config error: " prefix (SC-A5), and doctor
		// truncates the bare text (SC-A7).
		err := loadExpectingError(t, "    - source: owner/repo\n      ref: main\n")
		if strings.Contains(err.Error(), "marketplace config error") {
			t.Errorf("loader error %q must not carry the command-layer prefix", err)
		}
	})
}
