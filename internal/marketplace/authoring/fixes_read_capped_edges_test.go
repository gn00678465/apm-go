package authoring

import (
	"strings"
	"testing"
)

// SPEC marketplace-check-outdated-fixes §B, edge: the streaming `git show`
// reports a git that cannot be started instead of treating the path as
// missing.

func TestShowAtFetchHead_GitNotStartable_Errors(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	data, err := showAtFetchHead(t.TempDir(), "apm.yml")

	if err == nil || !strings.Contains(err.Error(), "git show apm.yml") || data != nil {
		t.Fatalf("showAtFetchHead = %q, %v; want a git show error and no data", data, err)
	}
}
