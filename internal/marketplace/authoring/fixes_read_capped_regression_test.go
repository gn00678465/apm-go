package authoring

import (
	"os"
	"strings"
	"testing"
	"time"
)

// SPEC marketplace-check-outdated-fixes §B, regression: an oversized
// manifest stops before the deadline and leaves no scratch dir (SC-F4; it
// already passed before the streaming cap, see SPEC Revisions), and a
// manifest of exactly the cap is still read (SC-F5).

func TestFetchManifestVersion_ManifestAtLimit_Reads(t *testing.T) {
	head := `{"version":"1.0.0","pad":"`
	tail := `"}`
	manifest := head + strings.Repeat("x", manifestReadMaxBytes-len(head)-len(tail)) + tail
	if len(manifest) != manifestReadMaxBytes {
		t.Fatalf("fixture is %d bytes, want %d", len(manifest), manifestReadMaxBytes)
	}
	dir, sha := manifestRepo(t, manifest, "")

	got, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "")

	if err != nil || got != "1.0.0" {
		t.Fatalf("FetchManifestVersion = %q, %v; want 1.0.0 and no error", got, err)
	}
}

func TestShowAtFetchHead_Oversized_StopsBeforeTimeout(t *testing.T) {
	orig := listRefsTimeout
	listRefsTimeout = 3 * time.Second
	t.Cleanup(func() { listRefsTimeout = orig })

	scratch := t.TempDir()
	origScratch := scratchTempRoot
	scratchTempRoot = scratch
	t.Cleanup(func() { scratchTempRoot = origScratch })

	big := `{"version":"1.0.0","pad":"` + strings.Repeat("x", 8*manifestReadMaxBytes) + `"}`
	check := func(t *testing.T, dir, sha string) {
		t.Helper()
		start := time.Now()
		_, err := gitManifestVersionFetcher{}.FetchManifestVersion(dir, sha, "")
		elapsed := time.Since(start)
		if err == nil || !strings.Contains(err.Error(), "exceeds") || strings.Contains(err.Error(), "timed out") {
			t.Errorf("err = %v, want the size cap error, not a timeout", err)
		}
		if elapsed >= listRefsTimeout {
			t.Errorf("took %s, want the read to stop before the %s deadline", elapsed, listRefsTimeout)
		}
		entries, err := os.ReadDir(scratch)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Errorf("scratch dirs left behind: %d", len(entries))
		}
	}

	dir, sha := manifestRepo(t, big, "")
	check(t, dir, sha)

	t.Run("ApmYML", func(t *testing.T) {
		dir, sha := manifestRepo(t, "", "name: tool\nversion: 1.0.0\npad: "+strings.Repeat("x", 8*manifestReadMaxBytes)+"\n")
		check(t, dir, sha)
	})
}
