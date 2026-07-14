package build

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// ── fakes ─────────────────────────────────────────────────────────────────

// fakeMetadataFetcher records every call it receives and returns a
// pre-programmed result, letting tests exercise ResolvePackages'/
// enrichRemoteMetadata's precedence and warning logic without a real git
// clone.
type fakeMetadataFetcher struct {
	description, version string
	err                  error
	calls                []string // source values FetchMetadata was called with
}

func (f *fakeMetadataFetcher) FetchMetadata(source, ref, subdir string) (string, string, error) {
	f.calls = append(f.calls, source)
	if f.err != nil {
		return "", "", f.err
	}
	return f.description, f.version, nil
}

// ── isDisplayVersion ──────────────────────────────────────────────────────

func TestIsDisplayVersion(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  bool
	}{
		{"empty string", "", false},
		{"plain semver", "1.2.3", true},
		{"caret range", "^1.2.3", false},
		{"tilde range", "~1.2.3", false},
		{"gte range", ">=1.2.3", false},
		{"lt range", "<2.0.0", false},
		{"eq prefix", "=1.2.3", false},
		{"space-separated range", "1.2.3 - 2.0.0", false},
		{"wildcard star", "1.2.*", false},
		{"trailing x wildcard", "1.2.x", false},
		{"trailing X wildcard uppercase", "1.2.X", false},
		{"x not the whole last segment", "1.2.3x", true},
		{"prerelease tag counts as display version", "2.0.0-rc.1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := isDisplayVersion(tt.value)

			// Assert
			if got != tt.want {
				t.Errorf("isDisplayVersion(%q) = %v, want %v", tt.value, got, tt.want)
			}
		})
	}
}

// ── enrichRemoteMetadata: curator-wins precedence ────────────────────────

func TestEnrichRemoteMetadata_CuratorSuppliesBothFields_SkipsFetchEntirely(t *testing.T) {
	// Arrange: curator's entry already supplies a usable (display) version
	// and description, so the remote apm.yml can never change the outcome
	// -- the fetch must never even be attempted (a panicking fetcher proves
	// this the same way panicLister proves "local packages never touch the
	// network").
	entry := authoring.PackageEntry{Name: "tool", Description: "Curator description", Version: "1.2.3"}

	// Act
	description, version, warning := enrichRemoteMetadata(entry, "v1.2.3", "", "owner/repo", panicMetadataFetcher{})

	// Assert
	if description != "Curator description" || version != "1.2.3" {
		t.Errorf("description/version = %q/%q, want curator's own values", description, version)
	}
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
}

func TestEnrichRemoteMetadata_CuratorSuppliesNeitherField_UsesRemoteFallback(t *testing.T) {
	// Arrange
	entry := authoring.PackageEntry{Name: "tool"}
	fetcher := &fakeMetadataFetcher{description: "Remote description", version: "2.0.0"}

	// Act
	description, version, warning := enrichRemoteMetadata(entry, "v2.0.0", "sub/dir", "owner/repo", fetcher)

	// Assert
	if description != "Remote description" || version != "2.0.0" {
		t.Errorf("description/version = %q/%q, want the fetched remote values", description, version)
	}
	if warning != "" {
		t.Errorf("warning = %q, want none", warning)
	}
	if len(fetcher.calls) != 1 || fetcher.calls[0] != "owner/repo" {
		t.Errorf("fetcher.calls = %v, want exactly one call for owner/repo", fetcher.calls)
	}
}

func TestEnrichRemoteMetadata_CuratorVersionIsARange_DoesNotWinDespiteBeingNonEmpty(t *testing.T) {
	// Arrange: entry.Version here is the semver RANGE used to resolve
	// ref/sha, not a display version -- mkt-050 修訂版 (c) forbids echoing
	// that range back out, so it must NOT count as a curator override even
	// though it is non-empty.
	entry := authoring.PackageEntry{Name: "tool", Version: "^1.0.0"}
	fetcher := &fakeMetadataFetcher{version: "1.5.0"}

	// Act
	_, version, _ := enrichRemoteMetadata(entry, "v1.5.0", "", "owner/repo", fetcher)

	// Assert
	if version != "1.5.0" {
		t.Errorf("version = %q, want the fetched remote version (curator's range must not win)", version)
	}
	if len(fetcher.calls) != 1 {
		t.Errorf("fetcher must still be called when curator's version is a range, calls = %v", fetcher.calls)
	}
}

func TestEnrichRemoteMetadata_PartialCuratorOverride_PerFieldPrecedence(t *testing.T) {
	// Arrange: curator supplies a description but not a usable version --
	// each field's precedence is decided independently, not all-or-nothing.
	entry := authoring.PackageEntry{Name: "tool", Description: "Curator description"}
	fetcher := &fakeMetadataFetcher{description: "Remote description (should lose)", version: "3.0.0"}

	// Act
	description, version, _ := enrichRemoteMetadata(entry, "v3.0.0", "", "owner/repo", fetcher)

	// Assert
	if description != "Curator description" {
		t.Errorf("description = %q, want curator's own value to win", description)
	}
	if version != "3.0.0" {
		t.Errorf("version = %q, want the fetched remote value (curator supplied none)", version)
	}
}

func TestEnrichRemoteMetadata_FetchFailure_ReturnsWarning_NotError(t *testing.T) {
	// Arrange
	entry := authoring.PackageEntry{Name: "tool"}
	fetcher := &fakeMetadataFetcher{err: errors.New("boom")}

	// Act
	description, version, warning := enrichRemoteMetadata(entry, "v1.0.0", "", "owner/repo", fetcher)

	// Assert
	if description != "" || version != "" {
		t.Errorf("description/version = %q/%q, want empty on fetch failure with no curator fallback", description, version)
	}
	if warning == "" {
		t.Fatal("expected a non-empty warning describing the fetch failure")
	}
	if !strings.Contains(warning, "tool") {
		t.Errorf("warning = %q, want it to name the package", warning)
	}
}

func TestEnrichRemoteMetadata_FetchFailure_CuratorFieldsStillWin(t *testing.T) {
	// Arrange: a fetch failure degrades gracefully to whatever the curator
	// already had -- it must never erase an already-known curator value.
	entry := authoring.PackageEntry{Name: "tool", Description: "Curator description"}
	fetcher := &fakeMetadataFetcher{err: errors.New("boom")}

	// Act
	description, version, warning := enrichRemoteMetadata(entry, "v1.0.0", "", "owner/repo", fetcher)

	// Assert
	if description != "Curator description" {
		t.Errorf("description = %q, want curator's value preserved despite fetch failure", description)
	}
	if version != "" {
		t.Errorf("version = %q, want empty (curator supplied none, fetch failed)", version)
	}
	if warning == "" {
		t.Fatal("expected a non-empty warning describing the fetch failure")
	}
}

// ── ResolvePackages integration: warnings surface, build never fails ────

func TestResolvePackages_MetadataFetchFailure_SurfacesWarning_BuildStillSucceeds(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "v1.0.0"},
	}}
	fetcher := &fakeMetadataFetcher{err: errors.New("network unreachable")}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{MetadataFetcher: fetcher})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v, want a fetch failure to never fail the build", err)
	}
	if len(resolved) != 1 || resolved[0].Ref != "v1.0.0" {
		t.Fatalf("resolved = %+v, want the package's ref/sha resolved despite the metadata fetch failure", resolved)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one warning", warnings)
	}
	if !strings.Contains(warnings[0], "tool") {
		t.Errorf("warnings[0] = %q, want it to name the package", warnings[0])
	}
}

func TestResolvePackages_MetadataEnrichment_PopulatesRemoteDescriptionAndVersion(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "tool", Source: dir, Ref: "v1.0.0"},
	}}
	fetcher := &fakeMetadataFetcher{description: "A remote tool", version: "1.0.0"}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{MetadataFetcher: fetcher})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resolved[0].RemoteDescription != "A remote tool" || resolved[0].RemoteVersion != "1.0.0" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want the fetched values", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

func TestResolvePackages_LocalPackage_NeverCallsMetadataFetcher(t *testing.T) {
	// Arrange: mkt-050 修訂版 (c)'s remote apm.yml fetch is scoped to remote
	// packages only -- a local package must never trigger it either.
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local", Source: "./pkgs/a"},
	}}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resolved[0].RemoteDescription != "" || resolved[0].RemoteVersion != "" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want empty for a local package", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

// ── F1 fix: local package metadata enrichment ────────────────────────────

func TestResolvePackages_LocalPackage_MetadataEnrichment_ReadsLocalApmYML(t *testing.T) {
	// Arrange: F1 fix -- a local package with no curator description/
	// version is enriched from its own apm.yml on disk, relative to
	// Options.ProjectRoot.
	projectRoot := t.TempDir()
	pkgDir := filepath.Join(projectRoot, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("description: A local tool\nversion: 2.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-tool", Source: "./pkgs/tool"},
	}}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{ProjectRoot: projectRoot, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resolved[0].RemoteDescription != "A local tool" || resolved[0].RemoteVersion != "2.0.0" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want A local tool/2.0.0", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

func TestResolvePackages_LocalPackage_MetadataEnrichment_CuratorWins(t *testing.T) {
	// Arrange: curator-supplied description/version must win over the
	// local apm.yml's own values.
	projectRoot := t.TempDir()
	pkgDir := filepath.Join(projectRoot, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte("description: from local apm.yml\nversion: 9.9.9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-tool", Source: "./pkgs/tool", Description: "curator description", Version: "1.0.0"},
	}}

	// Act
	resolved, _, err := ResolvePackages(cfg, Options{ProjectRoot: projectRoot, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if resolved[0].RemoteDescription != "curator description" || resolved[0].RemoteVersion != "1.0.0" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want curator description/1.0.0", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

func TestResolvePackages_LocalPackage_MetadataEnrichment_NoApmYML_NoWarning(t *testing.T) {
	// Arrange: a local package with no apm.yml at all on disk is the
	// ordinary case, not a failure -- no warning should be produced.
	projectRoot := t.TempDir()
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-tool", Source: "./pkgs/tool"},
	}}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{ProjectRoot: projectRoot, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none (a missing local apm.yml is not a failure)", warnings)
	}
	if resolved[0].RemoteDescription != "" || resolved[0].RemoteVersion != "" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want both empty", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

func TestResolvePackages_LocalPackage_MetadataEnrichment_SourceIsProjectRootItself_Skipped(t *testing.T) {
	// Arrange: a local package whose source resolves to the project root
	// itself must never read that apm.yml -- it's the marketplace's own
	// config, not a package manifest (mirrors the Python original's
	// "package_root == project_root" skip).
	projectRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "apm.yml"), []byte("description: the marketplace itself\nversion: 1.2.3\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "self", Source: "./"},
	}}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{ProjectRoot: projectRoot, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resolved[0].RemoteDescription != "" || resolved[0].RemoteVersion != "" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want both empty (project root's own apm.yml must never be read as package metadata)", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

func TestResolvePackages_LocalPackage_MetadataEnrichment_OverSizeCap_ReturnsWarning(t *testing.T) {
	// Arrange: F1's size cap on a local package's own apm.yml (matching
	// F4's identical cap on a remote package's apm.yml).
	projectRoot := t.TempDir()
	pkgDir := filepath.Join(projectRoot, "pkgs", "tool")
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	oversized := "description: \"" + strings.Repeat("x", localMetadataMaxBytes+1) + "\"\n"
	if err := os.WriteFile(filepath.Join(pkgDir, "apm.yml"), []byte(oversized), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-tool", Source: "./pkgs/tool"},
	}}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{ProjectRoot: projectRoot, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one warning", warnings)
	}
	if resolved[0].RemoteDescription != "" {
		t.Errorf("RemoteDescription = %q, want empty (oversized apm.yml must be skipped)", resolved[0].RemoteDescription)
	}
}

func TestResolvePackages_LocalPackage_NoProjectRootOption_DefaultsToCurrentDirectory(t *testing.T) {
	// Arrange: Options.ProjectRoot's zero value must default to "." rather
	// than erroring or panicking.
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{
		{Name: "local-tool", Source: "./pkgs/does-not-exist-anywhere"},
	}}

	// Act
	resolved, warnings, err := ResolvePackages(cfg, Options{Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})

	// Assert
	if err != nil {
		t.Fatalf("ResolvePackages() error = %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("warnings = %v, want none", warnings)
	}
	if resolved[0].RemoteDescription != "" || resolved[0].RemoteVersion != "" {
		t.Errorf("RemoteDescription/RemoteVersion = %q/%q, want both empty", resolved[0].RemoteDescription, resolved[0].RemoteVersion)
	}
}

// ── gitMetadataFetcher: real git clone against a local fixture repo ─────

// initGitRepoWithApmYML creates a real git repository in dir with an
// apm.yml (at dir/subPath/apm.yml, or dir/apm.yml when subPath is "")
// declaring description/version, commits it, and -- when tagName is
// non-empty -- tags the commit. Returns the commit's SHA.
func initGitRepoWithApmYML(t *testing.T, dir, subPath, description, version, tagName string) string {
	t.Helper()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")

	apmYmlDir := dir
	if subPath != "" {
		apmYmlDir = filepath.Join(dir, subPath)
		if err := os.MkdirAll(apmYmlDir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	content := fmt.Sprintf("name: tool\ndescription: %q\nversion: %q\n", description, version)
	if err := os.WriteFile(filepath.Join(apmYmlDir, "apm.yml"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	if tagName != "" {
		gitCmd(t, dir, "tag", tagName)
	}
	return revParse(t, dir, "HEAD")
}

func TestGitMetadataFetcher_FetchMetadata_ReadsDescriptionAndVersion(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithApmYML(t, dir, "", "A cool remote tool", "2.0.0", "v1.0.0")

	// Act
	description, version, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "")

	// Assert
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}
	if description != "A cool remote tool" || version != "2.0.0" {
		t.Errorf("description/version = %q/%q, want A cool remote tool/2.0.0", description, version)
	}
}

func TestGitMetadataFetcher_FetchMetadata_ReadsFromSubdir(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	initGitRepoWithApmYML(t, dir, "pkgs/a", "Subdir tool", "1.5.0", "v1.0.0")

	// Act
	description, version, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "pkgs/a")

	// Assert
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}
	if description != "Subdir tool" || version != "1.5.0" {
		t.Errorf("description/version = %q/%q, want Subdir tool/1.5.0", description, version)
	}
}

func TestGitMetadataFetcher_FetchMetadata_ShaPinnedRef(t *testing.T) {
	// Arrange: a 40-char lowercase hex ref cannot be cloned via
	// `--branch` -- FetchMetadata must fall back to a full clone + checkout.
	dir := t.TempDir()
	sha := initGitRepoWithApmYML(t, dir, "", "SHA-pinned tool", "3.0.0", "")

	// Act
	description, version, err := gitMetadataFetcher{}.FetchMetadata(dir, sha, "")

	// Assert
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}
	if description != "SHA-pinned tool" || version != "3.0.0" {
		t.Errorf("description/version = %q/%q, want SHA-pinned tool/3.0.0", description, version)
	}
}

func TestGitMetadataFetcher_FetchMetadata_MissingApmYml_ReturnsError(t *testing.T) {
	// Arrange: a real remote with no apm.yml at all.
	dir := t.TempDir()
	initGitRepoWithTags(t, dir, "v1.0.0")

	// Act
	_, _, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "")

	// Assert
	if err == nil {
		t.Fatal("expected an error: remote has no apm.yml to read")
	}
}

func TestGitMetadataFetcher_FetchMetadata_MalformedYaml_ReturnsError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("description: [unterminated"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	gitCmd(t, dir, "tag", "v1.0.0")

	// Act
	_, _, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "")

	// Assert
	if err == nil {
		t.Fatal("expected an error: apm.yml is not valid YAML")
	}
}

// F4 fix: an untrusted, cloned repo's apm.yml must be size-capped (64KiB,
// matching enrichLocalMetadata's identical cap for F1) before being read
// into memory and parsed -- rather than trusting an arbitrary remote to
// hand back a file of unbounded size.
func TestGitMetadataFetcher_FetchMetadata_OverSizeCap_ReturnsError(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	oversized := "description: \"" + strings.Repeat("x", remoteMetadataMaxBytes+1) + "\"\n"
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte(oversized), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	gitCmd(t, dir, "tag", "v1.0.0")

	// Act
	_, _, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "")

	// Assert
	if err == nil {
		t.Fatal("expected an error: apm.yml exceeds the size cap")
	}
}

func TestGitMetadataFetcher_FetchMetadata_NonMappingYaml_NoErrorButEmptyFields(t *testing.T) {
	// Arrange: valid YAML, but not a mapping -- mirrors Python's
	// "isinstance(data, dict)" guard: this is "no metadata found", not a
	// fetch failure.
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("- one\n- two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	gitCmd(t, dir, "tag", "v1.0.0")

	// Act
	description, version, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "")

	// Assert
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v, want a non-mapping document treated as ordinary 'no metadata'", err)
	}
	if description != "" || version != "" {
		t.Errorf("description/version = %q/%q, want both empty", description, version)
	}
}

func TestGitMetadataFetcher_FetchMetadata_MissingFieldsInApmYml_NoErrorButEmptyFields(t *testing.T) {
	// Arrange: apm.yml exists and is a mapping, but declares neither field.
	dir := t.TempDir()
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("name: tool\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	gitCmd(t, dir, "tag", "v1.0.0")

	// Act
	description, version, err := gitMetadataFetcher{}.FetchMetadata(dir, "v1.0.0", "")

	// Assert
	if err != nil {
		t.Fatalf("FetchMetadata() error = %v", err)
	}
	if description != "" || version != "" {
		t.Errorf("description/version = %q/%q, want both empty", description, version)
	}
}
