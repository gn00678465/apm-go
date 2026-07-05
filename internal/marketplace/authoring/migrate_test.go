package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ── preconditions: both files must exist ────────────────────────────────

func TestMigrate_LegacyFileMissing_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error when marketplace.yml does not exist")
	}
	if !strings.Contains(err.Error(), "marketplace.yml") {
		t.Errorf("error = %v, want it to mention marketplace.yml", err)
	}
}

func TestMigrate_ApmYMLMissing_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error when apm.yml does not exist")
	}
	if !strings.Contains(err.Error(), "apm.yml") {
		t.Errorf("error = %v, want it to mention apm.yml", err)
	}
}

func TestMigrate_InvalidLegacyYAML_ErrorsAndLeavesBothFilesUntouched(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	const apmOriginal = "name: demo\nversion: 1.0.0\n"
	writeFile(t, dir, "apm.yml", apmOriginal)
	const legacyOriginal = "owner: [this is not a mapping\n"
	writeFile(t, dir, "marketplace.yml", legacyOriginal)

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error for unparsable marketplace.yml")
	}
	assertFileContent(t, filepath.Join(dir, "apm.yml"), apmOriginal)
	assertFileContent(t, filepath.Join(dir, "marketplace.yml"), legacyOriginal)
}

func TestMigrate_ApmYMLMalformedYAML_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: [this is not a mapping\n")
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error for malformed apm.yml")
	}
}

func TestMigrate_ApmYMLEmpty_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "")
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error for an empty apm.yml")
	}
}

func TestMigrate_ApmYMLNotAMapping_Errors(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "- just\n- a\n- list\n")
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error when apm.yml's top level is not a mapping")
	}
}

func TestMigrate_InvalidLegacySource_Errors(t *testing.T) {
	// Arrange: req-mf-017 validation (manifest.ValidateMarketplaceSource,
	// reused via parseAuthoringNode) must reject the legacy file just like
	// LoadAuthoringConfig would, before migrate does anything destructive.
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\n")
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages:\n  - name: bad\n    source: ../escape\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error for an invalid packages[].source in marketplace.yml")
	}
}

// ── AC4: comment-preserving migration, other apm.yml sections byte-exact ──

const migrateLegacyYML = `# Legacy marketplace config -- migrate me!
owner:
  name: acme-org
  url: https://github.com/acme-org
build:
  tagPattern: "v{version}"
packages:
  # foo is our flagship plugin
  - name: foo
    description: Flagship tool
    source: ./packages/foo
  - name: bar # legacy compatibility shim
    description: Legacy shim
    source: ./packages/bar
    version: "^1.0.0"
`

const migrateApmYMLNoBlockYet = `name: demo
version: "1.0.0"
description: A demo project

scripts:
  build: echo hi   # inline comment kept exactly
`

func TestMigrate_NoExistingBlock_PreservesLegacyCommentsAndApmYMLBytes(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", migrateApmYMLNoBlockYet)
	writeFile(t, dir, "marketplace.yml", migrateLegacyYML)

	// Act
	diff, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}
	if diff == "" {
		t.Error("expected a non-empty diff describing the change")
	}

	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	content := string(data)

	// apm.yml's pre-existing bytes must be an untouched prefix: the new
	// marketplace: block is appended, not spliced in the middle.
	if !strings.HasPrefix(content, migrateApmYMLNoBlockYet) {
		t.Errorf("apm.yml's existing content was not preserved as an untouched prefix:\n%s", content)
	}

	// The legacy file's comments must survive the move.
	if !strings.Contains(content, "# Legacy marketplace config -- migrate me!") {
		t.Errorf("legacy head comment was lost:\n%s", content)
	}
	if !strings.Contains(content, "# foo is our flagship plugin") {
		t.Errorf("legacy sequence leading comment was lost:\n%s", content)
	}
	if !strings.Contains(content, "name: bar # legacy compatibility shim") {
		t.Errorf("legacy inline comment was lost:\n%s", content)
	}

	// marketplace.yml must be gone.
	if _, statErr := os.Stat(filepath.Join(dir, "marketplace.yml")); !os.IsNotExist(statErr) {
		t.Errorf("marketplace.yml still exists after a successful migrate (stat err = %v)", statErr)
	}

	// The migrated apm.yml must itself be loadable, with the moved data
	// intact.
	cfg, src, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatalf("LoadAuthoringConfig after migrate returned error: %v", lerr)
	}
	if src != ConfigSourceApmYML {
		t.Errorf("ConfigSource = %v, want ConfigSourceApmYML", src)
	}
	if cfg.Owner.Name != "acme-org" || len(cfg.Packages) != 2 {
		t.Errorf("migrated config = %+v, unexpected", cfg)
	}
}

// ── existing non-empty marketplace: block requires --force ──────────────

func TestMigrate_ExistingNonNullBlock_WithoutForce_ErrorsAndLeavesBothFilesUntouched(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	const apmOriginal = "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: existing\n  packages: []\n"
	writeFile(t, dir, "apm.yml", apmOriginal)
	const legacyOriginal = "owner:\n  name: acme\npackages: []\n"
	writeFile(t, dir, "marketplace.yml", legacyOriginal)

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err == nil {
		t.Fatal("expected an error: apm.yml already has a non-null marketplace: block")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Errorf("error %q should mention --force", err.Error())
	}
	assertFileContent(t, filepath.Join(dir, "apm.yml"), apmOriginal)
	assertFileContent(t, filepath.Join(dir, "marketplace.yml"), legacyOriginal)
}

func TestMigrate_ExistingNonNullBlock_WithForce_OverwritesBlockOnly(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: existing\n  packages: []\nscripts:\n  build: echo hi\n")
	writeFile(t, dir, "marketplace.yml", migrateLegacyYML)

	// Act
	_, err := Migrate(dir, MigrateOptions{Force: true})

	// Assert
	if err != nil {
		t.Fatalf("Migrate --force returned error: %v", err)
	}
	cfg, _, lerr := LoadAuthoringConfig(dir)
	if lerr != nil {
		t.Fatalf("LoadAuthoringConfig after forced migrate returned error: %v", lerr)
	}
	if cfg.Owner.Name != "acme-org" {
		t.Errorf("Owner.Name = %q, want the legacy file's value to have replaced the old block", cfg.Owner.Name)
	}
	if len(cfg.Packages) != 2 {
		t.Errorf("Packages = %+v, want the legacy file's 2 entries", cfg.Packages)
	}
	data, rerr := os.ReadFile(filepath.Join(dir, "apm.yml"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if !strings.Contains(string(data), "build: echo hi") {
		t.Errorf("unrelated scripts: block was lost:\n%s", string(data))
	}
}

// TestMigrate_NullExistingBlock_ProceedsWithoutForce locks mkt-047's own
// "_has_marketplace_block" semantics carried over into migrate: a bare
// "marketplace:" key with no value must not require --force, the same way
// schema.go's LoadAuthoringConfig treats it as absent.
func TestMigrate_NullExistingBlock_ProceedsWithoutForce(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", "name: demo\nversion: 1.0.0\nmarketplace:\n")
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{})

	// Assert
	if err != nil {
		t.Fatalf("Migrate on a null marketplace: block returned error without --force: %v", err)
	}
}

// ── --dry-run: zero writes, zero deletes ─────────────────────────────────

func TestMigrate_DryRun_WritesNothingAndDeletesNothing(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	writeFile(t, dir, "apm.yml", migrateApmYMLNoBlockYet)
	writeFile(t, dir, "marketplace.yml", migrateLegacyYML)

	// Act
	diff, err := Migrate(dir, MigrateOptions{DryRun: true})

	// Assert
	if err != nil {
		t.Fatalf("Migrate --dry-run returned error: %v", err)
	}
	if diff == "" {
		t.Error("expected a non-empty diff even in --dry-run mode")
	}
	if !strings.Contains(diff, "acme-org") {
		t.Errorf("diff = %q, want it to mention the moved content", diff)
	}
	assertFileContent(t, filepath.Join(dir, "apm.yml"), migrateApmYMLNoBlockYet)
	assertFileContent(t, filepath.Join(dir, "marketplace.yml"), migrateLegacyYML)
}

func TestMigrate_DryRun_ExistingNonNullBlockWithoutForce_StillErrors(t *testing.T) {
	// Arrange: --dry-run must not bypass the --force guard.
	dir := t.TempDir()
	const apmOriginal = "name: demo\nversion: 1.0.0\nmarketplace:\n  owner:\n    name: existing\n  packages: []\n"
	writeFile(t, dir, "apm.yml", apmOriginal)
	writeFile(t, dir, "marketplace.yml", "owner:\n  name: acme\npackages: []\n")

	// Act
	_, err := Migrate(dir, MigrateOptions{DryRun: true})

	// Assert
	if err == nil {
		t.Fatal("expected --dry-run to still enforce the --force guard")
	}
	assertFileContent(t, filepath.Join(dir, "apm.yml"), apmOriginal)
}

// ── helpers ───────────────────────────────────────────────────────────────

func assertFileContent(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Errorf("%s content = %q, want unchanged %q", path, string(got), want)
	}
}
