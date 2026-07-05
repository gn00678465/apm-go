package marketplace

import (
	"encoding/json"
	"testing"
)

// TestMarketplaceManifest_OwnerShapes verifies the manifest "owner" field
// accepts both the object form ({"name": ...}, the common Claude Code
// layout) and the plain-string form, mirroring the Python original's
// parse_marketplace_json. The object form regression was caught live by
// ab_marketplace_consumer.py: a naive string-typed field rejected every
// real-world manifest that used {"owner": {"name": ...}}.
func TestMarketplaceManifest_OwnerShapes(t *testing.T) {
	tests := []struct {
		name  string
		doc   string
		owner string
	}{
		{"object form", `{"name":"m","owner":{"name":"Acme","url":"https://acme.dev"}}`, "Acme"},
		{"string form", `{"name":"m","owner":"Acme"}`, "Acme"},
		{"missing", `{"name":"m"}`, ""},
		{"object without name", `{"name":"m","owner":{"url":"https://acme.dev"}}`, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m MarketplaceManifest
			if err := json.Unmarshal([]byte(tt.doc), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if m.Owner != tt.owner {
				t.Errorf("Owner = %q, want %q", m.Owner, tt.owner)
			}
		})
	}
}

// TestMarketplaceManifest_PluginNormalization verifies the per-entry rules
// mirrored from Python's _parse_plugin_entry: Copilot "repository" shape
// synthesis, nameless/sourceless drops, and the mkt-026 dual-layer npm
// behavior (type/source npm dropped at parse; "kind: npm" survives parsing
// and is rejected later at resolution).
func TestMarketplaceManifest_PluginNormalization(t *testing.T) {
	doc := `{
		"name": "m",
		"metadata": {"pluginRoot": "./plugins"},
		"plugins": [
			{"name": "claude-style", "source": "./plugins/a"},
			{"name": "copilot-style", "repository": "owner/repo", "ref": "v1.0.0"},
			{"name": "npm-typed", "source": {"type": "npm", "package": "x"}},
			{"name": "npm-source-key", "source": {"source": "npm", "package": "x"}},
			{"name": "npm-kind-variant", "source": {"kind": "npm", "package": "x"}},
			{"name": "", "source": "./plugins/nameless"},
			{"name": "sourceless"},
			{"name": "bad-repository", "repository": "norepo"}
		]
	}`
	var m MarketplaceManifest
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if m.PluginRoot != "./plugins" {
		t.Errorf("PluginRoot = %q, want %q", m.PluginRoot, "./plugins")
	}

	byName := map[string]MarketplacePlugin{}
	for _, p := range m.Plugins {
		byName[p.Name] = p
	}

	if len(m.Plugins) != 3 {
		names := make([]string, 0, len(m.Plugins))
		for _, p := range m.Plugins {
			names = append(names, p.Name)
		}
		t.Fatalf("kept %d plugins %v, want 3 (claude-style, copilot-style, npm-kind-variant)", len(m.Plugins), names)
	}

	if _, ok := byName["claude-style"]; !ok {
		t.Error("claude-style plugin dropped")
	}

	cp, ok := byName["copilot-style"]
	if !ok {
		t.Fatal("copilot-style plugin dropped")
	}
	src, ok := cp.Source.(map[string]any)
	if !ok {
		t.Fatalf("copilot-style source = %T, want synthesized map", cp.Source)
	}
	if src["type"] != "github" || src["repo"] != "owner/repo" || src["ref"] != "v1.0.0" {
		t.Errorf("copilot-style synthesized source = %v", src)
	}

	// mkt-026 dual layer: kind-variant survives parsing.
	if _, ok := byName["npm-kind-variant"]; !ok {
		t.Error("npm-kind-variant must survive parse (rejected later at resolution, not here)")
	}
	for _, dropped := range []string{"npm-typed", "npm-source-key", "sourceless", "bad-repository"} {
		if _, ok := byName[dropped]; ok {
			t.Errorf("%s must be dropped at parse", dropped)
		}
	}
}
