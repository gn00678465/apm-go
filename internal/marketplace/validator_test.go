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
