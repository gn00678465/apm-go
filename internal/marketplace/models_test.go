package marketplace

import (
	"encoding/json"
	"testing"
)

// TestMarketplaceSource_Kind covers mkt-001's ".kind" classification:
// local | url | github | gitlab | git, derived from the already-populated
// URL (and Path, for the direct-manifest-URL case) fields -- not from raw
// SOURCE-string parsing, which is ParseMarketplaceSource's job (step 2).
func TestMarketplaceSource_Kind(t *testing.T) {
	tests := []struct {
		name string
		src  MarketplaceSource
		want SourceKind
	}{
		// -- local paths --------------------------------------------------
		{"empty URL defaults to local", MarketplaceSource{URL: ""}, KindLocal},
		{"absolute POSIX path", MarketplaceSource{URL: "/abs/path"}, KindLocal},
		{"relative dot path", MarketplaceSource{URL: "./relative"}, KindLocal},
		{"relative dotdot path", MarketplaceSource{URL: "../relative"}, KindLocal},
		{"home-relative path", MarketplaceSource{URL: "~/marketplaces/acme"}, KindLocal},
		{"bare tilde", MarketplaceSource{URL: "~"}, KindLocal},
		{"file scheme POSIX", MarketplaceSource{URL: "file:///abs/path"}, KindLocal},
		{"windows drive letter backslash", MarketplaceSource{URL: `C:\Users\foo\marketplace`}, KindLocal},
		{"windows drive letter forward slash", MarketplaceSource{URL: "C:/Users/foo/marketplace"}, KindLocal},

		// -- direct hosted marketplace.json URL ---------------------------
		{
			"https URL naming marketplace.json with empty path is kind url",
			MarketplaceSource{URL: "https://example.com/repo/marketplace.json", Path: ""},
			KindURL,
		},
		{
			"trailing slash after marketplace.json still counts",
			MarketplaceSource{URL: "https://example.com/repo/marketplace.json/", Path: ""},
			KindURL,
		},
		{
			"non-empty path overrides the direct-manifest-URL shortcut",
			MarketplaceSource{URL: "https://example.com/repo/marketplace.json", Path: "marketplace.json"},
			KindGit,
		},
		{
			"arbitrary .json filename does not count as a direct manifest URL",
			MarketplaceSource{URL: "https://example.com/repo/other.json", Path: ""},
			KindGit,
		},

		// -- host-based classification for full https URLs ----------------
		{"github.com host", MarketplaceSource{URL: "https://github.com/owner/repo", Path: "marketplace.json"}, KindGitHub},
		{"gitlab.com host", MarketplaceSource{URL: "https://gitlab.com/owner/repo", Path: "marketplace.json"}, KindGitLab},
		{"unallowlisted self-managed gitlab host is generic git (no substring trust)", MarketplaceSource{URL: "https://gitlab.example.com/owner/repo", Path: "marketplace.json"}, KindGit},
		{"generic git host", MarketplaceSource{URL: "https://git.example.com/owner/repo", Path: "marketplace.json"}, KindGit},
		{"ghe cloud data residency host", MarketplaceSource{URL: "https://acme.ghe.com/owner/repo", Path: "marketplace.json"}, KindGitHub},

		// -- SCP-style SSH remotes ------------------------------------------
		{"scp github host", MarketplaceSource{URL: "git@github.com:owner/repo.git"}, KindGitHub},
		{"scp gitlab host", MarketplaceSource{URL: "git@gitlab.com:owner/repo.git"}, KindGitLab},
		{"scp generic host", MarketplaceSource{URL: "git@git.example.com:owner/repo.git"}, KindGit},

		// -- unqualified shorthand (no host embedded in the URL yet) ------
		{"bare owner/repo without a resolved host falls back to git", MarketplaceSource{URL: "owner/repo"}, KindGit},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			src := tt.src

			// Act
			got := src.Kind()

			// Assert
			if got != tt.want {
				t.Errorf("Kind() for URL=%q Path=%q = %q, want %q", src.URL, src.Path, got, tt.want)
			}
		})
	}
}

// TestClassifySourceHost_GitHubEnterpriseServerViaEnv covers the GHE-family
// fix (utils/github_host.py:170-198): a host matching the GITHUB_HOST env
// var (case-insensitively) classifies as KindGitHub, not the generic
// KindGit fallback -- and reverts to KindGit the moment the env var is
// unset or points elsewhere.
func TestClassifySourceHost_GitHubEnterpriseServerViaEnv(t *testing.T) {
	t.Run("no GITHUB_HOST set stays generic git", func(t *testing.T) {
		if got := classifySourceHost("ghe.example.com"); got != KindGit {
			t.Errorf("classifySourceHost(%q) = %q, want %q", "ghe.example.com", got, KindGit)
		}
	})

	t.Run("GITHUB_HOST match classifies as KindGitHub", func(t *testing.T) {
		t.Setenv(githubHostEnvVar, "ghe.example.com")
		if got := classifySourceHost("ghe.example.com"); got != KindGitHub {
			t.Errorf("classifySourceHost(%q) = %q, want %q", "ghe.example.com", got, KindGitHub)
		}
	})

	t.Run("GITHUB_HOST match is case-insensitive", func(t *testing.T) {
		t.Setenv(githubHostEnvVar, "GHE.Example.COM")
		if got := classifySourceHost("ghe.example.com"); got != KindGitHub {
			t.Errorf("classifySourceHost(%q) = %q, want %q", "ghe.example.com", got, KindGitHub)
		}
	})

	t.Run("GITHUB_HOST set to a different host does not reclassify this one", func(t *testing.T) {
		t.Setenv(githubHostEnvVar, "other.example.com")
		if got := classifySourceHost("ghe.example.com"); got != KindGit {
			t.Errorf("classifySourceHost(%q) = %q, want %q", "ghe.example.com", got, KindGit)
		}
	})

	t.Run("GITHUB_HOST accidentally set to gitlab.com never reclassifies gitlab.com as GitHub", func(t *testing.T) {
		t.Setenv(githubHostEnvVar, "gitlab.com")
		if got := classifySourceHost("gitlab.com"); got != KindGitLab {
			t.Errorf("classifySourceHost(%q) = %q, want %q (a misconfigured GITHUB_HOST must not steal gitlab.com)", "gitlab.com", got, KindGitLab)
		}
	})
}

// TestClassifySourceHost_GHECloud covers the "*.ghe.com" half of the
// GHE-family fix: classified as KindGitHub with no env var needed at all.
func TestClassifySourceHost_GHECloud(t *testing.T) {
	tests := []string{"acme.ghe.com", "ACME.GHE.COM", "tenant.sub.ghe.com"}
	for _, host := range tests {
		t.Run(host, func(t *testing.T) {
			if got := classifySourceHost(host); got != KindGitHub {
				t.Errorf("classifySourceHost(%q) = %q, want %q", host, got, KindGitHub)
			}
		})
	}
}

// TestMarketplaceManifest_TolerateRegistryKey covers mkt-005's revised
// requirement: a plugin entry carrying a "registry" key must parse
// successfully (no error) with the value ignored for routing purposes --
// the Python original shipped the parsing layer but never wired routing
// behavior on top of it.
func TestMarketplaceManifest_TolerateRegistryKey(t *testing.T) {
	// Arrange
	raw := []byte(`{
		"name": "acme-tools",
		"owner": "acme",
		"plugins": [
			{
				"name": "plugin-with-registry",
				"source": "./plugin-a",
				"description": "uses a dedicated registry field",
				"version": "1.2.3",
				"tags": ["foo", "bar"],
				"registry": "custom-registry"
			},
			{
				"name": "plain-plugin",
				"source": "./plugin-b"
			}
		]
	}`)

	// Act
	var manifest MarketplaceManifest
	err := json.Unmarshal(raw, &manifest)

	// Assert
	if err != nil {
		t.Fatalf("json.Unmarshal returned an error for a manifest with a 'registry' key: %v", err)
	}
	if manifest.Name != "acme-tools" {
		t.Errorf("Name = %q, want %q", manifest.Name, "acme-tools")
	}
	if len(manifest.Plugins) != 2 {
		t.Fatalf("len(Plugins) = %d, want 2", len(manifest.Plugins))
	}
	first := manifest.Plugins[0]
	if first.Name != "plugin-with-registry" {
		t.Errorf("Plugins[0].Name = %q, want %q", first.Name, "plugin-with-registry")
	}
	if src, ok := first.Source.(string); !ok || src != "./plugin-a" {
		t.Errorf("Plugins[0].Source = %#v, want %q", first.Source, "./plugin-a")
	}
	// The value is parsed (not silently dropped), but nothing in this
	// package consumes it for routing -- there is no dispatch on Registry
	// anywhere in this file or its dependents.
	if first.Registry != "custom-registry" {
		t.Errorf("Plugins[0].Registry = %q, want %q", first.Registry, "custom-registry")
	}

	second := manifest.Plugins[1]
	if second.Registry != "" {
		t.Errorf("Plugins[1].Registry = %q, want empty (no registry key present)", second.Registry)
	}
}

// TestMarketplacePlugin_SourceAsStructuredMap covers the second half of
// mkt-005's Source field: a dict-shaped source ({"type": "github", ...})
// must also parse without error, distinct from the plain relative-path
// string form.
func TestMarketplacePlugin_SourceAsStructuredMap(t *testing.T) {
	// Arrange
	raw := []byte(`{"name": "gh-plugin", "source": {"type": "github", "repo": "owner/repo"}}`)

	// Act
	var plugin MarketplacePlugin
	err := json.Unmarshal(raw, &plugin)

	// Assert
	if err != nil {
		t.Fatalf("json.Unmarshal returned an error for a structured-map source: %v", err)
	}
	m, ok := plugin.Source.(map[string]any)
	if !ok {
		t.Fatalf("Source = %#v, want map[string]any", plugin.Source)
	}
	if m["type"] != "github" || m["repo"] != "owner/repo" {
		t.Errorf("Source map = %#v, want type=github repo=owner/repo", m)
	}
}
