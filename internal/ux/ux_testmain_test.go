package ux

import (
	"os"
	"testing"
)

// TestMain neutralises CI detection for this package's tests, for the same
// reason cmd/apm-go's TestMain does -- see its comment for the full
// rationale.
//
// The specific gap here is the vendor-specific half of isCI: a number of
// this package's tests already pin t.Setenv("CI", ""), which is enough on a
// developer workstation but not under GitHub Actions, where GITHUB_ACTIONS
// is also set and isCI still reports true. Interactive prompting is then
// suppressed and the prompt/transcript tests see no error propagate.
//
// isCI's own coverage is unaffected: TestIsCI's vendor-variable table sets
// each key explicitly with t.Setenv, which overrides this baseline per-test
// and restores afterwards.
func TestMain(m *testing.M) {
	for _, key := range []string{
		"CI",
		"GITHUB_ACTIONS",
		"GITLAB_CI",
		"BUILDKITE",
		"TF_BUILD",
		"JENKINS_URL",
	} {
		os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
