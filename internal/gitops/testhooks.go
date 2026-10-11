package gitops

import "context"

// SetConfigProbeForTest replaces the `git config` runner behind
// ValidateURLRewriteSafety and returns a restore func. Test-only: the
// production runner reads the git configuration of the machine the tests run
// on, which no test may depend on.
func SetConfigProbeForTest(probe ConfigProbeFunc) (restore func()) {
	prev := configProbe
	configProbe = probe
	return func() { configProbe = prev }
}

// EmptyGitConfigProbe is a ConfigProbeFunc that reports a git configuration
// with no entries, so every URL passes the rewrite check. Test-only.
func EmptyGitConfigProbe(context.Context, ConfigProbe) ([]byte, int, error) {
	return nil, 0, nil
}

// FailingGitConfigProbe is a ConfigProbeFunc whose git exits 1, so every URL
// fails the rewrite check with "Git config probe failed". Test-only.
func FailingGitConfigProbe(context.Context, ConfigProbe) ([]byte, int, error) {
	return nil, 1, nil
}
