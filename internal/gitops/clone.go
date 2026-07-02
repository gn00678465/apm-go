package gitops

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/archive"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// RealPackageLoader implements resolver.PackageLoader via git clone.
type RealPackageLoader struct {
	ModulesDir  string
	DefaultHost string
	Lock        *lockfile.Lockfile
}

func (r *RealPackageLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	if ref.IsLocal {
		return r.loadLocalPackage(ref.LocalPath)
	}

	installDir, err := r.installPath(ref)
	if err != nil {
		return nil, err
	}

	if info, statErr := os.Stat(installDir); statErr == nil && info.IsDir() {
		if checkoutMatchesRef(installDir, resolvedRef) {
			return r.parseSubManifest(installDir)
		}
		// Stale/mismatched checkout (req-lk-007): skipping the clone here
		// would silently keep wrong content forever, changing the
		// observable post-install result versus a fresh install.
		if err := os.RemoveAll(installDir); err != nil {
			return nil, fmt.Errorf("remove stale checkout %s: %w", installDir, err)
		}
	}

	cloneURL := r.resolveCloneURL(ref)
	if err := r.cloneRepo(cloneURL, installDir, resolvedRef); err != nil {
		return nil, fmt.Errorf("clone %s: %w", cloneURL, err)
	}

	return r.parseSubManifest(installDir)
}

// checkoutMatchesRef reports whether installDir's current HEAD already
// equals resolvedRef AND the working tree is clean, resolved LOCALLY (no
// network) inside the existing checkout. resolvedRef may be a tag, branch,
// or commit SHA -- all resolve the same way via `git rev-parse <ref>^{commit}`
// (the ^{commit} peel handles annotated tags, which otherwise resolve to
// their own tag-object SHA rather than the commit they point at). Any
// failure (ref not found locally, not a git repo, dirty worktree, etc.) is
// treated as a mismatch: fail-safe, not fail-open. A dirty/modified working
// tree is treated as a mismatch even at the right commit, since req-lk-007
// requires the skip to never change the observable post-install result
// versus a fresh install.
func checkoutMatchesRef(installDir, resolvedRef string) bool {
	if resolvedRef == "" {
		return false
	}
	head, err := ResolveCommit(installDir)
	if err != nil {
		return false
	}
	resolved, err := resolveRefLocally(installDir, resolvedRef)
	if err != nil {
		return false
	}
	if head != resolved {
		return false
	}
	return worktreeClean(installDir)
}

func resolveRefLocally(repoDir, ref string) (string, error) {
	cmd := exec.Command("git", "rev-parse", ref+"^{commit}")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse %s in %s: %w", ref, repoDir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// worktreeClean reports whether repoDir has no uncommitted, untracked, or
// ignored changes. A fresh clone never contains an ignored file (nothing
// generates them at clone time), so the presence of one means some other
// process added content this checkout wouldn't otherwise have -- --ignored
// surfaces that (plain `git status --porcelain` omits ignored files).
func worktreeClean(repoDir string) bool {
	cmd := exec.Command("git", "status", "--porcelain", "--ignored")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) == 0
}

func (r *RealPackageLoader) installPath(ref *manifest.DependencyReference) (string, error) {
	key := ref.RepoURL
	if ref.VirtualPath != "" {
		key += "/" + ref.VirtualPath
	}
	// RepoURL/VirtualPath are only charset-validated at manifest-parse time
	// and do not reject ".." segments -- guard against a crafted dependency
	// resolving outside ModulesDir, or landing on an unrelated sibling
	// directory still technically inside it, before this path is used for a
	// destructive os.RemoveAll (req-lk-007's stale-checkout repair).
	if !archive.ContainedKey(r.ModulesDir, key) {
		return "", fmt.Errorf("refusing to resolve install path for %q outside %s", key, r.ModulesDir)
	}
	safe := strings.ReplaceAll(key, "/", string(filepath.Separator))
	return filepath.Join(r.ModulesDir, safe), nil
}

func (r *RealPackageLoader) resolveCloneURL(ref *manifest.DependencyReference) string {
	// Local filesystem git repo (git: ./path or git: ../path)
	if ref.Owner == "" && ref.Repo == "" && ref.RepoURL != "" {
		return ref.RepoURL
	}
	if ref.Scheme != "" {
		switch ref.Scheme {
		case "https", "http":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return ref.Scheme + "://" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
		case "ssh":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return "ssh://git@" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
		case "git":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return "git@" + host + ":" + ref.Owner + "/" + ref.Repo + ".git"
		}
	}
	host := ref.Host
	if host == "" {
		host = r.defaultHost()
	}
	return "https://" + host + "/" + ref.Owner + "/" + ref.Repo + ".git"
}

func (r *RealPackageLoader) defaultHost() string {
	if r.DefaultHost != "" {
		return r.DefaultHost
	}
	return "github.com"
}

func (r *RealPackageLoader) cloneRepo(url, dir, ref string) error {
	if isCommitSHA(ref) {
		return r.cloneRepoAtCommit(url, dir, ref)
	}

	args := []string{"clone", "--depth", "1"}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, url, dir)

	cmd := exec.Command("git", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	return nil
}

// cloneRepoAtCommit clones a repo pinned to an exact commit SHA. A shallow
// `git clone --depth 1 --branch <ref>` only accepts a branch/tag name -- git
// rejects a raw SHA with "Remote branch <sha> not found in upstream origin"
// -- so a SHA-shaped ref needs a full clone (fetches all branch/tag history,
// so the commit is guaranteed present if it's reachable from any of them)
// followed by an explicit checkout instead of the shallow-clone shorthand.
func (r *RealPackageLoader) cloneRepoAtCommit(url, dir, commit string) error {
	cloneCmd := exec.Command("git", "clone", url, dir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	checkoutCmd := exec.Command("git", "checkout", commit)
	checkoutCmd.Dir = dir
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s\n%s", err, string(out))
	}
	return nil
}

// isCommitSHA reports whether ref is a 40-character hex string (a full git
// SHA-1 commit hash), as opposed to a branch or tag name.
func isCommitSHA(ref string) bool {
	if len(ref) != 40 {
		return false
	}
	for _, c := range ref {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ResolveCommit returns the HEAD commit SHA of a cloned repo.
func ResolveCommit(repoDir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD in %s: %w", repoDir, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (r *RealPackageLoader) loadLocalPackage(path string) (*manifest.Manifest, error) {
	return r.parseSubManifest(path)
}

func (r *RealPackageLoader) parseSubManifest(dir string) (*manifest.Manifest, error) {
	apmYml := filepath.Join(dir, "apm.yml")
	data, err := os.ReadFile(apmYml)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", apmYml, err)
	}

	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		return nil, fmt.Errorf("validate %s: %w", apmYml, err)
	}
	return m, nil
}
