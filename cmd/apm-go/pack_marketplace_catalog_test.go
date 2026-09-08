package main

import (
	"bytes"
	"strings"
	"testing"
)

// The catalog block the pinned Oracle prints after its per-output success
// lines (_render_marketplace_catalog, pack.py:763-777). Ticket 28: apm-go
// printed none of these lines at all, and no parity case caught it because
// no case ran a marketplace pack to a real success -- pack-check-* use
// "-m none", pack-refuse-* exit 2 first, pack-json skips the deferred
// renders. These tests plus the pack-marketplace-success/
// pack-marketplace-multi-output cases close that hole from both sides.

func TestRenderMarketplaceCatalog_MatchesOracleLines(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	renders := []marketplaceRender{
		{format: "claude", absPath: "/proj/.claude-plugin/marketplace.json", count: 1},
	}

	// Act
	renderMarketplaceCatalog(&buf, renders)

	// Assert
	want := " i Marketplace artifacts ready:\n" +
		" i   [claude] /proj/.claude-plugin/marketplace.json\n" +
		" i How consumers install from this marketplace varies by AI assistant.\n" +
		" i See: https://microsoft.github.io/apm/producer/publish-to-a-marketplace/#consume-from-any-assistant\n"
	if got := buf.String(); got != want {
		t.Errorf("catalog block mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRenderMarketplaceCatalog_TagIsLeftJustifiedToWidestProfile(t *testing.T) {
	// The Oracle pads each tag with str.ljust(label_width) where
	// label_width is the widest profile name (pack.py:769-772), so "codex"
	// renders as "codex " when "claude" is also present.

	// Arrange
	var buf bytes.Buffer
	renders := []marketplaceRender{
		{format: "claude", absPath: "/proj/.claude-plugin/marketplace.json"},
		{format: "codex", absPath: "/proj/.agents/plugins/marketplace.json"},
	}

	// Act
	renderMarketplaceCatalog(&buf, renders)

	// Assert
	got := buf.String()
	for _, want := range []string{
		" i   [claude] /proj/.claude-plugin/marketplace.json\n",
		" i   [codex ] /proj/.agents/plugins/marketplace.json\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing padded row %q in:\n%s", want, got)
		}
	}
}

func TestRenderMarketplaceCatalog_SuppressedForDryRun(t *testing.T) {
	// pack.py:748 gates the catalog on `written and not dry_run` -- a
	// dry-run wrote no files, so there is nothing to catalogue.

	// Arrange
	var buf bytes.Buffer
	renders := []marketplaceRender{
		{format: "claude", absPath: "/proj/.claude-plugin/marketplace.json", dryRun: true},
	}

	// Act
	renderMarketplaceCatalog(&buf, renders)

	// Assert
	if got := buf.String(); got != "" {
		t.Errorf("dry-run must print no catalog, got %q", got)
	}
}

func TestRenderMarketplaceCatalog_NoOutputsPrintsNothing(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	renderMarketplaceCatalog(&buf, nil)

	// Assert
	if got := buf.String(); got != "" {
		t.Errorf("no renders must print no catalog, got %q", got)
	}
}

func TestRenderMarketplaceOutput_PrintsAbsolutePath(t *testing.T) {
	// The Oracle's resolve_effective_output_path (output_profiles.py:134-136)
	// joins any relative configured path onto the absolute project root, so
	// MarketplaceOutputReport.output_path is absolute at EVERY use site --
	// including this completion line. Ticket 13's displayPath relativisation
	// is bundle-only and must not leak here (ticket 28).
	tests := []struct {
		name   string
		render marketplaceRender
		want   string
	}{
		{
			name:   "real run",
			render: marketplaceRender{format: "claude", count: 1, outputPath: ".claude-plugin/marketplace.json", absPath: "/proj/.claude-plugin/marketplace.json"},
			want:   " + Built marketplace.json [claude] (1 package(s)) -> /proj/.claude-plugin/marketplace.json\n",
		},
		{
			name:   "dry run",
			render: marketplaceRender{format: "codex", count: 2, outputPath: ".agents/plugins/marketplace.json", absPath: "/proj/.agents/plugins/marketplace.json", dryRun: true},
			want:   " i dry-run: Would write marketplace.json [codex] (2 package(s)) -> /proj/.agents/plugins/marketplace.json\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var buf bytes.Buffer

			// Act
			renderMarketplaceOutput(&buf, tt.render)

			// Assert
			if got := buf.String(); got != tt.want {
				t.Errorf("\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}
