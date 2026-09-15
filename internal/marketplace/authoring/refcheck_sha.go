// This file implements the two apm-go-only remote seams `check` gained in
// SPEC specs/marketplace-check-outdated (2026-09-14) on top of mkt-041:
//
//   - CommitProber: the Oracle's check.py:157 only ever compares a pinned
//     ref against the NAMES `git ls-remote` advertises, so a 40-hex commit
//     SHA -- a pin form the Oracle's own pack (builder.py:668) accepts
//     verbatim -- always reported "Ref '<sha>' not found". apm-go first
//     matches the SHA against ls-remote's commit column (a tag or tip SHA
//     needs no second round trip) and only then probes the remote with
//     `git fetch --depth 1 -- <url> <sha>` into a throwaway bare repo.
//   - ManifestVersionFetcher: Claude Code refuses to install a plugin whose
//     marketplace.json version differs from the manifest version at the
//     pinned ref, so an entry with both a ref and a display version is
//     compared against `<subdir>/.claude-plugin/plugin.json`, falling back
//     to `<subdir>/apm.yml` (the file pack's enrichRemoteMetadata reads).
//
// Both share one fetch into a scratch repository: a remote that refuses
// arbitrary-SHA fetches (uploadpack.allowAnySHA1InWant off) answers
// "not our ref" exactly as a missing commit does, so such a pin is
// reported not found (decision D-a) -- GitHub and GitLab both allow it.
package authoring

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/yamlcore"
)

// CommitProber answers "does this commit object exist on the remote?" for a
// 40-hex SHA that `git ls-remote` did not list (a commit that is not any
// ref's tip). A false with a nil error means the remote positively rejected
// the object; a non-nil error means the remote could not be asked.
type CommitProber interface {
	HasCommit(source, sha string) (bool, error)
}

// ManifestVersionFetcher reads the version a plugin declares about itself at
// ref: `<subdir>/.claude-plugin/plugin.json` first, then `<subdir>/apm.yml`.
// "" with a nil error means neither file declares a version.
type ManifestVersionFetcher interface {
	FetchManifestVersion(source, ref, subdir string) (string, error)
}

// CheckDeps bundles `check`'s three remote seams so tests can prove, with a
// panicking fake, which of them a given entry never touches.
type CheckDeps struct {
	Lister   RefLister
	Prober   CommitProber
	Manifest ManifestVersionFetcher
}

// DefaultCommitProber and DefaultManifestVersionFetcher are the production
// seams, swappable by cmd-layer tests the way DefaultRefLister is.
var (
	DefaultCommitProber           CommitProber           = gitCommitProber{}
	DefaultManifestVersionFetcher ManifestVersionFetcher = gitManifestVersionFetcher{}
)

// scratchTempRoot is the parent directory for the throwaway bare repos a
// probe or manifest fetch runs in; "" means os.TempDir(). A var so a test
// can point it at its own directory and prove every scratch repo is removed.
var scratchTempRoot = ""

const scratchTempPrefix = "apm-check-*"

// manifestReadMaxBytes caps a manifest read out of the scratch repo, the
// same defence build/metadata.go applies to a remote apm.yml.
const manifestReadMaxBytes = 1 << 20

// errRefNotOnRemote is fetchRefIntoScratch's "the remote said no such
// object/ref" outcome, distinct from a transport failure.
var errRefNotOnRemote = errors.New("ref not on remote")

// CheckPackagesWith is CheckPackages with every seam injected.
func CheckPackagesWith(dir string, cfg *AuthoringConfig, deps CheckDeps, offline bool) []CheckResult {
	results := make([]CheckResult, 0, len(cfg.Packages))
	for _, pkg := range cfg.Packages {
		results = append(results, checkPackage(dir, cfg, pkg, deps, offline))
	}
	return results
}

type gitCommitProber struct{}

func (gitCommitProber) HasCommit(source, sha string) (bool, error) {
	_, cleanup, err := fetchRefIntoScratch(source, sha)
	defer cleanup()
	if errors.Is(err, errRefNotOnRemote) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

type gitManifestVersionFetcher struct{}

func (gitManifestVersionFetcher) FetchManifestVersion(source, ref, subdir string) (string, error) {
	dir, cleanup, err := fetchRefIntoScratch(source, ref)
	defer cleanup()
	if err != nil {
		if errors.Is(err, errRefNotOnRemote) {
			return "", fmt.Errorf("ref %q vanished from %q while reading its plugin manifest", ref, source)
		}
		return "", err
	}
	base := filepath.ToSlash(subdir)

	data, err := showAtFetchHead(dir, path.Join(base, ".claude-plugin", "plugin.json"))
	if err != nil {
		return "", err
	}
	if data != nil {
		var m struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(data, &m); err != nil {
			return "", fmt.Errorf("parse plugin.json at ref %q: %w", ref, err)
		}
		if v := strings.TrimSpace(m.Version); v != "" {
			return v, nil
		}
	}

	data, err = showAtFetchHead(dir, path.Join(base, "apm.yml"))
	if err != nil || data == nil {
		return "", err
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		return "", fmt.Errorf("parse apm.yml at ref %q: %w", ref, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return "", nil
	}
	return strings.TrimSpace(scalarString(doc.Content[0], "version")), nil
}

// newProbeFetchCmd builds `git -C <dir> fetch --depth 1 -- <cloneURL> <ref>`
// under gitops.ApplyCloneEnv (file transport only for a local path).
func newProbeFetchCmd(ctx context.Context, dir, cloneURL, ref string) *exec.Cmd {
	// gc.auto=0 / maintenance.auto=false: a post-fetch auto-gc would run as
	// a detached child holding the scratch directory open, which on Windows
	// leaves an empty directory behind when the parent removes it.
	cmd := exec.CommandContext(ctx, "git", "-c", "gc.auto=0", "-c", "maintenance.auto=false",
		"-C", dir, "fetch", "--depth", "1", "--", cloneURL, ref)
	gitops.ApplyCloneEnv(cmd, cloneURL)
	// git fetch hands the transport to a git-remote-https child that inherits
	// our stderr pipe; when the deadline kills git itself, Wait would still
	// block until that grandchild closes the pipe. WaitDelay bounds that so an
	// unresponsive remote surfaces as "timed out" instead of a hang.
	cmd.WaitDelay = subprocessWaitDelay
	pinGitLocale(cmd)
	return cmd
}

// subprocessWaitDelay is how long exec waits for inherited pipes to close
// after a context-killed git exits.
const subprocessWaitDelay = 2 * time.Second

// pinGitLocale forces the C locale on a scratch git subprocess: this file
// classifies outcomes on git's English messages ("not our ref", "does not
// exist in"), and a translated message would turn a clean "not found" into
// a transport error (SPEC marketplace-check-outdated SC-B20). Applied after
// the gitops env helpers so it wins over an inherited LANG/LC_ALL.
func pinGitLocale(cmd *exec.Cmd) {
	cmd.Env = append(cmd.Env, "LC_ALL=C", "LANGUAGE=C")
}

// fetchRefIntoScratch fetches ref (a SHA, tag, branch, or full ref name)
// from source into a fresh bare scratch repository whose FETCH_HEAD then
// names the commit. cleanup is always safe to call. Errors: errRefNotOnRemote
// when the remote rejected the ref, a "timed out" error past
// listRefsTimeout, otherwise the sanitized git stderr.
func fetchRefIntoScratch(source, ref string) (dir string, cleanup func(), err error) {
	cleanup = func() {}
	cloneURL, err := resolveCloneURL(source)
	if err != nil {
		return "", cleanup, err
	}
	safeURL := gitops.SanitizeGitOutput(cloneURL)

	dir, err = os.MkdirTemp(scratchTempRoot, scratchTempPrefix)
	if err != nil {
		return "", cleanup, fmt.Errorf("create scratch repository: %w", err)
	}
	// Capture the path in its own variable: the error returns below reset
	// the named `dir` result to "" before any deferred cleanup runs, and a
	// closure over the named result would then remove nothing.
	scratchDir := dir
	cleanup = func() { removeScratch(scratchDir) }

	ctx, cancel := context.WithTimeout(context.Background(), listRefsTimeout)
	defer cancel()

	initCmd := exec.CommandContext(ctx, "git", "init", "-q", "--bare", "--", dir)
	gitops.ApplySecureGitEnv(initCmd)
	initCmd.WaitDelay = subprocessWaitDelay
	pinGitLocale(initCmd)
	if out, err := initCmd.CombinedOutput(); err != nil {
		if timedOut, terr := fetchTimedOut(ctx, safeURL); timedOut {
			return "", cleanup, terr
		}
		return "", cleanup, fmt.Errorf("git init scratch repository: %s", gitops.SanitizeGitOutput(gitFailureText(string(out), err)))
	}

	fetchCmd := newProbeFetchCmd(ctx, dir, cloneURL, ref)
	var stderr bytes.Buffer
	fetchCmd.Stderr = &stderr
	if err := fetchCmd.Run(); err != nil {
		if timedOut, terr := fetchTimedOut(ctx, safeURL); timedOut {
			return "", cleanup, terr
		}
		msg := gitFailureText(stderr.String(), err)
		if isRefNotOnRemote(msg) {
			return "", cleanup, errRefNotOnRemote
		}
		return "", cleanup, fmt.Errorf("git fetch %s: %s", safeURL, gitops.SanitizeGitOutput(msg))
	}
	return dir, cleanup, nil
}

// fetchTimedOut reports whether ctx hit listRefsTimeout, and the error a
// caller returns for it. Both the scratch init and the fetch itself run
// under the same deadline, so a fake git that sleeps on every subcommand
// trips it at init -- the message is the same either way.
func fetchTimedOut(ctx context.Context, safeURL string) (bool, error) {
	if ctx.Err() != context.DeadlineExceeded {
		return false, nil
	}
	return true, fmt.Errorf("git fetch %s: timed out after %s", safeURL, listRefsTimeout)
}

// gitFailureText is the text a failed git subprocess is reported with: its
// trimmed stderr, or the exec error when git printed nothing (a killed
// process, a missing binary).
func gitFailureText(stderr string, err error) string {
	if msg := strings.TrimSpace(stderr); msg != "" {
		return msg
	}
	return err.Error()
}

// removeScratch removes a scratch repository, retrying briefly: git marks
// object files read-only and a just-exited child can still hold the
// directory on Windows, so a single RemoveAll is not reliable there.
func removeScratch(dir string) {
	for attempt := 0; attempt < 10; attempt++ {
		_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				_ = os.Chmod(p, 0o600)
			}
			return nil
		})
		if err := os.RemoveAll(dir); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// isRefNotOnRemote recognizes upload-pack's rejections of a want that the
// remote does not have (or refuses to serve): "not our ref" for an object,
// "couldn't find remote ref" for a name.
func isRefNotOnRemote(stderr string) bool {
	return strings.Contains(stderr, "not our ref") || strings.Contains(stderr, "couldn't find remote ref")
}

// showAtFetchHead returns the bytes of relPath at FETCH_HEAD in the scratch
// repo dir, nil when the path does not exist at that commit, or an error
// for anything else (including a file over manifestReadMaxBytes).
func showAtFetchHead(dir, relPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), listRefsTimeout)
	defer cancel()
	cmd := newShowCmd(ctx, dir, relPath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", relPath, err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("git show %s: %w", relPath, err)
	}
	// The blob comes from the remote, so it is capped while it streams
	// rather than after git has written all of it into memory (SPEC
	// marketplace-check-outdated-fixes SC-F3).
	data, readErr := readCapped(stdout, manifestReadMaxBytes)
	if errors.Is(readErr, errReadCapExceeded) {
		// Nothing drains the pipe any more, so git would block on a full
		// pipe until the deadline; stop it before waiting.
		cancel()
		_ = cmd.Wait()
		return nil, fmt.Errorf("%s at the pinned ref exceeds %d bytes", relPath, manifestReadMaxBytes)
	}
	if err := cmd.Wait(); err != nil {
		msg := stderr.String()
		if strings.Contains(msg, "does not exist in") || strings.Contains(msg, "exists on disk, but not in") {
			return nil, nil
		}
		return nil, fmt.Errorf("git show %s: %s", relPath, gitops.SanitizeGitOutput(strings.TrimSpace(msg)))
	}
	if readErr != nil {
		return nil, fmt.Errorf("git show %s: %w", relPath, readErr)
	}
	return data, nil
}

// newShowCmd builds `git -C <dir> show FETCH_HEAD:<relPath>` under the
// secure environment, pinned to the C locale.
func newShowCmd(ctx context.Context, dir, relPath string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "git", "-C", dir, "show", "FETCH_HEAD:"+relPath)
	gitops.ApplySecureGitEnv(cmd)
	cmd.WaitDelay = subprocessWaitDelay
	pinGitLocale(cmd)
	return cmd
}

var errReadCapExceeded = errors.New("read cap exceeded")

// readCapped reads r to EOF but never more than max+1 bytes; one byte past
// max is enough to know the input is over the cap.
func readCapped(r io.Reader, max int) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, int64(max)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > max {
		return nil, errReadCapExceeded
	}
	return data, nil
}

// IsDisplayVersion mirrors the Oracle's _is_display_version (builder.py /
// output_mappers.py): a fixed, human-readable version rather than a semver
// range or pattern. Both pack (which only echoes a display version into
// marketplace.json) and check (which only compares a display version
// against the plugin manifest) share this definition.
func IsDisplayVersion(value string) bool {
	// A blank value is no version at all (SC-B21): the Oracle's
	// _is_display_version only short-circuits the empty string, so "  "
	// would otherwise read as a display version and trigger a manifest
	// comparison against nothing.
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	for _, prefix := range []string{"^", "~", ">", "<", "="} {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	if strings.Contains(trimmed, " ") || strings.Contains(trimmed, "*") {
		return false
	}
	segments := strings.Split(strings.ToLower(trimmed), ".")
	return segments[len(segments)-1] != "x"
}
