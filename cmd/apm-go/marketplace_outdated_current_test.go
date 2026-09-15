package main

import (
	"os"
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated-fixes SC-F7: the CLI reads ./marketplace.json
// into the current map; a SHA pin with a display version and no release
// tags must not print that SHA as its Current version (decision D-f).
func TestMarketplaceOutdated_ShaPin_NoTags_MarketplaceJson_OmitsSha(t *testing.T) {
	chdirTemp(t)
	repoDir := t.TempDir()
	initGitRepoWithTags(t, repoDir)
	sha := gitCmd(t, repoDir, "rev-parse", "HEAD")
	withFixtureRemoteLister(t, repoDir)
	writeOutdatedFixture(t, "    - name: tool\n      source: owner/repo\n      ref: "+sha+"\n      version: 1.0.0\n")
	marketplaceJSON := `{"name":"demo","plugins":[{"name":"tool","source":{"source":"github","repo":"owner/repo","ref":"` + sha + `"}}]}`
	if err := os.WriteFile("marketplace.json", []byte(marketplaceJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runMarketplaceCmd(t, "outdated")

	if !strings.Contains(out, "No matching tags found") {
		t.Fatalf("output = %q, want the no-tags row", out)
	}
	if strings.Contains(out, sha[:12]) {
		t.Errorf("output = %q, want the pinned SHA absent from the row", out)
	}
}
