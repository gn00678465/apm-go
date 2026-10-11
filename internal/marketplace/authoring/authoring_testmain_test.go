package authoring

import (
	"os"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
)

// TestMain pins gitops.ValidateURLRewriteSafety to an empty git
// configuration: its production probe reads the configuration of the machine
// the tests run on. A test of the check's effect installs its own probe.
func TestMain(m *testing.M) {
	gitops.SetConfigProbeForTest(gitops.EmptyGitConfigProbe)
	os.Exit(m.Run())
}
