package marketplace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

// gitCloneTempDirPrefix names the os.MkdirTemp pattern fetchGit clones into,
// exported at package scope (not just a literal inline) so
// client_git_test.go can glob for leftover directories and prove the
// defer os.RemoveAll cleanup actually fires.
const gitCloneTempDirPrefix = "apm-marketplace-git-"

// fetchGit retrieves a KindGit source's manifest via a shallow clone into a
// temporary directory, reading the manifest with the same mkt-003 probe
// order fetchLocal uses on a plain local checkout, then removing the
// temporary clone (defer, so cleanup happens on every return path). This is
// the generic-git fallback (self-hosted GitLab, ADO, Gitea, plain git/SSH
// remotes, ...): classifySourceHost only ever routes github.com/gitlab-family
// hosts to fetchGitHub/fetchGitLab, so this path never runs for those.
//
// Unlike fetchGitHub/fetchGitLab, this function never reads GITHUB_APM_PAT/
// GITLAB_APM_PAT nor sets any auth header/credential -- mkt-011 trusts only
// the github/gitlab host families with a token, and this fallback exists
// precisely because the host is neither, so there is structurally nothing to
// forward here (a plain `git clone` subprocess, same as
// internal/gitops/clone.go's pattern but independently implemented per
// design.md, since that loader is coupled to the apm_modules dependency
// install flow rather than "read one marketplace.json").
func fetchGit(ctx context.Context, s *MarketplaceSource) (*MarketplaceManifest, error) {
	ref := s.Ref
	if ref == "" {
		ref = defaultSourceRef
	}

	tmpDir, err := os.MkdirTemp("", gitCloneTempDirPrefix+"*")
	if err != nil {
		return nil, fmt.Errorf("create temp clone directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := shallowCloneGit(ctx, s.URL, ref, tmpDir); err != nil {
		return nil, fmt.Errorf("clone marketplace source: %w", err)
	}

	return fetchLocal(ctx, &MarketplaceSource{URL: tmpDir, Path: s.Path})
}

// commitSHARe matches a full 40-character git commit SHA (case-insensitive:
// the ref is lowercased before matching against it) -- `git clone --branch`
// only resolves a branch or tag name, never an arbitrary commit, but
// mkt-010's --ref/#ref parsing does allow a SOURCE to pin to a commit SHA.
var commitSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

// shallowCloneGit clones remote into dir at ref. For a branch/tag ref (the
// common case), this runs `git clone --depth 1 --branch <ref> <remote>
// <dir>`, mirroring internal/gitops.RealPackageLoader.cloneRepo's shape (a
// shallow clone pinned to a branch/tag ref). For a SHA-shaped ref, `--branch`
// cannot resolve it at all, so this instead runs a full clone (no --depth,
// no --branch) followed by `git checkout <ref>`.
func shallowCloneGit(ctx context.Context, remote, ref, dir string) error {
	if commitSHARe.MatchString(strings.ToLower(ref)) {
		return cloneAndCheckoutSHA(ctx, remote, ref, dir)
	}
	args := []string{"clone", "--depth", "1", "--branch", ref, remote, dir}
	cmd := exec.CommandContext(ctx, "git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	return nil
}

// cloneAndCheckoutSHA runs a full (non-shallow) `git clone <remote> <dir>`
// followed by `git checkout <ref>`, the only way to pin a clone to an
// arbitrary commit SHA that may not be reachable via --depth 1's shallow
// history from the remote's current branch tip.
func cloneAndCheckoutSHA(ctx context.Context, remote, ref, dir string) error {
	cloneCmd := exec.CommandContext(ctx, "git", "clone", remote, dir)
	out, err := cloneCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	checkoutCmd := exec.CommandContext(ctx, "git", "-C", dir, "checkout", ref)
	out, err = checkoutCmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	return nil
}
