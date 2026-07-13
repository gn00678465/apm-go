package main

import (
	"os"
	"strings"
	"testing"
)

// ── flags wired ──────────────────────────────────────────────────────────

func TestMarketplaceMigrateCmd_FlagsWired(t *testing.T) {
	cmd := marketplaceMigrateCmd()
	for _, name := range []string{"force", "yes", "dry-run", "verbose"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("marketplace migrate is missing --%s", name)
		}
	}
	if cmd.Flags().ShorthandLookup("y") == nil {
		t.Error("marketplace migrate is missing the -y shorthand for --yes")
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("marketplace migrate is missing the -v shorthand for --verbose")
	}
}

// ── end-to-end: success, dry-run, force guard ────────────────────────────

const migrateCmdLegacyYML = `# legacy config, migrate me
owner:
  name: acme-org
packages:
  - name: foo # keep this comment
    source: ./packages/foo
`

func TestMarketplaceMigrate_Success_RemovesLegacyAndPrintsMessage(t *testing.T) {
	// Arrange
	dir := chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("marketplace.yml", []byte(migrateCmdLegacyYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "migrate")

	// Assert
	if err != nil {
		t.Fatalf("marketplace migrate returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Migrated marketplace.yml") {
		t.Errorf("output = %q, want a success message", out)
	}
	if _, statErr := os.Stat(dir + "/marketplace.yml"); !os.IsNotExist(statErr) {
		t.Errorf("marketplace.yml should have been removed (stat err = %v)", statErr)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "# keep this comment") {
		t.Errorf("apm.yml = %q, want the legacy comment preserved", string(data))
	}
}

func TestMarketplaceMigrate_DryRun_PrintsDiffWithoutWriting(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte("name: demo\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("marketplace.yml", []byte(migrateCmdLegacyYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "migrate", "--dry-run")

	// Assert
	if err != nil {
		t.Fatalf("marketplace migrate --dry-run returned error: %v (output: %s)", err, out)
	}
	if !strings.Contains(out, "Dry run") {
		t.Errorf("output = %q, want a dry-run banner", out)
	}
	if !strings.Contains(out, "acme-org") {
		t.Errorf("output = %q, want the printed diff to mention the moved content", out)
	}
	if _, statErr := os.Stat("marketplace.yml"); statErr != nil {
		t.Errorf("marketplace.yml should still exist after --dry-run: %v", statErr)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if strings.Contains(string(data), "marketplace:") {
		t.Errorf("apm.yml should not have been written under --dry-run:\n%s", string(data))
	}
}

func TestMarketplaceMigrate_ExistingBlockWithoutForce_Errors(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte("name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("marketplace.yml", []byte(migrateCmdLegacyYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	_, err := runMarketplaceCmd(t, "migrate")

	// Assert
	if err == nil {
		t.Fatal("expected marketplace migrate to fail without --force when a block already exists")
	}
}

// TestMarketplaceMigrate_YesShorthandActsAsForce proves -y is a genuine
// alias for --force (mkt-044's "--force|--yes/-y 同義"), not merely present
// as an unused flag.
func TestMarketplaceMigrate_YesShorthandActsAsForce(t *testing.T) {
	// Arrange
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte("name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile("marketplace.yml", []byte(migrateCmdLegacyYML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Act
	out, err := runMarketplaceCmd(t, "migrate", "-y")

	// Assert
	if err != nil {
		t.Fatalf("marketplace migrate -y returned error: %v (output: %s)", err, out)
	}
	data, rerr := os.ReadFile("apm.yml")
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "acme-org") {
		t.Errorf("apm.yml = %q, want the existing block overwritten via -y", string(data))
	}
}
