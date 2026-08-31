package main

import (
	"os"
	"testing"
)

// ciDetectionEnv is every environment variable apm-go inspects to decide it
// is running under CI: "CI" itself (lockfile.IsCIEnvironment, which makes
// `install` default to frozen -- req-lk-018 -- and ux.isCI, which suppresses
// interactivity), plus ux.isCI's vendor-specific fallbacks.
var ciDetectionEnv = []string{
	"CI",
	"GITHUB_ACTIONS",
	"GITLAB_CI",
	"BUILDKITE",
	"TF_BUILD",
	"JENKINS_URL",
}

// TestMain neutralises CI detection for this package's tests.
//
// These variables are an ambient input this package's tests never opt into:
// on a developer workstation they are unset, so `go test ./...` is green,
// while GitHub Actions sets CI=true and GITHUB_ACTIONS=true and ~88 install
// assertions fail with "frozen install requires a lockfile but none was
// found" -- `install` correctly defaults to frozen under CI (the Oracle
// behaviour behind the "[i] CI environment detected, defaulting to frozen
// install" line), and the tests were written against the non-frozen default.
// The product behaviour is right; reading the environment rather than
// pinning it is what is wrong, and it made the go-test job red for every
// commit while hiding whether any given commit had actually regressed
// anything.
//
// Clearing them here (rather than adding t.Setenv to each affected test)
// puts the whole package on one deterministic baseline, and keeps a future
// test from silently inheriting the same dependency. A test that wants CI
// semantics still opts in explicitly with t.Setenv("CI", "1") -- doctor_test
// and update_test already do, and t.Setenv overrides this per-test and
// restores afterwards.
func TestMain(m *testing.M) {
	for _, key := range ciDetectionEnv {
		os.Unsetenv(key)
	}
	os.Exit(m.Run())
}
