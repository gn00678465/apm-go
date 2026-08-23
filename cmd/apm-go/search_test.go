package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// searchFixtureManifest is shared by every test below: five plugins picked
// so a single, deliberate substring hits exactly one of name/description/tag
// (never more than one plugin), per ticket 05's fixture requirement (one
// match by name, one by description, one by tag, one >60-char description,
// one empty description).
const searchFixtureManifest = `{
  "name": "skills",
  "plugins": [
    {"name": "security-scanner", "source": "./security-scanner", "description": "Scans your dependency graph for known CVEs", "tags": ["security", "cli"]},
    {"name": "toolkit-helper", "source": "./toolkit-helper", "description": "Runs static license compliance checks across your repository", "tags": ["utility"]},
    {"name": "widget-pack", "source": "./widget-pack", "description": "Bundles widgets for rapid prototyping", "tags": ["experimental", "beta-channel"]},
    {"name": "docgen", "source": "./docgen", "description": "Generates comprehensive API documentation from source code annotations automatically and keeps it in sync", "tags": ["docs"]},
    {"name": "quiet-plugin", "source": "./quiet-plugin", "description": "", "tags": []}
  ]
}`

// runSearchCmd executes searchCmd() standalone (it is a top-level command,
// never nested under marketplaceCmd()) and returns its combined stdout+
// stderr and the RunE error.
func runSearchCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	c := searchCmd()
	var buf bytes.Buffer
	c.SetOut(&buf)
	c.SetErr(&buf)
	c.SetArgs(args)
	err := c.Execute()
	return buf.String(), err
}

// registerSearchFixture registers searchFixtureManifest under alias
// "skills" via the real `marketplace add` command (a KindLocal source, no
// network involved), so search's own Fetch path is exercised end to end.
func registerSearchFixture(t *testing.T) {
	t.Helper()
	dir := writeLocalManifestDir(t, searchFixtureManifest)
	if _, err := runMarketplaceCmd(t, "add", dir, "--name", "skills"); err != nil {
		t.Fatalf("registering search fixture: %v", err)
	}
}

func TestSearchCmd_TopLevelNotUnderMarketplace(t *testing.T) {
	isolatedMarketplaceRegistry(t)

	if _, _, err := marketplaceCmd().Find([]string{"search"}); err == nil {
		t.Error(`marketplaceCmd().Find(["search"]) found a subcommand, want "search" absent from the marketplace group`)
	}

	// The top-level command itself must exist and be runnable.
	out, err := runSearchCmd(t, "nomatch@skills")
	if err == nil {
		t.Fatalf("runSearchCmd: expected an error for an unregistered marketplace, got success: %s", out)
	}
}

func TestSearchCmd_NoAtSign_InvalidFormatError(t *testing.T) {
	isolatedMarketplaceRegistry(t)

	_, err := runSearchCmd(t, "nope")
	if err == nil {
		t.Fatal("expected an error for a QUERY@MARKETPLACE expression missing '@'")
	}
	want := "Invalid format: 'nope'. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestSearchCmd_EmptyQuery_BothRequiredError(t *testing.T) {
	isolatedMarketplaceRegistry(t)

	_, err := runSearchCmd(t, "@skills")
	if err == nil {
		t.Fatal("expected an error for an empty QUERY")
	}
	want := "Both QUERY and MARKETPLACE are required. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestSearchCmd_EmptyMarketplace_BothRequiredError(t *testing.T) {
	isolatedMarketplaceRegistry(t)

	_, err := runSearchCmd(t, "security@")
	if err == nil {
		t.Fatal("expected an error for an empty MARKETPLACE")
	}
	want := "Both QUERY and MARKETPLACE are required. Use QUERY@MARKETPLACE, e.g.: apm-go search security@skills"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestSearchCmd_SplitsOnLastAt proves "foo@bar@skills" splits into
// query="foo@bar", marketplace="skills" (the LAST '@'), not the first: with
// nothing registered, the not-registered error must name "skills", never
// "bar@skills" (what a first-'@' split would have produced).
func TestSearchCmd_SplitsOnLastAt(t *testing.T) {
	isolatedMarketplaceRegistry(t)

	_, err := runSearchCmd(t, "foo@bar@skills")
	if err == nil {
		t.Fatal("expected a not-registered error")
	}
	want := "Marketplace 'skills' is not registered. Use 'apm-go marketplace list' to see registered marketplaces."
	if err.Error() != want {
		t.Errorf("error = %q, want %q (proves split on the LAST '@', not the first)", err.Error(), want)
	}
}

func TestSearchCmd_UnknownMarketplace_NotRegisteredError(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	_, err := runSearchCmd(t, "foo@doesnotexist")
	if err == nil {
		t.Fatal("expected a not-registered error")
	}
	want := "Marketplace 'doesnotexist' is not registered. Use 'apm-go marketplace list' to see registered marketplaces."
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestSearchCmd_MatchesByName(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	out, err := runSearchCmd(t, "security@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	if !strings.Contains(out, "Found 1 plugin(s):") {
		t.Errorf("output = %q, want it to report 1 match", out)
	}
	if !strings.Contains(out, "security-scanner@skills -- Scans your dependency graph for known CVEs") {
		t.Errorf("output = %q, want the name-matched plugin line", out)
	}
	if strings.Contains(out, "toolkit-helper") || strings.Contains(out, "widget-pack") {
		t.Errorf("output = %q, want only the name match, no other plugin", out)
	}
}

func TestSearchCmd_MatchesByDescription(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	out, err := runSearchCmd(t, "license@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	if !strings.Contains(out, "toolkit-helper@skills -- Runs static license compliance checks across your repository") {
		t.Errorf("output = %q, want the description-matched plugin line", out)
	}
	if strings.Contains(out, "security-scanner") || strings.Contains(out, "widget-pack") {
		t.Errorf("output = %q, want only the description match", out)
	}
}

func TestSearchCmd_MatchesByTag(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	out, err := runSearchCmd(t, "experimental@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	if !strings.Contains(out, "widget-pack@skills -- Bundles widgets for rapid prototyping") {
		t.Errorf("output = %q, want the tag-matched plugin line", out)
	}
	if strings.Contains(out, "security-scanner") || strings.Contains(out, "toolkit-helper") {
		t.Errorf("output = %q, want only the tag match", out)
	}
}

func TestSearchCmd_EmptyDescriptionHasNoDashSuffix(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	out, err := runSearchCmd(t, "quiet-plugin@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	if !strings.Contains(out, "quiet-plugin@skills\n") {
		t.Errorf("output = %q, want a bare \"quiet-plugin@skills\" line with no \" -- \" suffix for an empty description", out)
	}
	if strings.Contains(out, "quiet-plugin@skills --") {
		t.Errorf("output = %q, want no \" -- \" suffix for an empty description", out)
	}
}

// TestSearchCmd_LimitAppliedAfterMatching proves --limit truncates the
// already-matched result set, keeping manifest order: "your" matches
// security-scanner (index 0) and toolkit-helper (index 1) by description;
// --limit 1 must keep only security-scanner.
func TestSearchCmd_LimitAppliedAfterMatching(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	full, err := runSearchCmd(t, "your@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	if !strings.Contains(full, "Found 2 plugin(s):") {
		t.Fatalf("output = %q, want 2 matches before --limit", full)
	}

	limited, err := runSearchCmd(t, "--limit", "1", "your@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	if !strings.Contains(limited, "Found 1 plugin(s):") {
		t.Errorf("output = %q, want exactly 1 match with --limit 1", limited)
	}
	if !strings.Contains(limited, "security-scanner@skills") {
		t.Errorf("output = %q, want the manifest-order-first match (security-scanner) to survive --limit 1", limited)
	}
	if strings.Contains(limited, "toolkit-helper") {
		t.Errorf("output = %q, want the second match (toolkit-helper) dropped by --limit 1", limited)
	}
}

func TestSearchCmd_DescriptionOver60CharsIsUnaffectedByLimitButFetched(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	// The non-rich fallback path never truncates (that is a rich-table-only
	// rendering rule -- __init__.py:1417-1424 is inside the `if console:`
	// branch); this only proves the >60-char-description plugin is found and
	// its full description reaches the renderer.
	out, err := runSearchCmd(t, "docgen@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: %v", err)
	}
	wantDesc := "Generates comprehensive API documentation from source code annotations automatically and keeps it in sync"
	if !strings.Contains(out, wantDesc) {
		t.Errorf("output = %q, want the full untruncated description in the non-rich fallback", out)
	}
}

func TestTruncateSearchDescription(t *testing.T) {
	tests := []struct {
		name string
		desc string
		want string
	}{
		{"empty", "", "--"},
		{"short", "a short description", "a short description"},
		{"exactly60", strings.Repeat("a", 60), strings.Repeat("a", 60)},
		{
			"over60",
			"Generates comprehensive API documentation from source code annotations automatically and keeps it in sync",
			"Generates comprehensive API documentation from source cod...",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := truncateSearchDescription(tt.desc); got != tt.want {
				t.Errorf("truncateSearchDescription(%q) = %q, want %q", tt.desc, got, tt.want)
			}
		})
	}
}

func TestSearchCmd_ZeroResultsWarningAndExit0(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	registerSearchFixture(t)

	out, err := runSearchCmd(t, "doesnotexist12345@skills")
	if err != nil {
		t.Fatalf("runSearchCmd: expected exit 0 (nil error) for zero results, got %v", err)
	}
	want := "No plugins found matching 'doesnotexist12345' in 'skills'. Try 'apm-go marketplace browse skills' to see all plugins."
	if !strings.Contains(out, want) {
		t.Errorf("output = %q, want it to contain %q", out, want)
	}
}

func TestSearchCmd_FetchFailure(t *testing.T) {
	isolatedMarketplaceRegistry(t)

	// Register a valid local source, then remove its marketplace.json so a
	// subsequent search's own Fetch (not `marketplace add`'s upfront probe)
	// is what fails -- exercising the "Search failed: <err>" wrapper.
	dir := writeLocalManifestDir(t, searchFixtureManifest)
	if _, err := runMarketplaceCmd(t, "add", dir, "--name", "vanishing"); err != nil {
		t.Fatalf("registering vanishing fixture: %v", err)
	}
	if err := os.Remove(dir + "/marketplace.json"); err != nil {
		t.Fatalf("removing marketplace.json: %v", err)
	}

	_, err := runSearchCmd(t, "foo@vanishing")
	if err == nil {
		t.Fatal("expected a Fetch failure error")
	}
	if !strings.HasPrefix(err.Error(), "Search failed: ") {
		t.Errorf("error = %q, want it to start with %q", err.Error(), "Search failed: ")
	}

	verboseOut, verboseErr := runSearchCmd(t, "-v", "foo@vanishing")
	if verboseErr == nil {
		t.Fatal("expected a Fetch failure error with -v")
	}
	if verboseOut == "" {
		t.Error("with -v, want extra diagnostic detail printed before the error, got none")
	}
}

func TestSearchCmd_HelpText(t *testing.T) {
	out, err := runSearchCmd(t, "--help")
	if err != nil {
		t.Fatalf("runSearchCmd --help: %v", err)
	}
	if !strings.Contains(out, "Search plugins in a marketplace (QUERY@MARKETPLACE)") {
		t.Errorf("help output = %q, want the Oracle's short description", out)
	}
	if !strings.Contains(out, "--limit") || !strings.Contains(out, "20") {
		t.Errorf("help output = %q, want --limit to show its default of 20", out)
	}
	if !strings.Contains(out, "-v, --verbose") {
		t.Errorf("help output = %q, want -v/--verbose listed", out)
	}
}
