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
// synthesis, nameless/sourceless drops, and npm rejection under any of the
// three discriminator keys ("type"/"source"/"kind" -- all dropped at parse
// time, see TestParseManifestPlugins_NPMRejectedForAllThreeDiscriminatorKeys
// in resolver_test.go for the ticket 11 attempt 2 correction of an earlier,
// incorrect "kind survives parsing" claim here).
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

	if len(m.Plugins) != 2 {
		names := make([]string, 0, len(m.Plugins))
		for _, p := range m.Plugins {
			names = append(names, p.Name)
		}
		t.Fatalf("kept %d plugins %v, want 2 (claude-style, copilot-style)", len(m.Plugins), names)
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

	// Ticket 11 attempt 2 correction: _parse_plugin_entry's own source_type
	// derivation (models.py:447-454) reads "type", "source", AND "kind" --
	// all three -- so "kind: npm" is rejected at PARSE time in the real
	// Oracle too, exactly like "type: npm"/"source: npm". There is no
	// parse-vs-resolve split for npm detection; mkt-026's "two-vs-three-key"
	// premise did not hold up against a full reading of models.py.
	for _, dropped := range []string{"npm-typed", "npm-source-key", "npm-kind-variant", "sourceless", "bad-repository"} {
		if _, ok := byName[dropped]; ok {
			t.Errorf("%s must be dropped at parse", dropped)
		}
	}
}

// TestMarketplaceManifest_TolerantShapes covers the real-world shapes
// Python's parse_marketplace_json (models.py:454-515) tolerates that a
// naive strictly-typed decode would hard-fail the whole document on:
// "plugins" not being an array (warned, treated as empty per :491-497), a
// non-object element inside "plugins" (skipped per :501-502), "tags" not
// being an array (coerced to empty per :367), and non-string
// "version"/"metadata"/pluginRoot values (ignored via Python's tolerant
// .get/isinstance checks).
func TestMarketplaceManifest_TolerantShapes(t *testing.T) {
	tests := []struct {
		name        string
		doc         string
		wantPlugins int
		check       func(t *testing.T, m MarketplaceManifest)
	}{
		{
			name: "plugins is an object, not an array -> treated as empty",
			doc:  `{"name":"m","plugins":{"a":1}}`,
		},
		{
			name: "plugins is a string, not an array -> treated as empty",
			doc:  `{"name":"m","plugins":"oops"}`,
		},
		{
			name: "plugins is a number, not an array -> treated as empty",
			doc:  `{"name":"m","plugins":42}`,
		},
		{
			name:        "a non-object plugin element is skipped, siblings kept",
			doc:         `{"name":"m","plugins":[{"name":"a","source":"./a"},"not-an-object",42,{"name":"b","source":"./b"}]}`,
			wantPlugins: 2,
		},
		{
			name:        "tags is a string, not an array -> coerced to empty",
			doc:         `{"name":"m","plugins":[{"name":"a","source":"./a","tags":"foo"}]}`,
			wantPlugins: 1,
			check: func(t *testing.T, m MarketplaceManifest) {
				if len(m.Plugins[0].Tags) != 0 {
					t.Errorf("Tags = %#v, want empty", m.Plugins[0].Tags)
				}
			},
		},
		{
			name:        "version is a number -> ignored, plugin still kept",
			doc:         `{"name":"m","plugins":[{"name":"a","source":"./a","version":1.0}]}`,
			wantPlugins: 1,
			check: func(t *testing.T, m MarketplaceManifest) {
				if m.Plugins[0].Version != "" {
					t.Errorf("Version = %q, want empty (non-string value ignored)", m.Plugins[0].Version)
				}
			},
		},
		{
			name: "metadata is a string, not an object -> pluginRoot stays empty",
			doc:  `{"name":"m","metadata":"x"}`,
			check: func(t *testing.T, m MarketplaceManifest) {
				if m.PluginRoot != "" {
					t.Errorf("PluginRoot = %q, want empty", m.PluginRoot)
				}
			},
		},
		{
			name: "metadata.pluginRoot is a number -> ignored",
			doc:  `{"name":"m","metadata":{"pluginRoot":5}}`,
			check: func(t *testing.T, m MarketplaceManifest) {
				if m.PluginRoot != "" {
					t.Errorf("PluginRoot = %q, want empty", m.PluginRoot)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m MarketplaceManifest
			if err := json.Unmarshal([]byte(tt.doc), &m); err != nil {
				t.Fatalf("unmarshal returned an error for a tolerated shape: %v", err)
			}
			if len(m.Plugins) != tt.wantPlugins {
				names := make([]string, 0, len(m.Plugins))
				for _, p := range m.Plugins {
					names = append(names, p.Name)
				}
				t.Fatalf("len(Plugins) = %d %v, want %d", len(m.Plugins), names, tt.wantPlugins)
			}
			if tt.check != nil {
				tt.check(t, m)
			}
		})
	}
}

// TestMarketplaceManifest_PluginSourceWrongType_Dropped covers the reverse
// over-tolerance bug: a plugin whose "source" is present but neither a
// string nor an object (a number, array, or bool) must be dropped at parse
// time, mirroring Python's _parse_plugin_entry "unrecognized source
// format" branch (models.py:387-389) -- previously Go kept these entries
// alive with a non-string/non-map Source value.
func TestMarketplaceManifest_PluginSourceWrongType_Dropped(t *testing.T) {
	doc := `{"name":"m","plugins":[
		{"name":"bad-number-source","source":42},
		{"name":"bad-array-source","source":[1,2,3]},
		{"name":"bad-bool-source","source":true},
		{"name":"good","source":"./good"}
	]}`
	var m MarketplaceManifest
	if err := json.Unmarshal([]byte(doc), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(m.Plugins) != 1 || m.Plugins[0].Name != "good" {
		names := make([]string, 0, len(m.Plugins))
		for _, p := range m.Plugins {
			names = append(names, p.Name)
		}
		t.Fatalf("kept plugins %v, want only [good]", names)
	}
}

// TestMarketplaceManifest_StructuralErrors covers ticket 11's two
// diagnostics: Python's parse_marketplace_json (models.py:589-609) records
// "plugins: expected a list" when the "plugins" key is present but not a
// JSON array (including explicit null, since isinstance(None, list) is
// False) and "plugins[N]: expected an object" per non-object element,
// while a wholly ABSENT "plugins" key is not an error either side.
func TestMarketplaceManifest_StructuralErrors(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want []string
	}{
		{
			name: "plugins absent is not a structural error",
			doc:  `{"name":"m"}`,
			want: nil,
		},
		{
			name: "plugins is an object, not an array",
			doc:  `{"name":"m","plugins":{"a":1}}`,
			want: []string{"plugins: expected a list"},
		},
		{
			name: "plugins is a string, not an array",
			doc:  `{"name":"m","plugins":"oops"}`,
			want: []string{"plugins: expected a list"},
		},
		{
			name: "plugins is a number, not an array",
			doc:  `{"name":"m","plugins":42}`,
			want: []string{"plugins: expected a list"},
		},
		{
			name: "plugins is explicit null",
			doc:  `{"name":"m","plugins":null}`,
			want: []string{"plugins: expected a list"},
		},
		{
			name: "plugins is an empty array is not a structural error",
			doc:  `{"name":"m","plugins":[]}`,
			want: nil,
		},
		{
			name: "non-object plugin elements are named by index, valid siblings kept",
			doc:  `{"name":"m","plugins":[{"name":"a","source":"./a"},"not-an-object",42,{"name":"b","source":"./b"}]}`,
			want: []string{"plugins[1]: expected an object", "plugins[2]: expected an object"},
		},
		// Ticket 11 eval attempt 2 blocking reproducer 1: a JSON null array
		// element unmarshals into a nil map with NO decode error, which is
		// indistinguishable from "not an object" unless checked explicitly
		// (the actual attempt-1 bug: it read as a valid, zero-key object).
		{
			name: "eval reproducer 1: a null plugin element is not an object",
			doc:  `{"name":"m","plugins":[null]}`,
			want: []string{"plugins[0]: expected an object"},
		},
		// Ticket 11 eval attempt 2 blocking reproducer 2: an empty object IS
		// a valid JSON object (unlike null), so it reaches per-field
		// validation and fails on the first field Python itself checks:
		// name.
		{
			name: "eval reproducer 2: an empty object plugin element fails on name",
			doc:  `{"name":"m","plugins":[{}]}`,
			want: []string{"plugins[0].name: expected a non-empty string"},
		},
		{
			name: "name present but blank after trim",
			doc:  `{"name":"m","plugins":[{"name":"   ","source":"./a"}]}`,
			want: []string{"plugins[0].name: expected a non-empty string"},
		},
		{
			name: "name present but not a string",
			doc:  `{"name":"m","plugins":[{"name":42,"source":"./a"}]}`,
			want: []string{"plugins[0].name: expected a non-empty string"},
		},
		{
			name: "neither source nor repository present",
			doc:  `{"name":"m","plugins":[{"name":"a"}]}`,
			want: []string{"plugins[0].source: expected a source or repository field"},
		},
		{
			name: "source explicitly null (must not silently fall back to a repository check)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":null,"repository":"owner/repo"}]}`,
			want: []string{"plugins[0].source: expected a string or object"},
		},
		{
			name: "source is a number",
			doc:  `{"name":"m","plugins":[{"name":"a","source":42}]}`,
			want: []string{"plugins[0].source: expected a string or object"},
		},
		{
			name: "source is an array",
			doc:  `{"name":"m","plugins":[{"name":"a","source":[1,2,3]}]}`,
			want: []string{"plugins[0].source: expected a string or object"},
		},
		{
			name: "source is a bool",
			doc:  `{"name":"m","plugins":[{"name":"a","source":true}]}`,
			want: []string{"plugins[0].source: expected a string or object"},
		},
		{
			name: "repository present but not a string",
			doc:  `{"name":"m","plugins":[{"name":"a","repository":42}]}`,
			want: []string{"plugins[0].repository: expected an owner/repository string"},
		},
		{
			name: "repository present but no slash",
			doc:  `{"name":"m","plugins":[{"name":"a","repository":"no-slash"}]}`,
			want: []string{"plugins[0].repository: expected an owner/repository string"},
		},
		{
			name: "repository valid, synthesizes a github source (no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"a","repository":"owner/repo"}]}`,
			want: nil,
		},
		{
			name: "dict source: npm via type key",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"npm","package":"x"}}]}`,
			want: []string{"plugins[0].source: unsupported source type 'npm'"},
		},
		{
			name: "dict source: npm via source key",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"source":"npm","package":"x"}}]}`,
			want: []string{"plugins[0].source: unsupported source type 'npm'"},
		},
		{
			name: "dict source: npm via kind key (ticket 11 attempt 2: all three keys reject npm at parse time)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"kind":"npm","package":"x"}}]}`,
			want: []string{"plugins[0].source: unsupported source type 'npm'"},
		},
		{
			name: "dict source: npm is case-insensitive",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"NPM"}}]}`,
			want: []string{"plugins[0].source: unsupported source type 'npm'"},
		},
		{
			name: "dict source: no type and no repo",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{}}]}`,
			want: []string{"plugins[0].source: expected a supported source type or an owner/repository field"},
		},
		{
			name: "dict source: no recognized type, but a valid repo (implicit github, no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"repo":"owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "dict source: unsupported type with an otherwise-valid repo",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"svn","repo":"owner/repo"}}]}`,
			want: []string{"plugins[0].source: unsupported source type 'svn'"},
		},
		{
			name: "dict source: github missing repo",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"github"}}]}`,
			want: []string{"plugins[0].source: github requires an owner/repository field"},
		},
		{
			name: "dict source: github repo is a local path",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"github","repo":"./local/path"}}]}`,
			want: []string{"plugins[0].source: github requires a valid non-local owner/repository field"},
		},
		{
			name: "dict source: github valid (no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"github","repo":"owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "dict source: url missing url field",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"url"}}]}`,
			want: []string{"plugins[0].source: url requires a non-empty url field"},
		},
		{
			name: "dict source: url is a local path",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"url","url":"./local/path"}}]}`,
			want: []string{"plugins[0].source: url requires a valid non-local url field"},
		},
		{
			name: "dict source: url valid (no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"url","url":"https://example.com/owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "dict source: git-subdir missing both repo and url",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"git-subdir"}}]}`,
			want: []string{"plugins[0].source: git-subdir requires an owner/repository or url field"},
		},
		{
			name: "dict source: git-subdir repo is a local path",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"git-subdir","repo":"./local"}}]}`,
			want: []string{"plugins[0].source: git-subdir requires a valid non-local owner/repository or url field"},
		},
		{
			name: "dict source: git-subdir valid via repo (no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"git-subdir","repo":"owner/repo","subdir":"pkg/a"}}]}`,
			want: nil,
		},
		{
			name: "dict source: gitlab missing both repo and url",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"gitlab"}}]}`,
			want: []string{"plugins[0].source: gitlab requires an owner/repository or url field"},
		},
		{
			name: "dict source: gitlab valid via url (no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"a","source":{"type":"gitlab","url":"https://gitlab.example.com/owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "multiple malformed entries accumulate, in index order (not first-error-stops)",
			doc:  `{"name":"m","plugins":[{},{"name":"a","source":{"type":"npm"}},{"name":"b","source":"./ok"}]}`,
			want: []string{"plugins[0].name: expected a non-empty string", "plugins[1].source: unsupported source type 'npm'"},
		},

		// Ticket 11 eval attempt 3: isValidRemoteCoordinate's coordinate-
		// grammar reproducers -- each syntactically invalid, but each was
		// previously accepted by the bounded (non-empty/non-local/no-control-
		// chars) approximation. All 4 verified against the real pinned
		// Oracle's parse_marketplace_json directly.
		{
			name: "eval attempt 3 reproducer: github repo with a trailing slash (empty repo segment)",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/"}}]}`,
			want: []string{"plugins[0].source: github requires a valid non-local owner/repository field"},
		},
		{
			name: "eval attempt 3 divergence-class probe: github repo with a doubled slash",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner//repo"}}]}`,
			want: []string{"plugins[0].source: github requires a valid non-local owner/repository field"},
		},
		{
			name: "eval attempt 3 divergence-class probe: github repo with a query string",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo?x"}}]}`,
			want: []string{"plugins[0].source: github requires a valid non-local owner/repository field"},
		},
		{
			name: "eval attempt 3 divergence-class probe: url is a bare word (not a valid dependency reference at all)",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"url","url":"foo"}}]}`,
			want: []string{"plugins[0].source: url requires a valid non-local url field"},
		},
		{
			name: "coordinate grammar still accepts every previously-valid shape (HTTPS URL, SCP SSH, host-qualified)",
			doc: `{"name":"m","plugins":[
				{"name":"a","source":{"type":"github","repo":"https://github.com/owner/repo"}},
				{"name":"b","source":{"type":"github","repo":"git@github.com:owner/repo.git"}},
				{"name":"c","source":{"type":"github","repo":"github.com/owner/repo"}}
			]}`,
			want: nil,
		},

		// Ticket 11 eval attempt 3: repo/repository fallback truthiness
		// reproducer -- Python's `raw.get("repo","") or raw.get("repository","")`
		// depends on the "repo" value's OWN truthiness, not "is it a string".
		{
			name: "eval attempt 3 reproducer: truthy non-string repo is used as-is, never falls back to repository",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":42,"repository":"owner/repo"}}]}`,
			want: []string{"plugins[0].source: github requires an owner/repository field"},
		},
		{
			name: "falsy empty-string repo DOES fall back to repository (valid)",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"","repository":"owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "falsy zero repo DOES fall back to repository (valid)",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":0,"repository":"owner/repo"}}]}`,
			want: nil,
		},

		// Ticket 11 eval attempt 3: tag_pattern deferral reproducer -- a
		// malformed tag_pattern is a per-element Structure diagnostic, not a
		// whole-document parse failure (see also
		// TestUnmarshalJSON_PluginSourceTagPattern for the marketplace-add/
		// browse/install blast-radius coverage).
		{
			name: "eval attempt 3 reproducer: tag_pattern wrong placeholder count",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":"{name}"}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' must contain exactly one {version} placeholder, got '{name}'"},
		},
		{
			name: "tag_pattern non-string",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":42}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' must be a non-empty string, got 42"},
		},
		{
			name: "tag_pattern unsupported placeholder",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":"{foo}"}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' contains unsupported placeholder(s): {foo}"},
		},
		{
			name: "tag_pattern valid (no structural error)",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":"{name}-v{version}"}}]}`,
			want: nil,
		},

		// Ticket 11 eval attempt 4 (orchestrator intervention): three new
		// reproducers, all verified directly against the pinned Oracle.
		{
			name: "eval attempt 4 reproducer 1: FQDN host gate -- url source with a non-FQDN host",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"url","url":"https://x/owner/repo"}}]}`,
			want: []string{"plugins[0].source: url requires a valid non-local url field"},
		},
		{
			name: "eval attempt 4 reproducer 1 (accept side): percent-decode -- github repo with a percent-encoded character is accepted",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/%72epo"}}]}`,
			want: nil,
		},
		{
			name: "eval attempt 4 reproducer 2: dict-shaped tag_pattern reprs with insertion order preserved",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":{"x":1}}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' must be a non-empty string, got {'x': 1}"},
		},
		{
			name: "eval attempt 4 reproducer 2 (multi-key): dict-shaped tag_pattern preserves ORIGINAL key order, not alphabetical",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":{"b":1,"a":2}}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' must be a non-empty string, got {'b': 1, 'a': 2}"},
		},

		// Ticket 11 eval attempt 5 (orchestrator strategy change: five
		// reproducers from '## Attempt 4' of the eval, all verified
		// byte-for-byte against the pinned Oracle directly).
		{
			name: "eval attempt 5 reproducer 1: ssh:// url source with an arbitrary (non-git) user is accepted",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"url","url":"ssh://alice@host/owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "eval attempt 5 reproducer 1 (SCP form): arbitrary user is accepted",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"url","url":"alice@host:owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "eval attempt 5 reproducer 2: url query string is stripped, not treated as part of the repo path",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"url","url":"https://x.io/owner/repo?x"}}]}`,
			want: nil,
		},
		{
			name: "eval attempt 5 reproducer 2 (shorthand host:port): port is split off before FQDN validation",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"x.io:443/owner/repo"}}]}`,
			want: nil,
		},
		{
			name: "eval attempt 5 reproducer 3: lone UTF-16 surrogate in tag_pattern survives repr byte-for-byte",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":"\ud800"}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' must contain exactly one {version} placeholder, got '\\ud800'"},
		},
		{
			name: "eval attempt 5 reproducer 4: float lexeme overflow becomes Python inf, not the raw lexeme",
			doc:  `{"name":"m","plugins":[{"name":"big","source":{"type":"github","repo":"owner/repo","tag_pattern":1e400}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'big' source.tag_pattern' must be a non-empty string, got inf"},
		},
		{
			name: "eval attempt 5 reproducer 5: duplicate tag_pattern dict keys keep first position, last value",
			doc:  `{"name":"m","plugins":[{"name":"p","source":{"type":"github","repo":"owner/repo","tag_pattern":{"a":1,"b":2,"a":3}}}]}`,
			want: []string{"plugins[0].source.tag_pattern: 'Plugin 'p' source.tag_pattern' must be a non-empty string, got {'a': 3, 'b': 2}"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var m MarketplaceManifest
			if err := json.Unmarshal([]byte(tt.doc), &m); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(m.StructuralErrors) != len(tt.want) {
				t.Fatalf("StructuralErrors = %v, want %v", m.StructuralErrors, tt.want)
			}
			for i, w := range tt.want {
				if m.StructuralErrors[i] != w {
					t.Errorf("StructuralErrors[%d] = %q, want %q", i, m.StructuralErrors[i], w)
				}
			}
		})
	}
}

// TestUnmarshalJSON_PluginSourceTagPattern locks the consumer half of upstream
// v0.27.0's tag_pattern propagation (models.py:325-330 field, :521-533 parse).
//
// Ticket 11 attempt 3 correction: an earlier version of this test (and its
// own doc comment) asserted an invalid tag_pattern fails the WHOLE
// document, believing _parse_plugin_entry's caller at models.py:531 lets
// TagPatternError propagate uncaught. Verified directly against the pinned
// Oracle's parse_marketplace_json: models.py:521-533 wraps the
// validate_tag_pattern call in `try: ... except TagPatternError as exc:
// return None, f"source.tag_pattern: {exc}"` -- the exact same
// skip-with-diagnostic shape as every other _parse_plugin_entry branch. A
// malformed tag_pattern is dropped from manifest.plugins and reported in
// structural_errors (visible via `apm marketplace validate`'s Structure
// check); it does NOT fail `marketplace add`/fetch for the whole manifest.
func TestUnmarshalJSON_PluginSourceTagPattern(t *testing.T) {
	t.Run("present and valid is carried through", func(t *testing.T) {
		var m MarketplaceManifest
		err := json.Unmarshal([]byte(`{"name":"mk","plugins":[
			{"name":"a","source":{"source":"github","repo":"acme/a","tag_pattern":"{name}-v{version}"}}
		]}`), &m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Plugins[0].TagPattern != "{name}-v{version}" {
			t.Errorf("TagPattern = %q, want %q", m.Plugins[0].TagPattern, "{name}-v{version}")
		}
	})

	t.Run("absent stays empty rather than defaulted", func(t *testing.T) {
		var m MarketplaceManifest
		err := json.Unmarshal([]byte(`{"name":"mk","plugins":[
			{"name":"a","source":{"source":"github","repo":"acme/a"}}
		]}`), &m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Upstream models the absent key as None and says so explicitly: it
		// means "old marketplace.json", and the resolver -- not the parser --
		// supplies the default.
		if m.Plugins[0].TagPattern != "" {
			t.Errorf("TagPattern = %q, want \"\" for an absent key", m.Plugins[0].TagPattern)
		}
	})

	t.Run("string source is unaffected", func(t *testing.T) {
		var m MarketplaceManifest
		err := json.Unmarshal([]byte(`{"name":"mk","plugins":[{"name":"a","source":"./local"}]}`), &m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if m.Plugins[0].TagPattern != "" {
			t.Errorf("TagPattern = %q, want \"\"", m.Plugins[0].TagPattern)
		}
	})

	t.Run("invalid is dropped per-entry and reported in StructuralErrors, siblings kept", func(t *testing.T) {
		var m MarketplaceManifest
		err := json.Unmarshal([]byte(`{"name":"mk","plugins":[
			{"name":"good","source":{"source":"github","repo":"acme/g"}},
			{"name":"bad","source":{"source":"github","repo":"acme/b","tag_pattern":"{name}"}}
		]}`), &m)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(m.Plugins) != 1 || m.Plugins[0].Name != "good" {
			names := make([]string, 0, len(m.Plugins))
			for _, p := range m.Plugins {
				names = append(names, p.Name)
			}
			t.Fatalf("kept plugins %v, want only [good] (the malformed entry is dropped, not the whole document)", names)
		}
		want := "plugins[1].source.tag_pattern: 'Plugin 'bad' source.tag_pattern' must contain exactly one {version} placeholder, got '{name}'"
		if len(m.StructuralErrors) != 1 || m.StructuralErrors[0] != want {
			t.Errorf("StructuralErrors = %v, want [%q]", m.StructuralErrors, want)
		}
	})
}

// TestPythonReprValue pins pythonReprValue/pythonReprNumber/pythonReprString
// against the pinned Oracle's own repr() output (ticket 11 attempt 4:
// `python3 -c "print(repr(x))"` run once per case to capture the expected
// bytes, not recomputed the way the port itself computes them). Covers
// every branch decodeOrderedJSON's doc comment names: dict insertion order
// (not alphabetical), list element order, integer-vs-float JSON lexemes
// (including a whole-number float needing ".0" Go's FormatFloat omits, and
// large/small magnitudes needing scientific notation), and string quote/
// escape selection.
func TestPythonReprValue(t *testing.T) {
	tests := []struct {
		name string
		json string
		want string
	}{
		{"dict single key", `{"x": 1}`, `{'x': 1}`},
		{"dict preserves insertion order, not alphabetical", `{"b": 1, "a": 2}`, `{'b': 1, 'a': 2}`},
		{"list of mixed types", `[1, "a", null, true]`, `[1, 'a', None, True]`},
		{"positive integer", `42`, `42`},
		{"negative integer", `-7`, `-7`},
		{"zero integer", `0`, `0`},
		{"whole-number float keeps .0", `1.0`, `1.0`},
		{"float with fraction", `1.5`, `1.5`},
		{"negative float", `-0.5`, `-0.5`},
		{"float many digits", `3.14159`, `3.14159`},
		{"large float uses scientific notation", `1e20`, `1e+20`},
		{"small float uses scientific notation", `1e-5`, `1e-05`},
		{"null", `null`, `None`},
		{"true", `true`, `True`},
		{"false", `false`, `False`},
		{"empty string", `""`, `''`},
		{"plain string", `"hello"`, `'hello'`},
		{"string with single quote uses double quotes", `"it's"`, `"it's"`},
		{"string with double quote uses single quotes", `"say \"hi\""`, `'say "hi"'`},
		{"string with both quotes escapes the single quote", `"both ' and \""`, `'both \' and "'`},
		{"string with tab", `"tab\there"`, `'tab\there'`},
		{"string with control char", `"\u0007"`, `'\x07'`},
		{"string with non-ASCII printable stays literal", `"héllo"`, `'héllo'`},
		{"string with newline", `"line\nbreak"`, `'line\nbreak'`},

		// Ticket 11 eval attempt 4 (orchestrator intervention round 2):
		// five new reproducers, all verified byte-for-byte against the
		// pinned Oracle directly.
		{"lone UTF-16 surrogate survives repr, not replaced with U+FFFD", `"\ud800"`, `'\ud800'`},
		{"float lexeme overflow becomes Python +inf", `1e400`, `inf`},
		{"float lexeme overflow becomes Python -inf", `-1e400`, `-inf`},
		{"integer -0 normalizes to 0 (Python int semantics)", `-0`, `0`},
		{"float -0.0 keeps its sign (distinct from integer -0)", `-0.0`, `-0.0`},
		{"duplicate object keys: first position, last value wins", `{"a":1,"b":2,"a":3}`, `{'a': 3, 'b': 2}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := decodeOrderedJSON(json.RawMessage(tt.json))
			if err != nil {
				t.Fatalf("decodeOrderedJSON(%s): %v", tt.json, err)
			}
			if got := pythonReprValue(v); got != tt.want {
				t.Errorf("pythonReprValue(%s) = %q, want %q", tt.json, got, tt.want)
			}
		})
	}
}

// TestPrintableASCIIText pins printableASCIIText against
// diagnostics.py:52-55's printable_ascii_text, verified directly (ticket 11
// attempt 4): a non-ASCII printable character that pythonReprString leaves
// literal (e.g. 'é') is squashed to a single '?' here, same as any
// remaining ASCII control character/DEL.
func TestPrintableASCIIText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hello", "hello"},
		{"héllo", "h?llo"},
		{"tab\ttab", "tab?tab"},
		{"\x1b[31m", "?[31m"},
		{"a\x7fb", "a?b"},
	}
	for _, tt := range tests {
		if got := printableASCIIText(tt.in); got != tt.want {
			t.Errorf("printableASCIIText(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
