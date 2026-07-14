package main

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// ── mkt-030: apm.yml persist-path guard ──

// TestValidatePersistableRef_RejectsUnresolvedMarketplaceDep proves
// install.go's apm.yml persist path (persistPackages/persistPackagesToManifest)
// cannot be fed a still-unresolved marketplace reference (Source=="marketplace")
// without erroring -- mirrors the Python original's to_apm_yml_entry()
// raise ValueError guard (checklist mkt-030).
func TestValidatePersistableRef_RejectsUnresolvedMarketplaceDep(t *testing.T) {
	// Arrange
	ref := &manifest.DependencyReference{
		Source:                "marketplace",
		MarketplaceName:       "acme",
		MarketplacePluginName: "p",
	}

	// Act
	err := validatePersistableRef("p@acme", ref)

	// Assert
	if err == nil {
		t.Fatal("expected error for an unresolved marketplace dependency, got nil")
	}
	if !strings.Contains(err.Error(), "cannot persist package") {
		t.Errorf("error %q should mention \"cannot persist package\"", err.Error())
	}
	if !strings.Contains(err.Error(), "unresolved marketplace dependency") {
		t.Errorf("error %q should mention \"unresolved marketplace dependency\" (from ValidateResolved)", err.Error())
	}
}

// TestValidatePersistableRef_AllowsResolvedDep is the sanity-check
// counterpart: an ordinary resolved git dependency (what
// resolvePositionalPackage always returns in practice) passes through
// untouched.
func TestValidatePersistableRef_AllowsResolvedDep(t *testing.T) {
	// Arrange
	ref := &manifest.DependencyReference{Source: "git", RepoURL: "owner/repo"}

	// Act
	err := validatePersistableRef("owner/repo", ref)

	// Assert
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
