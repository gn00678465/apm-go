package marketplace

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestLoadRegistry_MissingFileReturnsEmptySlice covers design.md's
// LoadRegistry contract: a registry file that does not exist yet is an
// empty marketplace list, not an error.
func TestLoadRegistry_MissingFileReturnsEmptySlice(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	// Act
	sources, err := LoadRegistry()

	// Assert
	if err != nil {
		t.Fatalf("LoadRegistry() on a missing file returned error: %v", err)
	}
	if len(sources) != 0 {
		t.Errorf("LoadRegistry() = %#v, want an empty slice", sources)
	}
}

// TestSaveRegistry_RoundTrip covers the atomic-write path (temp file +
// rename): what SaveRegistry writes, LoadRegistry must read back unchanged,
// including a fresh $APM_CONFIG_DIR that does not exist yet (mkt-002's
// "create directory, best-effort 0700" behavior).
func TestSaveRegistry_RoundTrip(t *testing.T) {
	// Arrange
	base := t.TempDir()
	configDir := filepath.Join(base, "not-yet-created", ".apm")
	t.Setenv("APM_CONFIG_DIR", configDir)
	want := []MarketplaceSource{
		{Name: "acme", URL: "https://github.com/acme/tools", Ref: "main", Path: "marketplace.json", Owner: "acme", Repo: "tools", Host: "github.com"},
	}

	// Act
	if err := SaveRegistry(want); err != nil {
		t.Fatalf("SaveRegistry() returned error: %v", err)
	}
	got, err := LoadRegistry()

	// Assert
	if err != nil {
		t.Fatalf("LoadRegistry() after SaveRegistry() returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRegistry() = %#v, want %#v", got, want)
	}
	p, err := RegistryPath()
	if err != nil {
		t.Fatalf("RegistryPath() returned error: %v", err)
	}
	if _, statErr := os.Stat(p); statErr != nil {
		t.Errorf("registry file does not exist at %q: %v", p, statErr)
	}
	// No leftover temp file from the atomic-write staging step.
	entries, err := os.ReadDir(filepath.Dir(p))
	if err != nil {
		t.Fatalf("ReadDir(%q): %v", filepath.Dir(p), err)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp") {
			t.Errorf("leftover temp file in registry dir: %q", e.Name())
		}
	}
}

// writeRegistryFixture writes a registry file directly (bypassing
// SaveRegistry) so tests exercise "a registry file that already existed
// with unrelated content", not just round-trips through this package's own
// writer (see marketplace-checklist.md "舊坑 1" / prd.md AC3). It still uses
// the wrapping {"marketplaces": [...]} shape (mkt-002) and MarketplaceSource's
// own MarshalJSON, since "bypassing SaveRegistry" means skipping its atomic
// temp-file dance, not writing an on-disk shape the package itself would
// never produce.
func writeRegistryFixture(t *testing.T, sources []MarketplaceSource) string {
	t.Helper()
	configDir := t.TempDir()
	t.Setenv("APM_CONFIG_DIR", configDir)
	data, err := json.MarshalIndent(registryDocument{Marketplaces: sources}, "", "  ")
	if err != nil {
		t.Fatalf("MarshalIndent fixture: %v", err)
	}
	p := filepath.Join(configDir, "marketplaces.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("WriteFile fixture: %v", err)
	}
	return p
}

// existingUnrelatedFixture is the "already has other marketplace entries"
// fixture AC3 requires every registry-write test path to exercise. Every
// entry uses the canonical (already-defaulted) field values Ref/Path/Host
// would hold after any read (LoadRegistry always fills in "main"/
// "marketplace.json"/"github.com" for an absent key, per A2 parity), so
// these fixtures stay stable across a write+read round trip regardless of
// source kind -- including unrelated-two, a local-path source, which still
// gets the same Ref/Host defaults Python's from_dict applies unconditionally.
func existingUnrelatedFixture() []MarketplaceSource {
	return []MarketplaceSource{
		{Name: "unrelated-one", URL: "https://github.com/foo/bar", Ref: "main", Path: "marketplace.json", Owner: "foo", Repo: "bar", Host: "github.com"},
		{Name: "unrelated-two", URL: "/abs/local/path", Ref: "main", Path: "marketplace.json", Host: "github.com"},
	}
}

// TestLoadRegistry_ParsesExistingFixture covers AC3's read side: a registry
// file that already exists on disk (not produced by this package) must
// parse back exactly as written.
func TestLoadRegistry_ParsesExistingFixture(t *testing.T) {
	// Arrange
	want := existingUnrelatedFixture()
	writeRegistryFixture(t, want)

	// Act
	got, err := LoadRegistry()

	// Assert
	if err != nil {
		t.Fatalf("LoadRegistry() returned error: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRegistry() = %#v, want %#v", got, want)
	}
}

// TestAddSource_PreservesUnrelatedEntries covers AC3's write side for the
// "brand new name" case: adding a marketplace to a registry that already
// has other, unrelated entries must not alter those entries.
func TestAddSource_PreservesUnrelatedEntries(t *testing.T) {
	// Arrange
	existing := existingUnrelatedFixture()
	writeRegistryFixture(t, existing)
	newEntry := MarketplaceSource{Name: "new-one", URL: "https://github.com/new/one", Ref: "main", Path: "marketplace.json", Owner: "new", Repo: "one", Host: "github.com"}

	// Act
	if err := AddSource(newEntry); err != nil {
		t.Fatalf("AddSource() returned error: %v", err)
	}
	got, err := LoadRegistry()

	// Assert
	if err != nil {
		t.Fatalf("LoadRegistry() returned error: %v", err)
	}
	want := append(append([]MarketplaceSource{}, existing...), newEntry)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRegistry() after AddSource() = %#v, want %#v", got, want)
	}
}

// TestAddSource_SameNameReplacesAndMovesToTail covers mkt-006 and A3: adding
// a marketplace whose name matches an existing entry (including a
// case-different match) silently replaces it -- no error, no confirmation
// prompt -- and the Python original's registry.py add_marketplace (:104-108)
// drops the old entry and appends the replacement, rather than swapping it
// in place. The pre-existing same-name entry deliberately starts first (not
// last), so a regression to in-place replacement cannot hide behind a
// coincidental position match.
func TestAddSource_SameNameReplacesAndMovesToTail(t *testing.T) {
	tests := []struct {
		name      string
		existing  string
		addAsName string
	}{
		{"exact case match", "acme", "acme"},
		{"different case matches too", "acme", "ACME"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			oldEntry := MarketplaceSource{Name: tt.existing, URL: "https://github.com/old/repo", Ref: "main", Path: "marketplace.json", Owner: "old", Repo: "repo", Host: "github.com"}
			existing := append([]MarketplaceSource{oldEntry}, existingUnrelatedFixture()...)
			writeRegistryFixture(t, existing)
			// Branch mirrors Ref explicitly here (not left as the zero value)
			// because MarshalJSON writes "branch" from Ref whenever Ref is
			// non-default, and LoadRegistry's UnmarshalJSON reads it back
			// into the Branch field -- so this is what a real round trip
			// through AddSource+LoadRegistry actually produces.
			replacement := MarketplaceSource{Name: tt.addAsName, URL: "https://github.com/new/repo", Ref: "v2", Path: "marketplace.json", Owner: "new", Repo: "repo", Host: "github.com", Branch: "v2"}

			// Act
			if err := AddSource(replacement); err != nil {
				t.Fatalf("AddSource() returned error: %v", err)
			}
			got, err := LoadRegistry()

			// Assert
			if err != nil {
				t.Fatalf("LoadRegistry() returned error: %v", err)
			}
			want := append(append([]MarketplaceSource{}, existingUnrelatedFixture()...), replacement)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("LoadRegistry() after AddSource() = %#v, want %#v (same-name add must drop the old entry and append the replacement to the tail, not swap it in place)", got, want)
			}
		})
	}
}

// TestFindByName_CaseInsensitive covers mkt-006's read-side counterpart:
// FindByName must match regardless of case.
func TestFindByName_CaseInsensitive(t *testing.T) {
	// Arrange
	writeRegistryFixture(t, []MarketplaceSource{
		{Name: "Acme-Tools", URL: "https://github.com/acme/tools", Ref: "main", Path: "marketplace.json", Owner: "acme", Repo: "tools", Host: "github.com"},
	})

	tests := []struct {
		name  string
		query string
	}{
		{"exact case", "Acme-Tools"},
		{"all lowercase", "acme-tools"},
		{"all uppercase", "ACME-TOOLS"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got, err := FindByName(tt.query)

			// Assert
			if err != nil {
				t.Fatalf("FindByName(%q) returned error: %v", tt.query, err)
			}
			if got == nil {
				t.Fatalf("FindByName(%q) = nil, want a match", tt.query)
			}
			if got.Name != "Acme-Tools" {
				t.Errorf("FindByName(%q).Name = %q, want %q", tt.query, got.Name, "Acme-Tools")
			}
		})
	}
}

// TestFindByName_NotFound covers the miss case: no matching name returns a
// nil source and no error (a "not registered" diagnostic is the caller's
// job, per design.md's CLI wiring in a later step).
func TestFindByName_NotFound(t *testing.T) {
	// Arrange
	writeRegistryFixture(t, existingUnrelatedFixture())

	// Act
	got, err := FindByName("does-not-exist")

	// Assert
	if err != nil {
		t.Fatalf("FindByName() returned error: %v", err)
	}
	if got != nil {
		t.Errorf("FindByName() = %#v, want nil", got)
	}
}

// TestRemoveSource_CaseInsensitive covers mkt-006's removal counterpart.
func TestRemoveSource_CaseInsensitive(t *testing.T) {
	// Arrange
	writeRegistryFixture(t, []MarketplaceSource{
		{Name: "Acme-Tools", URL: "https://github.com/acme/tools", Ref: "main", Path: "marketplace.json", Owner: "acme", Repo: "tools", Host: "github.com"},
	})

	// Act
	err := RemoveSource("ACME-tools")

	// Assert
	if err != nil {
		t.Fatalf("RemoveSource() returned error: %v", err)
	}
	got, loadErr := LoadRegistry()
	if loadErr != nil {
		t.Fatalf("LoadRegistry() returned error: %v", loadErr)
	}
	if len(got) != 0 {
		t.Errorf("LoadRegistry() after RemoveSource() = %#v, want empty", got)
	}
}

// TestRemoveSource_PreservesUnrelatedEntries covers AC3's write side for
// RemoveSource: removing one marketplace must not disturb the other,
// unrelated entries already in the registry.
func TestRemoveSource_PreservesUnrelatedEntries(t *testing.T) {
	// Arrange
	existing := existingUnrelatedFixture()
	writeRegistryFixture(t, existing)

	// Act
	if err := RemoveSource("unrelated-one"); err != nil {
		t.Fatalf("RemoveSource() returned error: %v", err)
	}
	got, err := LoadRegistry()

	// Assert
	if err != nil {
		t.Fatalf("LoadRegistry() returned error: %v", err)
	}
	want := existing[1:]
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadRegistry() after RemoveSource() = %#v, want %#v", got, want)
	}
}

// TestRemoveSource_NotFoundReturnsError covers the checklist's explicit
// requirement: removing a name that is not registered is an error, not a
// silent no-op.
func TestRemoveSource_NotFoundReturnsError(t *testing.T) {
	// Arrange
	writeRegistryFixture(t, existingUnrelatedFixture())

	// Act
	err := RemoveSource("does-not-exist")

	// Assert
	if err == nil {
		t.Fatalf("RemoveSource() returned no error, want one for an unregistered name")
	}
}

// TestSaveRegistry_WritesWrappingObjectShape covers A1 (mkt-002): the
// on-disk registry format must be a wrapping {"marketplaces": [...]} object,
// not a bare top-level array, so apm-go and the Python original can share a
// single ~/.apm/marketplaces.json without either CLI failing to parse the
// other's writes.
func TestSaveRegistry_WritesWrappingObjectShape(t *testing.T) {
	// Arrange
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	sources := []MarketplaceSource{
		{Name: "acme", URL: "https://github.com/acme/tools", Ref: "main", Path: "marketplace.json", Owner: "acme", Repo: "tools", Host: "github.com"},
	}

	// Act
	if err := SaveRegistry(sources); err != nil {
		t.Fatalf("SaveRegistry() returned error: %v", err)
	}
	p, err := RegistryPath()
	if err != nil {
		t.Fatalf("RegistryPath() returned error: %v", err)
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("ReadFile(%q): %v", p, err)
	}

	// Assert: top-level JSON value is an object with a "marketplaces" key,
	// not a bare array.
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("registry file is not a JSON object: %v (content: %s)", err, raw)
	}
	if _, ok := doc["marketplaces"]; !ok {
		t.Errorf("registry file %s has no top-level \"marketplaces\" key", raw)
	}
	var bareArray []json.RawMessage
	if err := json.Unmarshal(raw, &bareArray); err == nil {
		t.Errorf("registry file parsed as a bare top-level array, want a wrapping object: %s", raw)
	}
}

// TestMarketplaceSourceJSON_RoundTrip covers A2: MarketplaceSource's
// Marshal/UnmarshalJSON must align field-for-field with the Python
// original's MarketplaceSource.to_dict/from_dict (models.py:243-287) --
// default values (ref "main", path "marketplace.json", host "github.com")
// are omitted from the JSON entirely, while a meaningful non-default value
// (including an explicit empty path, the url-kind direct-manifest-URL
// marker) is always written back out and round-trips exactly.
func TestMarketplaceSourceJSON_RoundTrip(t *testing.T) {
	tests := []struct {
		name       string
		src        MarketplaceSource
		wantFields map[string]bool
	}{
		{
			name:       "github source with every default value omitted",
			src:        MarketplaceSource{Name: "acme", URL: "https://github.com/acme/tools", Ref: "main", Path: "marketplace.json", Owner: "acme", Repo: "tools", Host: "github.com"},
			wantFields: map[string]bool{"name": true, "url": true, "owner": true, "repo": true},
		},
		{
			name:       "url-kind source with an explicit empty path is preserved",
			src:        MarketplaceSource{Name: "hosted", URL: "https://example.com/repo/marketplace.json", Ref: "main", Path: "", Host: "example.com"},
			wantFields: map[string]bool{"name": true, "url": true, "path": true, "host": true},
		},
		{
			name: "custom host and non-default ref are both written, branch mirrors ref",
			// Branch is set explicitly to mirror Ref (not left at the zero
			// value) because that is what MarshalJSON actually derives it
			// from -- see MarshalJSON's doc comment.
			src:        MarketplaceSource{Name: "ghes", URL: "https://ghes.example.com/acme/tools", Ref: "v2", Path: "marketplace.json", Owner: "acme", Repo: "tools", Host: "ghes.example.com", Branch: "v2"},
			wantFields: map[string]bool{"name": true, "url": true, "ref": true, "owner": true, "repo": true, "host": true, "branch": true},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			data, err := json.Marshal(tt.src)
			if err != nil {
				t.Fatalf("json.Marshal() returned error: %v", err)
			}
			var raw map[string]json.RawMessage
			if err := json.Unmarshal(data, &raw); err != nil {
				t.Fatalf("json.Unmarshal(marshaled) returned error: %v", err)
			}

			// Assert: exactly the expected keys are present, no more, no less.
			for key := range tt.wantFields {
				if _, ok := raw[key]; !ok {
					t.Errorf("marshaled JSON %s is missing key %q", data, key)
				}
			}
			for key := range raw {
				if !tt.wantFields[key] {
					t.Errorf("marshaled JSON %s has unexpected key %q", data, key)
				}
			}

			var got MarketplaceSource
			if err := json.Unmarshal(data, &got); err != nil {
				t.Fatalf("json.Unmarshal() returned error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.src) {
				t.Errorf("round trip = %#v, want %#v", got, tt.src)
			}
		})
	}
}

// TestMarketplaceSourceJSON_ParsesPythonOriginalFixtures embeds literal JSON
// shaped exactly like the Python original's own writes (registry.py's
// {"marketplaces": [...]} wrapper plus MarketplaceSource.to_dict's field
// omission rules), so a registry file the Python CLI actually produced --
// not just one apm-go itself wrote -- parses correctly. Covers both the
// minimal shape (only name+url, everything else defaulted) and the full
// shape (every optional field present, including "branch" as a legacy
// mirror of "ref").
func TestMarketplaceSourceJSON_ParsesPythonOriginalFixtures(t *testing.T) {
	tests := []struct {
		name string
		json string
		want MarketplaceSource
	}{
		{
			name: "minimal shape: only name and url",
			json: `{"marketplaces":[{"name":"x","url":"https://github.com/o/r"}]}`,
			want: MarketplaceSource{Name: "x", URL: "https://github.com/o/r", Ref: "main", Path: "marketplace.json", Host: "github.com"},
		},
		{
			name: "full shape: ref, path, owner, repo, host, branch all present",
			json: `{"marketplaces":[{"name":"acme","url":"https://ghe.example.com/acme/tools","ref":"v1.2.3","path":".github/plugin/marketplace.json","owner":"acme","repo":"tools","host":"ghe.example.com","branch":"v1.2.3"}]}`,
			want: MarketplaceSource{Name: "acme", URL: "https://ghe.example.com/acme/tools", Ref: "v1.2.3", Path: ".github/plugin/marketplace.json", Owner: "acme", Repo: "tools", Host: "ghe.example.com", Branch: "v1.2.3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			configDir := t.TempDir()
			t.Setenv("APM_CONFIG_DIR", configDir)
			p := filepath.Join(configDir, "marketplaces.json")
			if err := os.WriteFile(p, []byte(tt.json), 0o644); err != nil {
				t.Fatalf("WriteFile fixture: %v", err)
			}

			// Act
			got, err := LoadRegistry()

			// Assert
			if err != nil {
				t.Fatalf("LoadRegistry() returned error: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("LoadRegistry() = %d entries, want 1", len(got))
			}
			if !reflect.DeepEqual(got[0], tt.want) {
				t.Errorf("LoadRegistry()[0] = %#v, want %#v", got[0], tt.want)
			}
		})
	}
}
