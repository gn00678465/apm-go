package resolver

import (
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/semver"
)

type ReferenceKind int

const (
	KindLocal ReferenceKind = iota
	KindRegistry
	KindGitSemver
	KindGitLiteral
	KindMarketplace
)

func (k ReferenceKind) String() string {
	switch k {
	case KindLocal:
		return "local"
	case KindRegistry:
		return "registry"
	case KindGitSemver:
		return "git-semver"
	case KindGitLiteral:
		return "git-literal"
	case KindMarketplace:
		return "marketplace"
	default:
		return "unknown"
	}
}

type RefType int

const (
	RefSemver RefType = iota
	RefLiteral
	RefNone
)

// ClassifyReference determines the reference kind per req-rs-008.
// Priority order: local > registry > git-semver > git-literal > marketplace.
// Deterministic function of the entry alone (no remote calls).
func ClassifyReference(ref *manifest.DependencyReference) ReferenceKind {
	if ref.IsLocal {
		return KindLocal
	}
	if ref.Source == "registry" {
		return KindRegistry
	}
	if ref.Source == "marketplace" {
		return KindMarketplace
	}
	if ClassifyRef(ref.Reference) == RefSemver {
		return KindGitSemver
	}
	return KindGitLiteral
}

// ClassifyRef determines the ref sub-type per req-rs-003.
func ClassifyRef(ref string) RefType {
	if ref == "" {
		return RefNone
	}
	if semver.IsSemverRange(ref) {
		return RefSemver
	}
	return RefLiteral
}
