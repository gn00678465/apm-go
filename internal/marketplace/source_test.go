package marketplace

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// captureStderr redirects os.Stderr for the duration of fn and returns
// everything written to it, mirroring the os.Pipe technique already used in
// cmd/apm/mcpinstall_test.go (there for os.Stdout).
func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	orig := os.Stderr
	os.Stderr = w
	fn()
	os.Stderr = orig
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

// TestParseMarketplaceSource_LocalPaths covers mkt-010 rule 1: every local
// path shape the checklist enumerates must resolve to KindLocal with an
// absolute URL, regardless of which separator style the raw SOURCE used.
func TestParseMarketplaceSource_LocalPaths(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"absolute POSIX path", "/abs/path"},
		{"relative dot path", "./relative"},
		{"relative dotdot path", "../relative"},
		{"home-relative path", "~/home"},
		{"windows drive letter", `C:\Users\foo\marketplace`},
		{"windows relative dot path", `.\relative`},
		{"windows relative dotdot path", `..\relative`},
		{"windows home-relative path", `~\relative`},
		{"bare tilde", "~"},
		{"file scheme", "file:///abs/path"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err != nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", raw, err)
			}
			if src.Kind() != KindLocal {
				t.Errorf("Kind() = %q, want %q", src.Kind(), KindLocal)
			}
			if !filepath.IsAbs(src.URL) {
				t.Errorf("URL = %q, want an absolute path", src.URL)
			}
			if src.Path != defaultManifestPath {
				t.Errorf("Path = %q, want %q", src.Path, defaultManifestPath)
			}
		})
	}
}

// TestParseMarketplaceSource_LocalPathPointingToDirectory covers the
// "not a file" half of mkt B5's file-vs-directory check: a local path that
// resolves to an existing *directory* keeps the default manifest-probing
// path, unlike a direct file (see
// TestParseMarketplaceSource_LocalPathPointingToFile).
func TestParseMarketplaceSource_LocalPathPointingToDirectory(t *testing.T) {
	// Arrange
	dir := t.TempDir()

	// Act
	src, err := ParseMarketplaceSource(dir, "")

	// Assert
	if err != nil {
		t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", dir, err)
	}
	if src.Path != defaultManifestPath {
		t.Errorf("Path = %q, want %q (an existing directory must keep the default probing path)", src.Path, defaultManifestPath)
	}
}

// TestParseMarketplaceSource_LocalPathPointingToFile covers mkt B5: a local
// SOURCE that resolves to an existing *file* (e.g. "apm marketplace add
// ./dir/marketplace.json") switches to direct-read mode -- Path is left ""
// instead of the default probing path -- mirroring the Python original's
// _local_source_points_to_file.
func TestParseMarketplaceSource_LocalPathPointingToFile(t *testing.T) {
	// Arrange
	dir := t.TempDir()
	manifestFile := filepath.Join(dir, "marketplace.json")
	if err := os.WriteFile(manifestFile, []byte(`{"name":"acme"}`), 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", manifestFile, err)
	}

	// Act
	src, err := ParseMarketplaceSource(manifestFile, "")

	// Assert
	if err != nil {
		t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", manifestFile, err)
	}
	if src.Kind() != KindLocal {
		t.Errorf("Kind() = %q, want %q", src.Kind(), KindLocal)
	}
	if src.Path != "" {
		t.Errorf("Path = %q, want empty (direct-file read mode)", src.Path)
	}
	if src.URL != manifestFile {
		t.Errorf("URL = %q, want %q", src.URL, manifestFile)
	}
}

// TestParseMarketplaceSource_RejectsBareHTTP covers mkt-010 rule 2: a bare
// http:// SOURCE is a hard error, and the error must never echo the raw
// value (it could carry embedded credentials in userinfo -- credsec).
func TestParseMarketplaceSource_RejectsBareHTTP(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"plain http URL", "http://example.com/repo"},
		{"http URL with embedded credentials", "http://user:sekret@example.com/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err == nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
			}
			if src != nil {
				t.Errorf("src = %#v, want nil on error", src)
			}
			if strings.Contains(err.Error(), "sekret") {
				t.Errorf("error message leaked the embedded credential: %v", err)
			}
		})
	}
}

// TestParseMarketplaceSource_SCPStyleSSH covers mkt-010 rule 3: an SCP-style
// SSH remote is classified by its embedded host, and the URL is preserved
// verbatim (the SSH transport must survive for a later git clone).
func TestParseMarketplaceSource_SCPStyleSSH(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantKind  SourceKind
		wantHost  string
		wantOwner string
		wantRepo  string
	}{
		{"github host", "git@github.com:owner/repo.git", KindGitHub, "github.com", "owner", "repo"},
		{"gitlab host", "git@gitlab.com:owner/repo.git", KindGitLab, "gitlab.com", "owner", "repo"},
		{"generic host", "git@git.example.com:owner/repo.git", KindGit, "git.example.com", "owner", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err != nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", raw, err)
			}
			if src.URL != raw {
				t.Errorf("URL = %q, want verbatim %q", src.URL, raw)
			}
			if src.Kind() != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", src.Kind(), tt.wantKind)
			}
			if src.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", src.Host, tt.wantHost)
			}
			if src.Owner != tt.wantOwner || src.Repo != tt.wantRepo {
				t.Errorf("Owner/Repo = %q/%q, want %q/%q", src.Owner, src.Repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// TestParseMarketplaceSource_FullHTTPSURL covers mkt-010 rule 4: the
// direct-manifest-URL shortcut is limited to a path ending in
// "/marketplace.json" (a trailing slash still counts, any other .json
// filename does not), and everything else falls back to host-based
// classification.
func TestParseMarketplaceSource_FullHTTPSURL(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantKind  SourceKind
		wantPath  string
		wantOwner string
		wantRepo  string
	}{
		{"direct manifest URL", "https://example.com/repo/marketplace.json", KindURL, "", "", ""},
		{"direct manifest URL with trailing slash", "https://example.com/repo/marketplace.json/", KindURL, "", "", ""},
		{"arbitrary json filename does not count", "https://example.com/repo/other.json", KindGit, defaultManifestPath, "repo", "other.json"},
		{"github host", "https://github.com/owner/repo", KindGitHub, defaultManifestPath, "owner", "repo"},
		{"gitlab host", "https://gitlab.com/owner/repo", KindGitLab, defaultManifestPath, "owner", "repo"},
		{"unallowlisted self-managed gitlab host is generic git", "https://gitlab.example.com/owner/repo", KindGit, defaultManifestPath, "owner", "repo"},
		{"generic git host", "https://git.example.com/owner/repo", KindGit, defaultManifestPath, "owner", "repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err != nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", raw, err)
			}
			if src.Kind() != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", src.Kind(), tt.wantKind)
			}
			if src.Path != tt.wantPath {
				t.Errorf("Path = %q, want %q", src.Path, tt.wantPath)
			}
			if tt.wantOwner != "" && (src.Owner != tt.wantOwner || src.Repo != tt.wantRepo) {
				t.Errorf("Owner/Repo = %q/%q, want %q/%q", src.Owner, src.Repo, tt.wantOwner, tt.wantRepo)
			}
		})
	}
}

// TestParseMarketplaceSource_FullHTTPSURL_RejectsEmbeddedCredentials covers
// the credsec guard on rule 4: a URL with userinfo is rejected outright,
// and the error must not echo the credential.
func TestParseMarketplaceSource_FullHTTPSURL_RejectsEmbeddedCredentials(t *testing.T) {
	// Arrange
	raw := "https://user:sekret@github.com/owner/repo"

	// Act
	src, err := ParseMarketplaceSource(raw, "")

	// Assert
	if err == nil {
		t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
	}
	if src != nil {
		t.Errorf("src = %#v, want nil on error", src)
	}
	if strings.Contains(err.Error(), "sekret") {
		t.Errorf("error message leaked the embedded credential: %v", err)
	}
}

// TestParseMarketplaceSource_FullHTTPSURL_OwnerRepoShapeValidation covers
// mkt B4: a github/gitlab-family host needs at least two path segments
// (OWNER/REPO); a generic git host may have just one; and any https URL
// with zero path segments at all is always a hard error, regardless of
// host.
func TestParseMarketplaceSource_FullHTTPSURL_OwnerRepoShapeValidation(t *testing.T) {
	t.Run("github host with a single path segment is rejected", func(t *testing.T) {
		raw := "https://github.com/owner"
		src, err := ParseMarketplaceSource(raw, "")
		if err == nil {
			t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
		}
		if src != nil {
			t.Errorf("src = %#v, want nil on error", src)
		}
		if !strings.Contains(err.Error(), "OWNER/REPO") {
			t.Errorf("error = %v, want it to mention 'OWNER/REPO'", err)
		}
	})

	t.Run("gitlab host with a single path segment is rejected", func(t *testing.T) {
		raw := "https://gitlab.com/owner"
		_, err := ParseMarketplaceSource(raw, "")
		if err == nil {
			t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
		}
	})

	t.Run("generic git host with a single path segment is accepted", func(t *testing.T) {
		raw := "https://git.example.com/repo"
		src, err := ParseMarketplaceSource(raw, "")
		if err != nil {
			t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", raw, err)
		}
		if src.Kind() != KindGit {
			t.Errorf("Kind() = %q, want %q", src.Kind(), KindGit)
		}
		if src.Owner != "repo" {
			t.Errorf("Owner = %q, want %q", src.Owner, "repo")
		}
	})

	t.Run("no path segments at all is rejected regardless of host", func(t *testing.T) {
		tests := []string{"https://example.com", "https://example.com/", "https://github.com"}
		for _, raw := range tests {
			_, err := ParseMarketplaceSource(raw, "")
			if err == nil {
				t.Errorf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
			}
		}
	})
}

// TestParseMarketplaceSource_FullHTTPSURL_RejectsTraversalSegments covers
// mkt B3 for the https URL form: any path segment shaped like a traversal
// marker is a hard error, including one that only reveals itself as such
// after percent-decoding (e.g. "%2E%2E").
func TestParseMarketplaceSource_FullHTTPSURL_RejectsTraversalSegments(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"literal dotdot segment", "https://github.com/owner/../repo"},
		{"literal dot segment", "https://github.com/owner/./repo"},
		{"literal tilde segment", "https://github.com/owner/~"},
		{"percent-encoded dotdot segment", "https://github.com/owner/%2E%2E"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err == nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
			}
			if src != nil {
				t.Errorf("src = %#v, want nil on error", src)
			}
		})
	}
}

// TestParseMarketplaceSource_Shorthand covers mkt-010 rule 5: OWNER/REPO
// falls back to defaultSourceHost, HOST/OWNER/REPO uses its embedded host
// (only when the first segment has FQDN shape, mkt B2), and a --host
// override applies whenever there is no embedded host to conflict with (a
// --host that *agrees* with an embedded host is also fine, mkt B1 -- see
// TestParseMarketplaceSource_ShorthandHostConflictIsHardError for the
// disagreeing case, now a hard error instead of a silent override).
func TestParseMarketplaceSource_Shorthand(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		host     string
		wantKind SourceKind
		wantHost string
		wantURL  string
	}{
		{"owner/repo defaults to github.com", "owner/repo", "", KindGitHub, "github.com", "https://github.com/owner/repo"},
		{"host/owner/repo uses embedded gitlab host (unallowlisted -> generic git)", "gitlab.example.com/owner/repo", "", KindGit, "gitlab.example.com", "https://gitlab.example.com/owner/repo"},
		{"host/owner/repo uses embedded generic host", "git.example.com/owner/repo", "", KindGit, "git.example.com", "https://git.example.com/owner/repo"},
		{"--host overrides default for owner/repo", "owner/repo", "gitlab.com", KindGitLab, "gitlab.com", "https://gitlab.com/owner/repo"},
		{"--host agreeing with embedded host for host/owner/repo is a no-op (unallowlisted -> generic git)", "gitlab.example.com/owner/repo", "gitlab.example.com", KindGit, "gitlab.example.com", "https://gitlab.example.com/owner/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw, host := tt.raw, tt.host

			// Act
			src, err := ParseMarketplaceSource(raw, host)

			// Assert
			if err != nil {
				t.Fatalf("ParseMarketplaceSource(%q, %q) returned error: %v", raw, host, err)
			}
			if src.Kind() != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", src.Kind(), tt.wantKind)
			}
			if src.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", src.Host, tt.wantHost)
			}
			if src.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", src.URL, tt.wantURL)
			}
			if src.Owner != "owner" || src.Repo != "repo" {
				t.Errorf("Owner/Repo = %q/%q, want owner/repo", src.Owner, src.Repo)
			}
		})
	}
}

// TestParseMarketplaceSource_HostConflictIsHardError covers mkt-011
// (revised): a --host that disagrees with a full URL's embedded host,
// including the direct-manifest-URL shortcut, or a shorthand SOURCE's
// embedded host (mkt B1) is exit-1-worthy: a non-nil error and no
// partially-built source.
func TestParseMarketplaceSource_HostConflictIsHardError(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		host string
	}{
		{"conflicts with a repo URL's host", "https://github.com/owner/repo", "gitlab.com"},
		{"conflicts with a direct manifest URL's host", "https://example.com/repo/marketplace.json", "other.example.com"},
		{"conflicts with a HOST/OWNER/REPO shorthand's embedded host", "somehost.example.com/owner/repo", "github.com"},
		{"conflicts with a nested-group shorthand's embedded host", "gitlab.example.com/group/subgroup/repo", "github.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw, host := tt.raw, tt.host

			// Act
			src, err := ParseMarketplaceSource(raw, host)

			// Assert
			if err == nil {
				t.Fatalf("ParseMarketplaceSource(%q, --host=%q) returned no error, want a hard error", raw, host)
			}
			if src != nil {
				t.Errorf("src = %#v, want nil on error", src)
			}
		})
	}
}

// TestParseMarketplaceSource_HostIgnoredWarnsAndSucceeds covers mkt-011
// (revised)'s three non-fatal cases: a --host that matches a full URL's
// host, targets a local source, or mismatches an SCP remote's host is
// ignored (never applied) and produces a warning, not an error.
func TestParseMarketplaceSource_HostIgnoredWarnsAndSucceeds(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		host       string
		wantHost   string
		wantKind   SourceKind
		wantWarned bool
	}{
		{"matches a repo URL's own host", "https://github.com/owner/repo", "github.com", "github.com", KindGitHub, true},
		{"local source ignores any --host", "./relative", "github.com", "", KindLocal, true},
		{"mismatches an SCP remote's host", "git@github.com:owner/repo.git", "gitlab.com", "github.com", KindGitHub, true},
		{"matches an SCP remote's host silently", "git@github.com:owner/repo.git", "github.com", "github.com", KindGitHub, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw, host := tt.raw, tt.host
			var src *MarketplaceSource
			var err error

			// Act
			stderr := captureStderr(t, func() {
				src, err = ParseMarketplaceSource(raw, host)
			})

			// Assert
			if err != nil {
				t.Fatalf("ParseMarketplaceSource(%q, --host=%q) returned error: %v", raw, host, err)
			}
			if src.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q (the --host override must be ignored)", src.Host, tt.wantHost)
			}
			if src.Kind() != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", src.Kind(), tt.wantKind)
			}
			gotWarned := strings.Contains(stderr, "--host")
			if gotWarned != tt.wantWarned {
				t.Errorf("stderr warning presence = %v (output: %q), want %v", gotWarned, stderr, tt.wantWarned)
			}
		})
	}
}

// TestParseMarketplaceSource_ShorthandHostMatchesEmbeddedSilently covers
// mkt B1's "equal -> normal use, no warning" half of mkt-011 (revised) for
// shorthand SOURCE strings, in contrast to the full-URL case (rule 4),
// which warns even on a match.
func TestParseMarketplaceSource_ShorthandHostMatchesEmbeddedSilently(t *testing.T) {
	// Arrange
	raw, host := "gitlab.example.com/owner/repo", "gitlab.example.com"
	var src *MarketplaceSource
	var err error

	// Act
	stderr := captureStderr(t, func() {
		src, err = ParseMarketplaceSource(raw, host)
	})

	// Assert
	if err != nil {
		t.Fatalf("ParseMarketplaceSource(%q, --host=%q) returned error: %v", raw, host, err)
	}
	if src.Host != host {
		t.Errorf("Host = %q, want %q", src.Host, host)
	}
	if strings.Contains(stderr, "--host") {
		t.Errorf("stderr = %q, want no --host warning when --host matches the shorthand's embedded host", stderr)
	}
}

// TestParseMarketplaceSource_ShorthandNestedGroups covers mkt B2: the first
// "/"-segment of a shorthand SOURCE is only treated as an embedded HOST
// when it has FQDN shape; otherwise (and for any additional segments beyond
// HOST/OWNER/REPO) the whole prefix up to the final segment is an OWNER
// path that may itself contain "/" (nested groups), with 4+ segment forms
// supported in both the HOST-prefixed and bare-OWNER-path shapes.
func TestParseMarketplaceSource_ShorthandNestedGroups(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		host      string
		wantKind  SourceKind
		wantHost  string
		wantOwner string
		wantRepo  string
		wantURL   string
	}{
		{
			"OWNER/GROUP/REPO with non-FQDN first segment has no embedded host",
			"owner/repo/extra/segment", "",
			KindGitHub, "github.com", "owner/repo/extra", "segment",
			"https://github.com/owner/repo/extra/segment",
		},
		{
			"HOST/OWNER/GROUP/REPO uses the FQDN-shaped first segment as host",
			"gitlab.example.com/group/subgroup/repo", "",
			KindGit, "gitlab.example.com", "group/subgroup", "repo",
			"https://gitlab.example.com/group/subgroup/repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw, host := tt.raw, tt.host

			// Act
			src, err := ParseMarketplaceSource(raw, host)

			// Assert
			if err != nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned error: %v", raw, err)
			}
			if src.Kind() != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", src.Kind(), tt.wantKind)
			}
			if src.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", src.Host, tt.wantHost)
			}
			if src.Owner != tt.wantOwner || src.Repo != tt.wantRepo {
				t.Errorf("Owner/Repo = %q/%q, want %q/%q", src.Owner, src.Repo, tt.wantOwner, tt.wantRepo)
			}
			if src.URL != tt.wantURL {
				t.Errorf("URL = %q, want %q", src.URL, tt.wantURL)
			}
		})
	}
}

// TestParseMarketplaceSource_ShorthandRejectsTraversalSegments covers mkt
// B3 for the shorthand form: any owner or repo segment shaped like a
// traversal marker (".", "..", "~") is a hard error.
func TestParseMarketplaceSource_ShorthandRejectsTraversalSegments(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"dotdot in owner path", "owner/../repo"},
		{"dot in owner path", "owner/./repo"},
		{"tilde in owner path", "owner/~/repo"},
		{"dotdot as repo name", "owner/.."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err == nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
			}
			if src != nil {
				t.Errorf("src = %#v, want nil on error", src)
			}
		})
	}
}

// TestParseMarketplaceSource_RejectsUnrecognizedShape covers SOURCE strings
// that still fit none of the five rules after mkt B2's nested-group
// widening: a single bare segment (no "/" at all, so not even an
// OWNER/REPO shape), and an FQDN-shaped first segment with nothing past it
// but a single more segment (looks like it wants HOST/OWNER/REPO but is
// missing REPO).
func TestParseMarketplaceSource_RejectsUnrecognizedShape(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"single bare segment", "onlyowner"},
		{"FQDN-shaped first segment with too few remaining segments", "example.com/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			raw := tt.raw

			// Act
			src, err := ParseMarketplaceSource(raw, "")

			// Assert
			if err == nil {
				t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
			}
			if src != nil {
				t.Errorf("src = %#v, want nil on error", src)
			}
		})
	}
}

// TestParseMarketplaceSource_RejectsEmpty covers the trivial empty-SOURCE
// input.
func TestParseMarketplaceSource_RejectsEmpty(t *testing.T) {
	// Arrange
	raw := "   "

	// Act
	src, err := ParseMarketplaceSource(raw, "")

	// Assert
	if err == nil {
		t.Fatalf("ParseMarketplaceSource(%q) returned no error, want a rejection", raw)
	}
	if src != nil {
		t.Errorf("src = %#v, want nil on error", src)
	}
}
