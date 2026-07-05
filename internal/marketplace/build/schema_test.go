// Tests for this sub-task's step 6 / prd.md's AC2: running the upstream
// Claude Code marketplace.json JSON Schema (informational, mkt-052's
// tests/fixtures/schemas/claude-code-marketplace.schema.json) against
// ClaudeMapper's actual composed output for a marketplace.packages[] mix of
// local and every remote source shape composeRemoteSource produces (github
// shorthand, git-subdir, non-default-host url) -- proving the Go output
// stays inside the upstream schema's field shapes, not just that it "looks
// right" by eye.
//
// The schema is embedded as this package's own testdata/ copy rather than
// read from the sibling Python apm repo's tests/fixtures/schemas/ path, so
// this test never depends on that repo being checked out alongside apm-go.
//
// Codex's output is deliberately never validated against this schema: its
// shape is a materially different, Codex-only document (mkt-052/053,
// codexmapper.go's own doc comment) that upstream's Claude Code marketplace
// schema was never written to describe (mkt-052: "schema 本身...是
// informational...Go 版本只需相容輸出子集").
package build

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v5"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// marketplaceSchemaPath is this package's embedded copy of the upstream
// schema (see this file's doc comment for why it is copied in rather than
// referenced across repos).
const marketplaceSchemaPath = "testdata/claude-code-marketplace.schema.json"

// compileMarketplaceSchema compiles marketplaceSchemaPath once per call --
// cheap enough (a single ~90KB file) that each test recompiling it is not
// worth caching across tests.
func compileMarketplaceSchema(t *testing.T) *jsonschema.Schema {
	t.Helper()
	data, err := os.ReadFile(marketplaceSchemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", marketplaceSchemaPath, err)
	}
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource(marketplaceSchemaPath, strings.NewReader(string(data))); err != nil {
		t.Fatalf("AddResource(%s): %v", marketplaceSchemaPath, err)
	}
	schema, err := compiler.Compile(marketplaceSchemaPath)
	if err != nil {
		t.Fatalf("Compile(%s): %v", marketplaceSchemaPath, err)
	}
	return schema
}

func TestClaudeMapperOutput_ConformsToUpstreamMarketplaceSchema_MixedLocalRemote(t *testing.T) {
	// Arrange: one local package plus all three remote source shapes
	// composeRemoteSource produces (github shorthand, git-subdir,
	// non-default-host url) -- design.md's four-rule composeRemoteSource
	// table, minus rule 1 (local, already covered by the first entry).
	cfg := &authoring.AuthoringConfig{
		Name:  "demo-marketplace",
		Owner: authoring.Owner{Name: "Acme", Email: "acme@example.com", URL: "https://acme.example.com"},
	}
	resolved := []ResolvedPackage{
		{
			Entry: authoring.PackageEntry{
				Name: "local-tool", Source: "./pkgs/tool-a", Description: "a local tool", Version: "0.1.0",
				Author: map[string]string{"name": "Jane Doe"}, License: "MIT", Homepage: "https://example.com/local",
			},
			IsLocal: true,
			Subdir:  "./pkgs/tool-a",
			Tags:    []string{"local"},
		},
		{
			Entry:             authoring.PackageEntry{Name: "github-tool", Source: "owner/repo-b"},
			SourceRepo:        "owner/repo-b",
			Ref:               "v1.0.0",
			SHA:               strings.Repeat("a", 40),
			RemoteDescription: "a github-hosted tool",
			RemoteVersion:     "1.0.0",
			Tags:              []string{"remote"},
		},
		{
			Entry:      authoring.PackageEntry{Name: "subdir-tool", Source: "owner/mono", Subdir: "packages/tool-c"},
			SourceRepo: "owner/mono",
			Subdir:     "packages/tool-c",
			Ref:        "v2.0.0",
			SHA:        strings.Repeat("b", 40),
		},
		{
			Entry:      authoring.PackageEntry{Name: "ghe-tool", Source: "ghe.example.com/owner/repo-d"},
			Host:       "ghe.example.com",
			SourceRepo: "owner/repo-d",
			Ref:        "v3.0.0",
			SHA:        strings.Repeat("c", 40),
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Assert
	schema := compileMarketplaceSchema(t)
	if err := schema.Validate(v); err != nil {
		t.Fatalf("output does not conform to upstream marketplace.json schema subset: %v\noutput: %s", err, raw)
	}
}

func TestClaudeMapperOutput_LocalOnly_ConformsToUpstreamMarketplaceSchema(t *testing.T) {
	// Arrange: a single local package (no remote source shapes at all) --
	// this sub-task's "本地/遠端混合輸出通過" requirement covers the local
	// leg of the mix independently of the previous mixed test, so a bug
	// scoped to only-local or only-remote output can't hide behind the
	// other's presence.
	cfg := &authoring.AuthoringConfig{
		Name:  "local-only-marketplace",
		Owner: authoring.Owner{Name: "Acme"},
	}
	resolved := []ResolvedPackage{
		{
			Entry:   authoring.PackageEntry{Name: "local-tool", Source: "./pkgs/tool-a"},
			IsLocal: true,
			Subdir:  "./pkgs/tool-a",
		},
	}

	// Act
	doc, _, err := ClaudeMapper{}.Compose(cfg, resolved)
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Assert
	schema := compileMarketplaceSchema(t)
	if err := schema.Validate(v); err != nil {
		t.Fatalf("output does not conform to upstream marketplace.json schema subset: %v\noutput: %s", err, raw)
	}
}

// TestMarketplaceSchema_CatchesRealViolation guards against the validator
// itself being a tautology (design.md's Review Gate C concern, applied here
// a step early): a doc missing the schema's required "owner" must fail, so
// a future regression that silently drops ClaudeMapper's owner field would
// be caught by this schema check, not just by the mapper's own unit tests.
func TestMarketplaceSchema_CatchesRealViolation(t *testing.T) {
	// Arrange
	schema := compileMarketplaceSchema(t)
	invalid := map[string]any{
		"name":    "demo-marketplace",
		"plugins": []any{},
		// "owner" deliberately omitted -- required by the schema.
	}

	// Act
	err := schema.Validate(invalid)

	// Assert
	if err == nil {
		t.Fatal("expected a validation error for a document missing the required 'owner' field")
	}
}
