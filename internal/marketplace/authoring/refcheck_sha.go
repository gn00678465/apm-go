package authoring

import (
	"context"
	"errors"
	"os/exec"
)

// RED stubs for SPEC marketplace-check-outdated §B. Every function here
// compiles so the tests fail on behaviour; the GREEN commit replaces them.

// CommitProber answers "does this commit object exist on the remote?" for a
// 40-hex SHA that `git ls-remote` did not list (a commit that is not any
// ref's tip).
type CommitProber interface {
	HasCommit(source, sha string) (bool, error)
}

// ManifestVersionFetcher reads the version a plugin declares about itself at
// ref: `<subdir>/.claude-plugin/plugin.json` first, then `<subdir>/apm.yml`.
// "" with a nil error means neither file declares a version.
type ManifestVersionFetcher interface {
	FetchManifestVersion(source, ref, subdir string) (string, error)
}

// CheckDeps bundles `check`'s three remote seams.
type CheckDeps struct {
	Lister   RefLister
	Prober   CommitProber
	Manifest ManifestVersionFetcher
}

var DefaultCommitProber CommitProber = gitCommitProber{}
var DefaultManifestVersionFetcher ManifestVersionFetcher = gitManifestVersionFetcher{}

// scratchTempRoot is the parent directory for probe/fetch scratch repos;
// "" means os.TempDir(). A var so tests can prove cleanup.
var scratchTempRoot = ""

type gitCommitProber struct{}

func (gitCommitProber) HasCommit(source, sha string) (bool, error) {
	return false, errors.New("not implemented")
}

type gitManifestVersionFetcher struct{}

func (gitManifestVersionFetcher) FetchManifestVersion(source, ref, subdir string) (string, error) {
	return "", errors.New("not implemented")
}

func newProbeFetchCmd(ctx context.Context, dir, cloneURL, ref string) *exec.Cmd {
	return exec.CommandContext(ctx, "git")
}

// CheckPackagesWith is CheckPackages with every seam injected.
func CheckPackagesWith(dir string, cfg *AuthoringConfig, deps CheckDeps, offline bool) []CheckResult {
	return CheckPackages(dir, cfg, deps.Lister, offline)
}
