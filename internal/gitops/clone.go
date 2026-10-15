package gitops

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode"
	"unicode/utf8"

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

	// subdirCommits maps the module key of each subdirectory package this
	// loader cloned to the commit it was taken from.
	subdirCommits map[string]string
}

// SubdirCommit returns the commit of the repository that the subdirectory
// package with module key key was taken from, or "" when this loader did not
// clone it. The package's module directory holds only the subdirectory's
// content and no .git, so git run there answers for whatever repository
// encloses the project; the clone is the one place the commit is known.
func (r *RealPackageLoader) SubdirCommit(key string) string {
	return r.subdirCommits[path.Clean(key)]
}

func (r *RealPackageLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	// A dependency carrying LocalSourcePath is materialized by COPYING the
	// local directory into apm_modules (mirrors Python's local-dependency
	// model: apm_modules/_local/<name>/), never git-cloned -- the source may
	// be a plain (non-git) directory, and its absolute path could never be a
	// valid clone DESTINATION under apm_modules. ref.RepoURL already holds the
	// sanitized, contained apm_modules key (set by cmd/apm-go's normalizeLocalDep).
	if ref.LocalSourcePath != "" {
		return r.materializeLocalCopy(ref)
	}
	if ref.IsLocal {
		return r.loadLocalPackage(ref.LocalPath)
	}

	installDir, err := r.installPath(ref)
	if err != nil {
		return nil, err
	}

	if info, statErr := os.Stat(installDir); statErr == nil && info.IsDir() {
		if checkoutMatchesRef(installDir, resolvedRef) {
			return parseModuleManifest(installDir)
		}
		// Stale/mismatched checkout (req-lk-007): skipping the clone here
		// would silently keep wrong content forever, changing the
		// observable post-install result versus a fresh install.
		if err := os.RemoveAll(installDir); err != nil {
			return nil, fmt.Errorf("remove stale checkout %s: %w", installDir, err)
		}
	}

	cloneURL := r.resolveCloneURL(ref)
	if err := validateCloneURL(cloneURL); err != nil {
		return nil, err
	}

	if ref.VirtualPath != "" && ref.VirtualType == "subdirectory" {
		return r.cloneAndExtractSubdir(cloneURL, installDir, resolvedRef, ref.VirtualPath)
	}

	if err := r.cloneRepo(cloneURL, installDir, resolvedRef); err != nil {
		return nil, fmt.Errorf("clone %s: %w", SanitizeGitOutput(cloneURL), err)
	}

	return parseModuleManifest(installDir)
}

// cloneAndExtractSubdir clones the full repo into a temp directory, then
// moves only the VirtualPath subdirectory into installDir. This mirrors the
// Oracle's download_subdirectory_package (github_downloader.py:1363):
// clone to temp, locate subdir, copy contents to target, delete temp.
func (r *RealPackageLoader) cloneAndExtractSubdir(cloneURL, installDir, resolvedRef, virtualPath string) (*manifest.Manifest, error) {
	tmpDir, err := os.MkdirTemp("", "apm-subdir-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir for subdirectory clone: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	tmpClone := filepath.Join(tmpDir, "repo")
	if err := r.cloneRepo(cloneURL, tmpClone, resolvedRef); err != nil {
		return nil, fmt.Errorf("clone %s: %w", SanitizeGitOutput(cloneURL), err)
	}

	commit, err := ResolveCommit(tmpClone)
	if err != nil {
		return nil, err
	}

	// The package root while the subdirectory is taken out is the clone, as
	// in the Oracle (deps/github_downloader.py:1656-1670): a symlink may lead
	// elsewhere in the repository, and is copied as what it points at.
	rel, err := archive.ResolveIn(tmpClone, virtualPath)
	if err != nil {
		return nil, fmt.Errorf("subdirectory %q: %w", virtualPath, err)
	}
	subdir := filepath.Join(tmpClone, filepath.FromSlash(rel))
	info, err := os.Stat(subdir)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("subdirectory %q not found in cloned repository", virtualPath)
	}
	if err := archive.CheckTree(tmpClone, virtualPath); err != nil {
		return nil, fmt.Errorf("subdirectory %q: %w", virtualPath, err)
	}

	created := absentDirs(installDir)
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return nil, fmt.Errorf("create install dir: %w", err)
	}
	if err := copyDirContents(subdir, installDir); err != nil {
		err = errors.Join(err, os.RemoveAll(installDir))
		for _, dir := range created {
			_ = os.Remove(dir)
		}
		return nil, fmt.Errorf("extract subdirectory %q: %w", virtualPath, err)
	}

	if key, err := filepath.Rel(r.ModulesDir, installDir); err == nil {
		if r.subdirCommits == nil {
			r.subdirCommits = make(map[string]string)
		}
		r.subdirCommits[filepath.ToSlash(key)] = commit
	}

	return r.parseSubManifest(installDir)
}

// copyDirContents copies src into dst, a symlink as what it points at: src
// has passed archive.CheckTree, so each one stays in the repository and none
// loops.
func copyDirContents(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath, dstPath := filepath.Join(src, e.Name()), filepath.Join(dst, e.Name())
		info, err := os.Stat(srcPath)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := os.MkdirAll(dstPath, 0755); err != nil {
				return err
			}
			if err := copyDirContents(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return err
		}
		if err := os.WriteFile(dstPath, data, 0644); err != nil {
			return err
		}
	}
	return nil
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
	cmd := gitCommand("rev-parse", ref+"^{commit}")
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
	cmd := gitCommand("status", "--porcelain", "--ignored")
	cmd.Dir = repoDir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(string(out))) == 0
}

func (r *RealPackageLoader) installPath(ref *manifest.DependencyReference) (string, error) {
	return installPathIn(r.ModulesDir, ref)
}

func installPathIn(modulesDir string, ref *manifest.DependencyReference) (string, error) {
	key := ref.RepoURL
	if ref.VirtualPath != "" {
		key += "/" + ref.VirtualPath
	}
	// RepoURL/VirtualPath are only charset-validated at manifest-parse time
	// and do not reject ".." segments -- guard against a crafted dependency
	// resolving outside ModulesDir, or landing on an unrelated sibling
	// directory still technically inside it, before this path is used for a
	// destructive os.RemoveAll (req-lk-007's stale-checkout repair).
	if !archive.ContainedKey(modulesDir, key) {
		return "", fmt.Errorf("refusing to resolve install path for %q outside %s", key, modulesDir)
	}
	safe := strings.ReplaceAll(key, "/", string(filepath.Separator))
	return filepath.Join(modulesDir, safe), nil
}

func (r *RealPackageLoader) resolveCloneURL(ref *manifest.DependencyReference) string {
	// Local filesystem git repo (git: ./path or git: ../path)
	if ref.Owner == "" && ref.Repo == "" && ref.RepoURL != "" {
		return ref.RepoURL
	}
	repoPath := ref.RepoURL
	if repoPath == "" {
		repoPath = ref.Owner + "/" + ref.Repo
	}
	if ref.ArtifactoryPrefix != "" {
		repoPath = ref.ArtifactoryPrefix + "/" + repoPath
	}
	if ref.Scheme != "" {
		switch ref.Scheme {
		case "https", "http":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return ref.Scheme + "://" + host + "/" + repoPath + ".git"
		case "ssh":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return "ssh://git@" + host + "/" + repoPath + ".git"
		case "git":
			host := ref.Host
			if host == "" {
				host = r.defaultHost()
			}
			return "git@" + host + ":" + repoPath + ".git"
		}
	}
	host := ref.Host
	if host == "" {
		host = r.defaultHost()
	}
	return "https://" + host + "/" + repoPath + ".git"
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
	// "--" terminates option parsing: url and dir can never be mistaken for
	// git options even if a crafted value begins with "-".
	args = append(args, "--", url, dir)

	if err := ValidateURLRewriteSafety(url); err != nil {
		return err
	}
	cmd := cloneCommandFor(url, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", err, SanitizeGitOutput(string(out)))
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
	if err := ValidateURLRewriteSafety(url); err != nil {
		return err
	}
	cloneCmd := cloneCommandFor(url, "clone", "--", url, dir)
	if out, err := cloneCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s\n%s", err, SanitizeGitOutput(string(out)))
	}
	checkoutCmd := gitCommand("checkout", commit)
	checkoutCmd.Dir = dir
	if out, err := checkoutCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s\n%s", err, SanitizeGitOutput(string(out)))
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
	cmd := gitCommand("rev-parse", "HEAD")
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

// materializeLocalCopy vendors a local-directory dependency into apm_modules
// by copying it, then parses its sub-manifest -- the Go equivalent of Python's
// _copy_local_package (apm_modules/_local/<name>/). The destination is
// installPath(ref), whose apm_modules key (ref.RepoURL, e.g. "_local/foo-ab12")
// is guarded by archive.ContainedKey exactly like every other dependency, so a
// crafted key can never escape ModulesDir. The clone SOURCE guard is moot here
// (no git clone happens); the source directory is user-trusted (it came from
// the user's own apm.yml/CLI arg). A symlink under it is copied as what it
// points at when that stays inside the package, and fails the copy otherwise
// (copyTreeDereferencing); a failed copy leaves no installDir behind.
func (r *RealPackageLoader) materializeLocalCopy(ref *manifest.DependencyReference) (*manifest.Manifest, error) {
	installDir, err := r.installPath(ref)
	if err != nil {
		return nil, err
	}
	// The copy reads the source checked here, symlinks resolved: a declared
	// path through the destination link would read whatever is left once
	// RemoveAll has removed that link.
	src, err := checkLocalCopy(r.ModulesDir, ref)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(src)
	if err != nil {
		return nil, fmt.Errorf("local dependency source %s: %w", ref.LocalSourcePath, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("local dependency source %s is not a directory", ref.LocalSourcePath)
	}

	// Replace any stale prior materialization so the result matches a fresh
	// copy of the current source (parity with the stale-checkout repair on the
	// git path).
	if err := os.RemoveAll(installDir); err != nil {
		return nil, fmt.Errorf("remove stale local copy %s: %w", installDir, err)
	}
	created := absentDirs(filepath.Dir(installDir))
	if err := os.MkdirAll(filepath.Dir(installDir), 0o755); err != nil {
		return nil, err
	}
	if err := copyTreeDereferencing(src, installDir, src, nil); err != nil {
		err = errors.Join(err, os.RemoveAll(installDir))
		// The Oracle removes an apm_modules it created that is empty after
		// a failed install (install/resolution_staging.py:195-200). A
		// directory that another copy filled since is not empty and stays.
		for _, dir := range created {
			_ = os.Remove(dir)
		}
		return nil, fmt.Errorf("copy local dependency %s: %w", ref.LocalSourcePath, err)
	}

	return r.parseSubManifest(installDir)
}

// CheckLocalCopy refuses a copy of the local dependency ref into modulesDir
// that would remove or write outside its own copy: the copy step removes the
// destination and then writes the source into it. Install and update run it
// for every direct local dependency before anything is written, so a refusal
// leaves earlier copies as they were; the loader runs it again for the paths
// that skip that check. It is a no-op for a dependency that is not copied.
//
// Refused, in this order: a destination whose parent directories, symlinks
// resolved, put it outside modulesDir (a symlinked apm_modules/_local); a
// source that holds the project (the working directory); a source and
// destination where one holds the other. The last component of the
// destination is not resolved: a symlink there is removed, not followed.
//
// Deliberate deviation from the Oracle, which copies a source holding the
// project into itself until "[Errno 36] File name too long", exits 1 and
// leaves .claude/ behind.
func CheckLocalCopy(modulesDir string, ref *manifest.DependencyReference) error {
	if ref.LocalSourcePath == "" {
		return nil
	}
	_, err := checkLocalCopy(modulesDir, ref)
	return err
}

// checkLocalCopy is CheckLocalCopy that also returns the source it checked,
// with symlinks resolved.
func checkLocalCopy(modulesDir string, ref *manifest.DependencyReference) (string, error) {
	declared := ref.LocalDeclaredPath
	if declared == "" {
		declared = ref.LocalSourcePath
	}
	modules := filepath.ToSlash(filepath.Clean(modulesDir))
	dest, err := installPathIn(modulesDir, ref)
	if err != nil {
		return "", err
	}
	destParent, err := realPath(filepath.Dir(dest))
	if err != nil {
		return "", fmt.Errorf("local dependency %s: %w", declared, err)
	}
	destReal := filepath.Join(destParent, filepath.Base(dest))
	modulesReal, err := realPath(modulesDir)
	if err != nil {
		return "", fmt.Errorf("local dependency %s: %w", declared, err)
	}
	if !within(modulesReal, destReal) || destReal == modulesReal {
		return "", fmt.Errorf("local dependency %s would be copied to %s, which resolves to %s outside %s/; refusing to replace it",
			declared, filepath.ToSlash(dest), destReal, modules)
	}
	src, err := realPath(ref.LocalSourcePath)
	if err != nil {
		return "", fmt.Errorf("local dependency source %s: %w", ref.LocalSourcePath, err)
	}
	project, err := realPath(".")
	if err != nil {
		return "", fmt.Errorf("local dependency %s: %w", declared, err)
	}
	if within(src, project) {
		return "", fmt.Errorf("local dependency %s is the project directory or one of its parent directories; it cannot be copied into %s/", declared, modules)
	}
	if within(src, destReal) || within(destReal, src) {
		return "", fmt.Errorf("local dependency %s overlaps its copy %s; a package cannot be copied into or over itself", declared, filepath.ToSlash(dest))
	}
	return src, nil
}

// absentDirs lists dir and each parent above it that does not exist yet,
// deepest first: the directories os.MkdirAll(dir) would create.
func absentDirs(dir string) []string {
	var absent []string
	for ; ; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); !os.IsNotExist(err) || filepath.Dir(dir) == dir {
			return absent
		}
		absent = append(absent, dir)
	}
}

// within reports whether path is dir or below it.
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// realPath is p made absolute with every symlink resolved. The part of p
// that does not exist yet (ModulesDir before the first install) is joined
// to the real path of the nearest directory above it that does.
func realPath(p string) (string, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err == nil {
		return resolved, nil
	}
	parent := filepath.Dir(abs)
	if !os.IsNotExist(err) || parent == abs {
		return "", err
	}
	resolved, err = realPath(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, filepath.Base(abs)), nil
}

// copyTreeDereferencing copies srcDir to dstDir the way Oracle 0.33.0 copies a
// local package (install/phases/local_content.py:91-181): a symlink is resolved
// and its target copied as a regular file or directory when it lies inside
// pkgRoot; a dangling symlink, one that resolves outside pkgRoot (its target is
// not read), or one back to a directory already entered on the current path
// (visited, per branch as in the Oracle) fails the copy. Every failure reading
// the package carries the Oracle's reason, Python's own OSError and
// RuntimeError texts included. Devices, pipes and sockets are skipped: reading
// a FIFO would block.
func copyTreeDereferencing(srcDir, dstDir, pkgRoot string, visited []string) error {
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return fmt.Errorf("Cannot read package directory '%s': %s. Check that the package directory has read permissions. Local install aborted.", srcDir, pyOSError(err))
	}
	for _, e := range entries {
		srcPath := filepath.Join(srcDir, e.Name())
		dstPath := filepath.Join(dstDir, e.Name())
		info, err := e.Info()
		if err != nil {
			return errors.New(pyOSError(err))
		}
		if info.Mode()&os.ModeSymlink != 0 {
			resolved, err := realpathStrict(srcPath)
			if err != nil {
				// pathlib turns ELOOP into RuntimeError("Symlink loop from %r"),
				// which the Oracle does not catch: it is the whole reason.
				var pe *os.PathError
				if errors.As(err, &pe) && errors.Is(pe.Err, syscall.ELOOP) {
					return fmt.Errorf("Symlink loop from %s", pyRepr(pe.Path))
				}
				return fmt.Errorf("Symlink '%s' has a broken or unresolvable target: %s. Remove the dangling symlink or restore its target. Local install aborted.", srcPath, pyOSError(err))
			}
			if !within(pkgRoot, resolved) {
				return fmt.Errorf("Symlink '%s' resolves to '%s', which escapes the package root '%s'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted.", srcPath, resolved, pkgRoot)
			}
			if info, err = os.Stat(resolved); err != nil {
				return errors.New(pyOSError(err))
			}
			if info.IsDir() {
				if slices.Contains(visited, resolved) {
					return fmt.Errorf("Circular symlink detected: '%s' resolves to '%s', which was already visited during this install. Break the cycle by replacing one symlink with a real directory copy. Local install aborted.", srcPath, resolved)
				}
				if err := copyTreeDereferencing(resolved, dstPath, pkgRoot, append(slices.Clip(visited), resolved)); err != nil {
					return err
				}
				continue
			}
			srcPath = resolved
		}
		if info.IsDir() {
			if err := copyTreeDereferencing(srcPath, dstPath, pkgRoot, visited); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			continue
		}
		data, err := os.ReadFile(srcPath)
		if err != nil {
			return errors.New(pyOSError(err))
		}
		if err := os.WriteFile(dstPath, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// realpathStrict is Python 3.12's os.path.realpath(path, strict=True) for an
// absolute POSIX path (posixpath.py:420-496). It resolves one component at a
// time, so an error names the component Python's names, and a symlink met
// again while it is being resolved is ELOOP on that symlink; filepath.
// EvalSymlinks gives up after 255 links and names no path. Every error is an
// *os.PathError.
func realpathStrict(path string) (string, error) {
	return joinRealpath("", path, map[string]*string{})
}

// joinRealpath is posixpath._joinrealpath with strict=True. seen maps a
// symlink to its resolved path, or to nil while it is being resolved.
func joinRealpath(path, rest string, seen map[string]*string) (string, error) {
	if strings.HasPrefix(rest, "/") {
		rest, path = rest[1:], "/"
	}
	for rest != "" {
		var name string
		name, rest, _ = strings.Cut(rest, "/")
		switch name {
		case "", ".":
			continue
		case "..":
			path = filepath.Dir(path)
			continue
		}
		newpath := filepath.Join(path, name)
		info, err := os.Lstat(newpath)
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink == 0 {
			path = newpath
			continue
		}
		if resolved, ok := seen[newpath]; ok {
			if resolved != nil {
				path = *resolved
				continue
			}
			if _, err := os.Stat(newpath); err != nil {
				return "", err
			}
			return "", &os.PathError{Op: "stat", Path: newpath, Err: syscall.ELOOP}
		}
		seen[newpath] = nil
		target, err := os.Readlink(newpath)
		if err != nil {
			return "", err
		}
		if path, err = joinRealpath(path, target, seen); err != nil {
			return "", err
		}
		resolved := path
		seen[newpath] = &resolved
	}
	return path, nil
}

// pyOSError is str(OSError) as Python 3.12 prints err:
// "[Errno <n>] <strerror>: <repr(filename)>". Go's text for a Linux errno is
// glibc's strerror with a lowercase first letter; the two differ only for
// errno 41, 58 and 133, which no path operation returns.
func pyOSError(err error) string {
	var pe *os.PathError
	var errno syscall.Errno
	if !errors.As(err, &pe) || !errors.As(pe.Err, &errno) {
		return err.Error()
	}
	text := errno.Error()
	return fmt.Sprintf("[Errno %d] %s%s: %s", int(errno), strings.ToUpper(text[:1]), text[1:], pyRepr(pe.Path))
}

// pyRepr is Python's repr() of a str holding a path, an invalid UTF-8 byte b
// being the lone surrogate U+DC00+b Python decodes it to. It follows the rule
// of pythonReprPyStr (internal/marketplace/models.go), which gitops cannot
// import.
func pyRepr(s string) string {
	quote := '\''
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteRune(quote)
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&sb, `\u%04x`, 0xdc00+int(s[i]))
		case r == quote || r == '\\':
			sb.WriteByte('\\')
			sb.WriteRune(r)
		case r == '\n':
			sb.WriteString(`\n`)
		case r == '\r':
			sb.WriteString(`\r`)
		case r == '\t':
			sb.WriteString(`\t`)
		case unicode.IsPrint(r):
			sb.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&sb, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&sb, `\u%04x`, r)
		default:
			fmt.Fprintf(&sb, `\U%08x`, r)
		}
		i += size
	}
	sb.WriteRune(quote)
	return sb.String()
}

// parseModuleManifest is parseSubManifest for a git checkout, which keeps the
// symlinks of the repository: an apm.yml that is a symlink leaving the
// package fails, so the declared dependencies and MCP servers come from the
// package only (issue #89).
func parseModuleManifest(dir string) (*manifest.Manifest, error) {
	rel, err := archive.ResolveIn(dir, "apm.yml")
	if err != nil {
		return nil, err
	}
	return parseManifestFile(filepath.Join(dir, filepath.FromSlash(rel)))
}

func (r *RealPackageLoader) parseSubManifest(dir string) (*manifest.Manifest, error) {
	return parseManifestFile(filepath.Join(dir, "apm.yml"))
}

func parseManifestFile(apmYml string) (*manifest.Manifest, error) {
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
