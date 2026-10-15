package lockfile

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #69: testdata/oracle/local-targets*.lock.yaml are the locks Oracle
// 0.33.0 wrote for a local skill package declared with `targets: [claude]`
// (deployed to .claude/) and `targets: [codex]` (no active target, nothing
// deployed), in a project whose targets are claude and copilot.

func readOracleLock(t *testing.T, name string) ([]byte, *Lockfile) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "oracle", name+".lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data, lf
}

func TestTargetSubset_OracleLockRoundTrips(t *testing.T) {
	for name, want := range map[string][]string{
		"local-targets":         {"claude"},
		"local-targets-skipped": {"codex"},
	} {
		t.Run(name, func(t *testing.T) {
			data, lf := readOracleLock(t, name)
			if got := lf.Dependencies[0].TargetSubset; !slices.Equal(got, want) {
				t.Errorf("TargetSubset = %q, want %q", got, want)
			}
			// Without the original node: target_subset comes from the model.
			got, err := WriteLockfile(lf, nil)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(data) {
				t.Errorf("lock differs from the Oracle's\n--- got\n%s\n--- want\n%s", got, data)
			}
		})
	}
}

func TestTargetSubset_ChangeAloneIsNotSemanticallyEqual(t *testing.T) {
	_, a := readOracleLock(t, "local-targets")
	_, b := readOracleLock(t, "local-targets")
	b.Dependencies[0].TargetSubset = []string{"copilot"}
	if IsSemanticEqual(a, b) {
		t.Error("IsSemanticEqual = true for locks that differ only in target_subset")
	}
	b.Dependencies[0].TargetSubset = nil
	if IsSemanticEqual(a, b) {
		t.Error("IsSemanticEqual = true after target_subset was dropped")
	}
}

func TestTargetSubset_DroppedFromModelLeavesTheLock(t *testing.T) {
	data, lf := readOracleLock(t, "local-targets")
	original, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf.Dependencies[0].TargetSubset = nil
	got, err := WriteLockfile(lf, original)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "target_subset") {
		t.Errorf("the old target_subset was carried over from the original lock:\n%s", got)
	}
}
