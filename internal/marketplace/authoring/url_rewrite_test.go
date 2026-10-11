package authoring

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

const rewriteProbeFailure = "Unable to verify Git URL rewrite safety (Git config probe failed); check Git configuration and retry; " +
	`inspect matching rules with 'git config --show-origin --get-regexp ^url\..*\.insteadOf$'`

// Each network git call site of this package stops at a failed rewrite
// check: the error is the check's own text with no prefix (`check` and
// `outdated` cut it to 60 characters), and git is never run, not even to
// create the scratch repository.
func TestNetworkGit_NotRunWhenRewriteCheckFails(t *testing.T) {
	record := filepath.Join(t.TempDir(), "git-invocations")
	t.Setenv("PATH", buildFakeGit(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_RECORD_FILE", record)
	t.Cleanup(gitops.SetConfigProbeForTest(gitops.FailingGitConfigProbe))
	const source = "https://example.invalid/owner/repo"
	commit := strings.Repeat("a", 40)

	calls := map[string]func() error{
		"ls-remote": func() error {
			_, err := DefaultRefLister.ListRefs(source)
			return err
		},
		"fetch for a commit probe": func() error {
			_, err := DefaultCommitProber.HasCommit(source, commit)
			return err
		},
		"fetch for a manifest version": func() error {
			_, err := DefaultManifestVersionFetcher.FetchManifestVersion(source, "v1.0.0", "")
			return err
		},
		"fetch for a subdir object id": func() error {
			_, _, err := DefaultSubdirObjectReader.SubdirObjectID(source, commit, "plugins/tool")
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()

			if err == nil || err.Error() != rewriteProbeFailure {
				t.Fatalf("error\n got %v\nwant %s", err, rewriteProbeFailure)
			}
			if invoked, readErr := os.ReadFile(record); readErr == nil {
				t.Fatalf("git was run: %s", invoked)
			}
		})
	}
}
