package build

import (
	"errors"
	"reflect"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

// Expected strings in this file are the Oracle's (v0.33.0
// marketplace/builder.py:77-80,211-235 and drift_check.py:73-81), except the
// causes apm-go defines for its git-clone fetch (metadata_enrichment.go).

func TestEnrichRemoteMetadata_Statuses(t *testing.T) {
	secret := errors.New("fatal: https://user:hunter2@example.com/o/r.git denied")
	cases := []struct {
		name    string
		entry   authoring.PackageEntry
		fetcher MetadataFetcher
		opts    Options
		want    MetadataOutcome
	}{
		{
			name:    "explicit: curator fixes description and display version",
			entry:   authoring.PackageEntry{Name: "p", Description: "d", Version: "1.0.0"},
			fetcher: panicMetadataFetcher{},
			want:    MetadataOutcome{Package: "p", Status: MetadataExplicit},
		},
		{
			name:    "a range is not a display version, so the entry is not explicit",
			entry:   authoring.PackageEntry{Name: "p", Description: "d", Version: "^1.0.0"},
			fetcher: &fakeMetadataFetcher{version: "1.2.0"},
			want:    MetadataOutcome{Package: "p", Status: MetadataFetched},
		},
		{
			name:    "fetched: description only",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{description: "d"},
			want:    MetadataOutcome{Package: "p", Status: MetadataFetched},
		},
		{
			name:    "empty: neither field",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{},
			want:    MetadataOutcome{Package: "p", Status: MetadataEmpty},
		},
		{
			name:    "empty: a version of only whitespace does not count",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{version: "  "},
			want:    MetadataOutcome{Package: "p", Status: MetadataEmpty},
		},
		{
			name:    "failed: an untyped error gets the fixed cause, never its text",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{err: secret},
			want:    MetadataOutcome{Package: "p", Status: MetadataFailed, Cause: "remote apm.yml could not be read"},
		},
		{
			name:    "failed: timeout",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{err: &MetadataFetchError{Cause: metadataCauseTimeout, Err: secret}},
			want:    MetadataOutcome{Package: "p", Status: MetadataFailed, Cause: "request timed out"},
		},
		{
			name:    "failed: authentication",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{err: &MetadataFetchError{Cause: metadataCauseAuth, Err: secret}},
			want:    MetadataOutcome{Package: "p", Status: MetadataFailed, Cause: "authentication required"},
		},
		{
			name:    "failed: network",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: &fakeMetadataFetcher{err: &MetadataFetchError{Cause: metadataCauseNetwork, Err: secret}},
			want:    MetadataOutcome{Package: "p", Status: MetadataFailed, Cause: "network request failed"},
		},
		{
			name:    "offline: no fetch",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: panicMetadataFetcher{},
			opts:    Options{Offline: true},
			want:    MetadataOutcome{Package: "p", Status: MetadataOffline, Cause: "metadata fetch skipped by --offline"},
		},
		{
			name:    "skipped: no fetch and no status",
			entry:   authoring.PackageEntry{Name: "p"},
			fetcher: panicMetadataFetcher{},
			opts:    Options{SkipRemoteMetadata: true},
			want:    MetadataOutcome{Package: "p"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, got := enrichRemoteMetadata(c.entry, "v1", "", "owner/repo", c.fetcher, c.opts)
			if got != c.want {
				t.Errorf("outcome = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestEnrichRemoteMetadata_StripsRemoteVersion(t *testing.T) {
	_, version, _ := enrichRemoteMetadata(authoring.PackageEntry{Name: "p"}, "v1", "", "owner/repo",
		&fakeMetadataFetcher{version: " 1.2.3 "}, Options{})
	if version != "1.2.3" {
		t.Errorf("version = %q, want %q", version, "1.2.3")
	}
}

func TestCloneError_CauseFromGitStderr(t *testing.T) {
	cases := []struct {
		stderr, want string
	}{
		{"fatal: Authentication failed for 'https://example.com/o/r.git/'", "authentication required"},
		{"fatal: unable to access 'https://example.com/': Could not resolve host: example.com", "network request failed"},
		{"fatal: repository 'https://example.com/o/r.git/' not found", "remote apm.yml could not be read"},
		{"fatal: something else", "remote apm.yml could not be read"},
	}
	for _, c := range cases {
		err := cloneError(t.Context(), "https://example.com/o/r.git", []byte(c.stderr), errors.New("exit status 128"))
		var fetchErr *MetadataFetchError
		if !errors.As(err, &fetchErr) {
			t.Fatalf("cloneError(%q) = %T, want *MetadataFetchError", c.stderr, err)
		}
		if fetchErr.Cause != c.want {
			t.Errorf("cloneError(%q).Cause = %q, want %q", c.stderr, fetchErr.Cause, c.want)
		}
	}
}

func TestResolvePackages_LocalPackageIsLocalWithOrWithoutApmYML(t *testing.T) {
	root := t.TempDir()
	cfg := &authoring.AuthoringConfig{Packages: []authoring.PackageEntry{{Name: "loc", Source: "./missing"}}}
	resolved, _, err := ResolvePackages(cfg, Options{ProjectRoot: root, Lister: panicLister{}, MetadataFetcher: panicMetadataFetcher{}})
	if err != nil {
		t.Fatal(err)
	}
	if want := (MetadataOutcome{Package: "loc", Status: MetadataLocal}); resolved[0].Metadata != want {
		t.Errorf("outcome = %+v, want %+v", resolved[0].Metadata, want)
	}
}

func TestMetadataEnrichment_Certifiable(t *testing.T) {
	cases := []struct {
		statuses []MetadataStatus
		want     bool
	}{
		{nil, true},
		{[]MetadataStatus{MetadataFetched, MetadataEmpty, MetadataLocal, MetadataExplicit}, true},
		{[]MetadataStatus{MetadataFetched, MetadataFailed}, false},
		{[]MetadataStatus{MetadataOffline}, false},
		{[]MetadataStatus{""}, false},
		{[]MetadataStatus{"future"}, false},
	}
	for _, c := range cases {
		var m MetadataEnrichment
		for _, s := range c.statuses {
			m.Outcomes = append(m.Outcomes, MetadataOutcome{Package: "p", Status: s})
		}
		if got := m.Certifiable(); got != c.want {
			t.Errorf("Certifiable(%v) = %v, want %v", c.statuses, got, c.want)
		}
	}
}

func TestMetadataEnrichment_Warnings(t *testing.T) {
	m := MetadataEnrichment{Outcomes: []MetadataOutcome{
		{Package: "ok", Status: MetadataFetched},
		{Package: "off", Status: MetadataOffline, Cause: "metadata fetch skipped by --offline"},
		{Package: "host", Status: MetadataFailed, Cause: "metadata fetch is unsupported for host 'gitlab.com'"},
		{Package: "net", Status: MetadataFailed, Cause: "HTTP 503"},
		{Package: "nocause", Status: MetadataFailed},
	}}
	want := []string{
		"Package 'off': metadata enrichment skipped by --offline. Rerun without --offline when certification is required.",
		"Package 'host': metadata enrichment failed (metadata fetch is unsupported for host 'gitlab.com'). Add fixed description and version fields to certify this package.",
		"Package 'net': metadata enrichment failed (HTTP 503). Restore repository, network, or credential access and retry.",
		"Package 'nocause': metadata enrichment failed (unknown failure). Restore repository, network, or credential access and retry.",
	}
	if got := m.Warnings(); !reflect.DeepEqual(got, want) {
		t.Errorf("Warnings() =\n%q\nwant\n%q", got, want)
	}
}

func TestMetadataFor_OnlyTheClaudeProfileUsesMetadata(t *testing.T) {
	resolved := []ResolvedPackage{
		{Metadata: MetadataOutcome{Package: "a", Status: MetadataLocal}},
		{Metadata: MetadataOutcome{Package: "b", Status: MetadataFailed, Cause: "request timed out"}},
	}
	if got := MetadataFor("codex", resolved); got != nil {
		t.Errorf("MetadataFor(codex) = %+v, want nil", got)
	}
	got := MetadataFor("claude", resolved)
	want := []MetadataOutcome{resolved[0].Metadata, resolved[1].Metadata}
	if got == nil || !reflect.DeepEqual(got.Outcomes, want) {
		t.Errorf("MetadataFor(claude) = %+v, want outcomes %+v in resolved order", got, want)
	}
}

func TestCheckMarketplaceDrift_UncertifiableMetadataIsNotCompared(t *testing.T) {
	root := t.TempDir()
	cfg := driftFixtureConfig()
	cfg.Outputs = []string{"claude", "codex"}
	// An on-disk file that would be "drift" if it were compared.
	writeMarketplaceDriftFixture(t, root, map[string]any{"sentinel": "unchanged"})
	resolved := []ResolvedPackage{{
		Entry:    authoring.PackageEntry{Name: "remote-tool", Source: "./remote-tool", Category: "tools"},
		IsLocal:  true,
		Subdir:   "./remote-tool",
		Metadata: MetadataOutcome{Package: "remote-tool", Status: MetadataFailed, Cause: "transport closed"},
	}}

	report, err := CheckMarketplaceDrift(cfg, resolved, root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if report.OK {
		t.Fatal("report.OK = true, want false")
	}
	claude := report.Outputs[0]
	warning := "Package 'remote-tool': metadata enrichment failed (transport closed). Restore repository, network, or credential access and retry."
	if claude.Format != "claude" || claude.Status != "uncertifiable" || len(claude.Differences) != 0 ||
		!reflect.DeepEqual(claude.MetadataWarnings, []string{warning}) {
		t.Errorf("claude output = %+v, want uncertifiable with the metadata warning and no differences", claude)
	}
	wantMsg := []string{".claude-plugin/marketplace.json: remote metadata unavailable; regeneration is uncertifiable"}
	if got := claude.ErrorMessages(); !reflect.DeepEqual(got, wantMsg) {
		t.Errorf("claude.ErrorMessages() = %q, want %q", got, wantMsg)
	}
	// The codex profile uses no metadata, so it is still compared.
	if codex := report.Outputs[1]; codex.Format != "codex" || codex.Status != "missing" || len(codex.MetadataWarnings) != 0 {
		t.Errorf("codex output = %+v, want a compared (missing) output", codex)
	}
	if report.Metadata.Certifiable() || len(report.Metadata.Outcomes) != 1 {
		t.Errorf("report.Metadata = %+v, want the one failed outcome", report.Metadata)
	}
}

func TestCheckMarketplaceDrift_NoMetadataProfileReportsNoOutcomes(t *testing.T) {
	root := t.TempDir()
	cfg := driftFixtureConfig()
	cfg.Outputs = []string{"codex"}
	resolved := []ResolvedPackage{{
		Entry: authoring.PackageEntry{Name: "x", Source: "./x", Category: "tools"}, IsLocal: true, Subdir: "./x",
		Metadata: MetadataOutcome{Package: "x", Status: MetadataFailed},
	}}
	report, err := CheckMarketplaceDrift(cfg, resolved, root, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Metadata.Outcomes) != 0 || report.Outputs[0].Status == "uncertifiable" {
		t.Errorf("report = %+v, want no outcomes and a compared output", report)
	}
}
