package marketplace

import (
	"reflect"
	"testing"
)

// TestValidate covers Validate's structural checks on a MarketplaceManifest
// (mkt-016's dependency): the manifest carries a non-empty name, every
// plugin entry has a non-empty name and a "source", and no two plugin names
// collide case-insensitively. This mirrors the Python original's
// marketplace.validator.validate_marketplace (validate_plugin_schema +
// validate_no_duplicate_names), flattened into a single []Finding slice.
func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		manifest *MarketplaceManifest
		want     []Finding
	}{
		{
			name:     "nil manifest reports a single error finding instead of panicking",
			manifest: nil,
			want: []Finding{
				{Level: LevelError, Message: "marketplace manifest is nil"},
			},
		},
		{
			name: "valid manifest with distinct plugin names produces no findings",
			manifest: &MarketplaceManifest{
				Name: "acme-tools",
				Plugins: []MarketplacePlugin{
					{Name: "foo", Source: "./plugin-a"},
					{Name: "bar", Source: "./plugin-b"},
				},
			},
			want: nil,
		},
		{
			name: "empty manifest name is an error",
			manifest: &MarketplaceManifest{
				Name: "",
				Plugins: []MarketplacePlugin{
					{Name: "foo", Source: "./plugin-a"},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: "marketplace manifest name is empty"},
			},
		},
		{
			name: "whitespace-only manifest name is an error",
			manifest: &MarketplaceManifest{
				Name: "   ",
				Plugins: []MarketplacePlugin{
					{Name: "foo", Source: "./plugin-a"},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: "marketplace manifest name is empty"},
			},
		},
		{
			name: "plugin with empty name is an error",
			manifest: &MarketplaceManifest{
				Name: "acme-tools",
				Plugins: []MarketplacePlugin{
					{Name: "", Source: "./plugin-a"},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: "plugin entry has empty name"},
			},
		},
		{
			name: "plugin missing source is an error",
			manifest: &MarketplaceManifest{
				Name: "acme-tools",
				Plugins: []MarketplacePlugin{
					{Name: "foo", Source: nil},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: `plugin "foo" is missing required field 'source'`},
			},
		},
		{
			name: "plugin with both empty name and missing source reports both, name first",
			manifest: &MarketplaceManifest{
				Name: "acme-tools",
				Plugins: []MarketplacePlugin{
					{Name: "", Source: nil},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: "plugin entry has empty name"},
				{Level: LevelError, Message: `plugin "" is missing required field 'source'`},
			},
		},
		{
			name: "duplicate plugin names are case-insensitive",
			manifest: &MarketplaceManifest{
				Name: "acme-tools",
				Plugins: []MarketplacePlugin{
					{Name: "Foo", Source: "./plugin-a"},
					{Name: "foo", Source: "./plugin-b"},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: `duplicate plugin name: "foo" (conflicts with "Foo")`},
			},
		},
		{
			name: "schema findings are reported before duplicate-name findings",
			manifest: &MarketplaceManifest{
				Name: "",
				Plugins: []MarketplacePlugin{
					{Name: "foo", Source: nil},
					{Name: "foo", Source: "./plugin-b"},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: "marketplace manifest name is empty"},
				{Level: LevelError, Message: `plugin "foo" is missing required field 'source'`},
				{Level: LevelError, Message: `duplicate plugin name: "foo" (conflicts with "foo")`},
			},
		},
		{
			name: "three or more plugins sharing a name each report a conflict against the first-seen entry",
			manifest: &MarketplaceManifest{
				Name: "acme-tools",
				Plugins: []MarketplacePlugin{
					{Name: "foo", Source: "./plugin-a"},
					{Name: "FOO", Source: "./plugin-b"},
					{Name: "Foo", Source: "./plugin-c"},
				},
			},
			want: []Finding{
				{Level: LevelError, Message: `duplicate plugin name: "FOO" (conflicts with "foo")`},
				{Level: LevelError, Message: `duplicate plugin name: "Foo" (conflicts with "foo")`},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			manifest := tt.manifest

			// Act
			got := Validate(manifest)

			// Assert
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Validate() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

// TestValidateChecks covers the per-check grouping `marketplace validate`
// renders from (one line per named check, passing checks included),
// mirroring Python validate_marketplace's [Schema, Names] ValidationResult
// list.
func TestValidateChecks(t *testing.T) {
	t.Run("clean manifest: three named checks, all passed", func(t *testing.T) {
		m := &MarketplaceManifest{
			Name: "acme-tools",
			Plugins: []MarketplacePlugin{
				{Name: "alpha", Source: "./alpha"},
				{Name: "beta", Source: "./beta"},
			},
		}
		got := ValidateChecks(m)
		if len(got) != 3 || got[0].CheckName != "Structure" || got[1].CheckName != "Schema" || got[2].CheckName != "Names" {
			t.Fatalf("ValidateChecks() checks = %#v, want [Structure, Schema, Names]", got)
		}
		if !got[0].Passed() || !got[1].Passed() || !got[2].Passed() {
			t.Errorf("ValidateChecks() = %#v, want all three checks passed", got)
		}
	})

	t.Run("schema and duplicate findings land in their own checks", func(t *testing.T) {
		m := &MarketplaceManifest{
			Name: "acme-tools",
			Plugins: []MarketplacePlugin{
				{Name: "alpha"},
				{Name: "Alpha", Source: "./a"},
			},
		}
		got := ValidateChecks(m)
		if len(got) != 3 {
			t.Fatalf("ValidateChecks() returned %d checks, want 3", len(got))
		}
		structure, schema, names := got[0], got[1], got[2]
		if !structure.Passed() {
			t.Errorf("Structure check = %#v, want passed (no structural errors)", structure)
		}
		if schema.Passed() || len(schema.Findings) != 1 {
			t.Errorf("Schema check = %#v, want exactly the missing-source finding", schema)
		}
		if names.Passed() || len(names.Findings) != 1 {
			t.Errorf("Names check = %#v, want exactly the duplicate-name finding", names)
		}
	})

	t.Run("structural errors land in the Structure check, in manifest order", func(t *testing.T) {
		m := &MarketplaceManifest{
			Name:             "acme-tools",
			StructuralErrors: []string{"plugins: expected a list"},
		}
		got := ValidateChecks(m)
		structure := got[0]
		if structure.CheckName != "Structure" || structure.Passed() || len(structure.Findings) != 1 {
			t.Fatalf("Structure check = %#v, want exactly one failing finding", structure)
		}
		if structure.Findings[0].Message != "plugins: expected a list" || structure.Findings[0].Level != LevelError {
			t.Errorf("Structure finding = %#v, want the structural error verbatim as an error", structure.Findings[0])
		}
	})

	t.Run("flat Validate stays the ordered flatten of ValidateChecks", func(t *testing.T) {
		m := &MarketplaceManifest{
			Plugins: []MarketplacePlugin{{Name: "alpha"}, {Name: "Alpha", Source: "./a"}},
		}
		var want []Finding
		for _, check := range ValidateChecks(m) {
			want = append(want, check.Findings...)
		}
		if got := Validate(m); !reflect.DeepEqual(got, want) {
			t.Errorf("Validate() = %#v, want ValidateChecks flattened %#v", got, want)
		}
	})
}
