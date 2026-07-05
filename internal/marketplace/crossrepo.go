// This file (crossrepo.go) implements mkt-028's cross-repo
// dependency-confusion fail-closed gate. Wired into resolve_plugin.go's
// ResolvePlugin right after the mkt-027 structured-DepRef decision, and
// BEFORE mkt-035's registered-ref propagation and mkt-021/033's version_spec
// resolution: a detected risk makes ResolvePlugin return an error
// immediately, guaranteeing zero further network activity -- in particular,
// version_spec's semver-range branch, which would otherwise call
// opts.tagLister().ListTags against this exact (possibly
// attacker-controlled) canonical coordinate. This ordering follows the
// mkt-028 checklist item's "拒絕先於任何網路探測" requirement, which takes
// priority over design.md's own literal ResolvePlugin step numbering
// (design.md lists version_spec as step 7, before this gate's step 8) per
// this task's instructions to resolve any design/checklist conflict in the
// checklist's favor.
//
// design.md sketches a `Resolution.Risk *CrossRepoRisk` field for callers to
// inspect and fail closed on. This implementation instead has ResolvePlugin
// itself return a formatted, non-nil error the moment the risk is detected:
// that makes the fail-closed behavior atomic and unconditional (it cannot
// be bypassed by a caller that forgets to check a field), avoids a
// Resolution field that would always be nil on every successfully returned
// Resolution (every risky case now returns (nil, err) instead), and already
// carries the two-remediation-option message text the checklist requires,
// so a future CLI caller (mkt-029/031 wiring) needs no special-case
// handling beyond propagating the error.
package marketplace

import (
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

// ErrCrossRepoMisconfig is returned by ResolvePlugin when mkt-028's
// dependency-confusion sentinel fires. Wrapped with %w so callers can route
// it via errors.Is.
var ErrCrossRepoMisconfig = errors.New("marketplace plugin refused: cross-repo dependency-confusion risk (mkt-028)")

// CrossRepoMisconfigRisk carries the data behind ErrCrossRepoMisconfig,
// mirroring the Python original's CrossRepoMisconfigRisk dataclass
// (resolver.py:55-84). Its Error() text supplies the two remediation
// options the mkt-028 checklist item requires ("錯誤訊息含兩個修正選項").
type CrossRepoMisconfigRisk struct {
	MarketplaceHost        string
	BareRepoField          string
	SuggestedQualifiedRepo string
}

func (r *CrossRepoMisconfigRisk) Error() string {
	return fmt.Sprintf(
		"bare `repo: %s` on enterprise marketplace %q is ambiguous -- it silently defaults to github.com, which a dependency-confusion attacker could pre-register; host-qualify the plugin's repo field in marketplace.json to one of:\n"+
			"  - %q (an enterprise dependency on this marketplace)\n"+
			"  - \"github.com/%s\" (a declared cross-host dependency on public github.com)",
		r.BareRepoField, r.MarketplaceHost, r.SuggestedQualifiedRepo, r.BareRepoField,
	)
}

// detectCrossRepoMisconfigRisk mirrors the Python original's
// _compute_cross_repo_misconfig_risk (resolver.py:299-378), adapted for
// design.md gaps A5's widened enterprise boundary: isGitHubHostname(host) &&
// host != "github.com" (*.ghe.com, OR a GITHUB_HOST-configured GHES host --
// not just the ".ghe.com" suffix the Python docstring's own prose
// describes). That widening is necessary here specifically because Go's
// sourceNeedsExplicitGitPath (resolve_plugin.go) already treats a GHES host
// the same as github.com/*.ghe.com for the mkt-027 depRef-routing decision
// (models.go's isGitHubHostname is the single shared source of truth for
// all of Kind(), mkt-027, and this gate) -- so a GHES marketplace ALSO takes
// the bare virtual-shorthand canonical path here and needs this same guard,
// unlike the Python original where a GHES marketplace is routed to a
// structured dep_ref upstream and never reaches this check at all.
//
// Returns non-nil only when ALL of:
//   - depRef is nil (a structured DepRef -- mkt-027 -- already carries an
//     explicit host; this ambiguity does not apply to it)
//   - plugin.Source is a dict whose coerced type is "github"
//   - the source is NOT in-marketplace (isInMarketplaceSource)
//   - src.Host is a GitHub-family host other than "github.com" itself
//   - the dict's repo/repository field is a non-empty "owner/repo" shape
//   - that repo field does NOT already declare an explicit host (a URL, an
//     SCP-style remote, or a "host/owner/repo" shorthand whose first
//     segment looks like a hostname -- ANY host, not just the
//     marketplace's own, counts as unambiguous declared intent)
func detectCrossRepoMisconfigRisk(plugin *MarketplacePlugin, src *MarketplaceSource, depRef *manifest.DependencyReference) *CrossRepoMisconfigRisk {
	if depRef != nil {
		return nil
	}
	dictSrc, ok := plugin.Source.(map[string]any)
	if !ok {
		return nil
	}
	if coercePluginType(dictSrc) != "github" {
		return nil
	}
	if isInMarketplaceSource(plugin, src) {
		return nil
	}
	if !isEnterpriseGitHubFamilyHost(src.Host) {
		return nil
	}

	repo := stringField(dictSrc, "repo")
	if repo == "" {
		repo = stringField(dictSrc, "repository")
	}
	bare := strings.TrimPrefix(strings.TrimSpace(repo), "/")
	if !strings.Contains(bare, "/") {
		return nil
	}
	if repoFieldDeclaresExplicitHost(bare) {
		return nil
	}

	return &CrossRepoMisconfigRisk{
		MarketplaceHost:        src.Host,
		BareRepoField:          bare,
		SuggestedQualifiedRepo: src.Host + "/" + bare,
	}
}

// isEnterpriseGitHubFamilyHost reports whether host is a GitHub-family host
// OTHER than github.com itself (design.md gaps A5: isGitHubHostname(host) &&
// host != "github.com" -- *.ghe.com, or a GITHUB_HOST-configured GHES host).
func isEnterpriseGitHubFamilyHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	return isGitHubHostname(h) && !strings.EqualFold(h, "github.com")
}

// repoFieldDeclaresExplicitHost reports whether a dict source's raw repo/
// repository field already names an explicit host -- a URL, an SCP-style
// SSH remote, or a "host/owner/repo" shorthand whose first segment looks
// like a hostname -- mirroring the escape hatch in the Python original's
// _compute_cross_repo_misconfig_risk (resolver.py:355-378). ANY
// FQDN-shaped/known-host first segment counts, not just the marketplace's
// own host: explicit cross-host intent (even to some third host) is
// unambiguous and exempt, matching the Python original's own comment that
// the goal is distinguishing "looks like a hostname" from "bare owner/repo",
// not restricting to an allowlist.
func repoFieldDeclaresExplicitHost(bare string) bool {
	lower := strings.ToLower(bare)
	var explicitHost string
	switch {
	case strings.HasPrefix(lower, "https://"), strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "ssh://"):
		u, err := url.Parse(bare)
		if err != nil {
			return false
		}
		explicitHost = u.Hostname()
	case strings.HasPrefix(bare, "git@") && strings.Contains(bare, ":"):
		rest := strings.TrimPrefix(bare, "git@")
		explicitHost, _, _ = strings.Cut(rest, ":")
	default:
		explicitHost, _, _ = strings.Cut(bare, "/")
	}
	return looksLikeSupportedGitHost(explicitHost)
}

// looksLikeSupportedGitHost mirrors the Python original's
// is_supported_git_host (utils/github_host.py:205-233), which the mkt-028
// escape hatch consults to decide "does this look like a real hostname"
// (explicit intent) as opposed to a bare owner segment: GitHub-family,
// Azure DevOps, or any valid-FQDN-shaped string.
func looksLikeSupportedGitHost(host string) bool {
	h := strings.TrimSpace(host)
	if h == "" {
		return false
	}
	if isGitHubHostname(h) || isAzureDevOpsHostname(h) {
		return true
	}
	return looksLikeFQDN(h)
}
