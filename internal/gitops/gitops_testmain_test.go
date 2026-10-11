package gitops

import (
	"os"
	"testing"
)

// TestMain pins the URL rewrite check to an empty git configuration. The
// production probe reads the configuration of the machine the tests run on,
// so an insteadOf rule there would decide whether this package's clone and
// ls-remote tests pass. A test of the check itself installs its own probe
// with SetConfigProbeForTest.
func TestMain(m *testing.M) {
	SetConfigProbeForTest(EmptyGitConfigProbe)
	os.Exit(m.Run())
}
