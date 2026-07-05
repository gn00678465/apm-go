package marketplace

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// TestDetectCrossRepoMisconfigRisk covers mkt-028's risk-determination
// function in isolation: which combinations of marketplace host, plugin
// dict source, and repo field trigger the sentinel, and which are exempt.
func TestDetectCrossRepoMisconfigRisk(t *testing.T) {
	enterpriseMkt := &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "corp.ghe.com"}

	tests := []struct {
		name     string
		plugin   *MarketplacePlugin
		src      *MarketplaceSource
		depRef   *manifest.DependencyReference
		wantRisk bool
	}{
		{
			name:     "bare owner/repo on enterprise GHE marketplace triggers",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "some-org/some-repo"}},
			src:      enterpriseMkt,
			wantRisk: true,
		},
		{
			name:     "public github.com marketplace is exempt (not enterprise)",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "some-org/some-repo"}},
			src:      &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "github.com"},
			wantRisk: false,
		},
		{
			name:     "non-GitHub-family enterprise host is exempt (gitlab)",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "some-org/some-repo"}},
			src:      &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.example.com"},
			wantRisk: false,
		},
		{
			name:     "host-qualified repo field (public github.com, cross-host) is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "github.com/some-org/some-repo"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "host-qualified repo field (marketplace's own enterprise host) is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "corp.ghe.com/some-org/some-repo"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "full HTTPS URL repo field is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "https://github.com/some-org/some-repo"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "SCP-style SSH remote repo field is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "git@github.com:some-org/some-repo.git"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "in-marketplace repo field (matches marketplace project) is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "acme-owner/acme-repo"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "structured DepRef already set is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "some-org/some-repo"}},
			src:      enterpriseMkt,
			depRef:   &manifest.DependencyReference{},
			wantRisk: false,
		},
		{
			name:     "relative-string source is exempt (not a dict source)",
			plugin:   &MarketplacePlugin{Name: "p", Source: "./plugins/p"},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "non-github dict type is exempt (git-subdir)",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "git-subdir", "repo": "some-org/some-repo"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "repo field missing a slash is exempt",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "no-slash"}},
			src:      enterpriseMkt,
			wantRisk: false,
		},
		{
			name:     "'repository' alias field is honored the same as 'repo'",
			plugin:   &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repository": "some-org/some-repo"}},
			src:      enterpriseMkt,
			wantRisk: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			got := detectCrossRepoMisconfigRisk(tt.plugin, tt.src, tt.depRef)

			// Assert
			if (got != nil) != tt.wantRisk {
				t.Errorf("detectCrossRepoMisconfigRisk() = %+v, want risk=%v", got, tt.wantRisk)
			}
		})
	}
}

// TestDetectCrossRepoMisconfigRisk_GHESEnvVar covers design.md gaps A5's GHES
// boundary: a GITHUB_HOST-configured self-hosted GitHub Enterprise Server
// host must ALSO trigger the gate, not just the "*.ghe.com" suffix form.
func TestDetectCrossRepoMisconfigRisk_GHESEnvVar(t *testing.T) {
	// Arrange
	t.Setenv("GITHUB_HOST", "ghes.corp.io")
	src := &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "ghes.corp.io"}
	plugin := &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "some-org/some-repo"}}

	// Act
	got := detectCrossRepoMisconfigRisk(plugin, src, nil)

	// Assert
	if got == nil {
		t.Error("detectCrossRepoMisconfigRisk() = nil, want a risk for a GITHUB_HOST-configured GHES marketplace")
	}
}

// TestDetectCrossRepoMisconfigRisk_GHESEnvVar_UnrelatedHostUnaffected proves
// the GITHUB_HOST env var only widens the boundary for the host it names,
// not for any arbitrary enterprise-looking host.
func TestDetectCrossRepoMisconfigRisk_GHESEnvVar_UnrelatedHostUnaffected(t *testing.T) {
	// Arrange
	t.Setenv("GITHUB_HOST", "ghes.corp.io")
	src := &MarketplaceSource{Owner: "acme-owner", Repo: "acme-repo", Host: "gitlab.example.com"}
	plugin := &MarketplacePlugin{Name: "p", Source: map[string]any{"type": "github", "repo": "some-org/some-repo"}}

	// Act
	got := detectCrossRepoMisconfigRisk(plugin, src, nil)

	// Assert
	if got != nil {
		t.Errorf("detectCrossRepoMisconfigRisk() = %+v, want nil (gitlab.example.com is not the configured GITHUB_HOST)", got)
	}
}

// TestCrossRepoMisconfigRisk_ErrorMessage covers the two-remediation-option
// error text the mkt-028 checklist item requires.
func TestCrossRepoMisconfigRisk_ErrorMessage(t *testing.T) {
	// Arrange
	risk := &CrossRepoMisconfigRisk{
		MarketplaceHost:        "corp.ghe.com",
		BareRepoField:          "some-org/some-repo",
		SuggestedQualifiedRepo: "corp.ghe.com/some-org/some-repo",
	}

	// Act
	msg := risk.Error()

	// Assert
	for _, want := range []string{"corp.ghe.com/some-org/some-repo", "github.com/some-org/some-repo"} {
		if !strings.Contains(msg, want) {
			t.Errorf("CrossRepoMisconfigRisk.Error() = %q, want it to contain %q", msg, want)
		}
	}
}
