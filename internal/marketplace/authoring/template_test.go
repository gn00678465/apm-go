package authoring

import (
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/yamlcore"
)

// ── RenderMinimalApmYMLShell ──

func TestRenderMinimalApmYMLShell_DefaultsToMyMarketplace(t *testing.T) {
	// Act
	got := RenderMinimalApmYMLShell("")

	// Assert
	want := "name: my-marketplace\nversion: 0.1.0\ndescription: A short description of what this repo offers\n"
	if got != want {
		t.Errorf("RenderMinimalApmYMLShell(\"\") = %q, want %q", got, want)
	}
}

func TestRenderMinimalApmYMLShell_UsesGivenName(t *testing.T) {
	// Act
	got := RenderMinimalApmYMLShell("my-project")

	// Assert
	if !strings.HasPrefix(got, "name: my-project\n") {
		t.Errorf("RenderMinimalApmYMLShell(%q) = %q, want it to start with \"name: my-project\\n\"", "my-project", got)
	}
}

func TestRenderMinimalApmYMLShell_IsValidYAML(t *testing.T) {
	// Act
	got := RenderMinimalApmYMLShell("demo")

	// Assert
	if _, err := yamlcore.SafeLoad([]byte(got)); err != nil {
		t.Errorf("RenderMinimalApmYMLShell output does not parse as YAML: %v", err)
	}
}

// ── RenderInitBlock: shape (AC2 section-by-section comparison) ──

func TestRenderInitBlock_DefaultsOwnerToAcmeOrg(t *testing.T) {
	// Act
	got := RenderInitBlock("")

	// Assert
	if !strings.Contains(got, "name: acme-org") {
		t.Errorf("RenderInitBlock(\"\") = %q, want owner.name defaulted to acme-org", got)
	}
	if !strings.Contains(got, "url: https://github.com/acme-org") {
		t.Errorf("RenderInitBlock(\"\") = %q, want owner.url defaulted to https://github.com/acme-org", got)
	}
}

func TestRenderInitBlock_UsesGivenOwnerEverywhere(t *testing.T) {
	// Act
	got := RenderInitBlock("my-org")

	// Assert
	for _, want := range []string{
		"name: my-org",
		"url: https://github.com/my-org",
		"source: my-org/example-package",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderInitBlock(\"my-org\") missing %q; got:\n%s", want, got)
		}
	}
	if strings.Contains(got, "acme-org") {
		t.Errorf("RenderInitBlock(\"my-org\") still contains the default owner acme-org; got:\n%s", got)
	}
}

// TestRenderInitBlock_NeverSuggestsRefMain locks mkt-040 修訂版's trap: the
// upstream Python template's pinned-package example uses "ref: main", which
// `apm pack` rejects with HeadNotAllowedError (pack exposes no allow-head
// escape hatch, checklist mkt-055). The Go scaffold must not reproduce it.
func TestRenderInitBlock_NeverSuggestsRefMain(t *testing.T) {
	// Act
	got := RenderInitBlock("acme-org")

	// Assert
	if strings.Contains(got, "ref: main") {
		t.Errorf("RenderInitBlock output contains %q, want the mkt-040 修訂版 fix (a pinned tag example instead); got:\n%s", "ref: main", got)
	}
}

// TestRenderInitBlock_SectionsPresent walks AC2's required shape section by
// section: owner, build.tagPattern, outputs (map form with claude enabled
// and codex commented out), packages[] with a commented local-package
// example, and the "category" hint required for codex output.
func TestRenderInitBlock_SectionsPresent(t *testing.T) {
	// Act
	got := RenderInitBlock("acme-org")

	// Assert
	sections := map[string]string{
		"top-level marketplace key":       "marketplace:",
		"owner block":                     "  owner:",
		"owner.name":                      "    name: acme-org",
		"owner.url":                       "    url: https://github.com/acme-org",
		"build.tagPattern":                `tagPattern: "v{version}"`,
		"outputs map form":                "outputs:",
		"outputs.claude enabled":          "claude: {}",
		"outputs.codex commented out":     "# codex: {}",
		"category hint for codex outputs": "category:",
		"packages list":                   "packages:",
		"example package name":            "name: example-package",
		"example package source":          "source: acme-org/example-package",
		"example package version range":   `version: "^1.0.0"`,
		"commented local-path example":    "# - name: local-tool",
		"commented local-path source":     "#   source: ./packages/local-tool",
	}
	for label, want := range sections {
		if !strings.Contains(got, want) {
			t.Errorf("RenderInitBlock output missing %s (%q); got:\n%s", label, want, got)
		}
	}
}

func TestRenderInitBlock_IsValidYAML(t *testing.T) {
	// Act
	got := RenderInitBlock("acme-org")

	// Assert
	node, err := yamlcore.SafeLoad([]byte(got))
	if err != nil {
		t.Fatalf("RenderInitBlock output does not parse as YAML: %v", err)
	}
	root := node.Content[0]
	if root.Kind != yaml.MappingNode {
		t.Fatalf("RenderInitBlock output's root is not a mapping: %+v", root)
	}
	if root.Content[0].Value != "marketplace" {
		t.Errorf("RenderInitBlock output's only top-level key = %q, want %q", root.Content[0].Value, "marketplace")
	}
}
