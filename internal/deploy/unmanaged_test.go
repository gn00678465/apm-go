package deploy

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
)

// Issue #144: a project deploy leaves what the user put where a primitive
// deploys and the lock before the run does not record (Managed), as the
// Oracle's _check_adopt_or_skip does (integration/base_integrator.py:
// 198-241,429-495).

var unmanagedSource = map[string]string{
	".apm/agents/ag.agent.md":   "---\nname: ag\ndescription: d\n---\nbody\n",
	".apm/agents/same.agent.md": "---\nname: same\ndescription: d\n---\nsame\r\n",
	".apm/skills/sk/SKILL.md":   "---\nname: sk\ndescription: d\n---\nskill\n",
	".apm/hooks/h.json":         "{}\n",
}

// unmanagedRun deploys unmanagedSource to claude and copilot into deployDir,
// judging what is there below root, which holds userFiles.
func unmanagedRun(t *testing.T, deployDir string, userFiles map[string]string, managed map[string]bool) (*DeployResult, string) {
	t.Helper()
	root := t.TempDir()
	writeKeptDirFiles(t, root, unmanagedSource)
	writeKeptDirFiles(t, root, userFiles)
	if deployDir == "" {
		deployDir = root
	}
	result, err := Run([]string{"claude", "copilot"}, root, &manifest.Manifest{}, nil, nil, deployDir, false, &Managed{Paths: managed, Root: root})
	if err != nil {
		t.Fatal(err)
	}
	return result, root
}

func TestRun_UnmanagedPaths(t *testing.T) {
	user := map[string]string{
		".claude/agents/ag.md":       "USER\n",
		".claude/skills/sk/SKILL.md": "USER\n",
		// What the Oracle compares an agent with: the source, CRLF as LF.
		".github/agents/same.agent.md": "---\nname: same\ndescription: d\n---\nsame\n",
		// A hook the Oracle never adopts (hook_integrator.py:1070).
		".github/hooks/local-h.json": "{}\n",
	}
	for _, trial := range []bool{false, true} {
		deployDir := ""
		if trial {
			deployDir = t.TempDir()
		}
		result, root := unmanagedRun(t, deployDir, user, nil)
		if deployDir == "" {
			deployDir = root
		}
		left := result.Unmanaged[""]
		sort.Strings(left)
		if want := []string{".claude/agents/ag.md", ".claude/skills/sk", ".github/hooks/local-h.json"}; !reflect.DeepEqual(left, want) {
			t.Errorf("trial %v: Unmanaged = %q, want %q", trial, left, want)
		}
		for rel, content := range user {
			if got, err := os.ReadFile(filepath.Join(root, rel)); err != nil || string(got) != content {
				t.Errorf("trial %v: %s = %q, %v; want the user's", trial, rel, got, err)
			}
		}
		local := result.PerDep[""]
		for _, rel := range []string{".claude/agents/ag.md", ".claude/skills/sk/SKILL.md", ".github/hooks/local-h.json"} {
			if _, ok := local.Hashes[rel]; ok {
				t.Errorf("trial %v: %s is recorded", trial, rel)
			}
		}
		adopted := ".github/agents/same.agent.md"
		want, err := lockfile.HashDeployedFile(filepath.Join(root, adopted))
		if err != nil {
			t.Fatal(err)
		}
		if local.Hashes[adopted] != want || !result.Adopted[adopted] {
			t.Errorf("trial %v: %s hash %q adopted %v, want %q adopted", trial, adopted, local.Hashes[adopted], result.Adopted[adopted], want)
		}
		if trial {
			for _, rel := range []string{".claude/agents/ag.md", ".claude/skills/sk", adopted} {
				if _, err := os.Lstat(filepath.Join(deployDir, rel)); !os.IsNotExist(err) {
					t.Errorf("trial: %s written in the trial directory: %v", rel, err)
				}
			}
			if _, err := os.Stat(filepath.Join(deployDir, ".claude/agents/same.md")); err != nil {
				t.Errorf("trial: a path that is free is not written: %v", err)
			}
		}
	}
}

// A path the lock records, also a skill directory with only a file below it
// recorded, is written over; so is everything with no Managed.
func TestRun_ManagedPathsWritten(t *testing.T) {
	user := map[string]string{".claude/agents/ag.md": "USER\n", ".claude/skills/sk/SKILL.md": "USER\n"}
	result, root := unmanagedRun(t, "", user, map[string]bool{".claude/agents/ag.md": true, ".claude/skills/sk/SKILL.md": true})
	if len(result.Unmanaged) != 0 {
		t.Errorf("recorded: Unmanaged = %v", result.Unmanaged)
	}
	for rel := range user {
		if got, _ := os.ReadFile(filepath.Join(root, rel)); string(got) == "USER\n" {
			t.Errorf("recorded: %s not written over", rel)
		}
	}
	root = t.TempDir()
	writeKeptDirFiles(t, root, unmanagedSource)
	writeKeptDirFiles(t, root, user)
	result, err := Run([]string{"claude"}, root, &manifest.Manifest{}, nil, nil, "", false, nil)
	if err != nil || len(result.Unmanaged) != 0 {
		t.Fatalf("no Managed: %v, Unmanaged %v", err, result.Unmanaged)
	}
	if got, _ := os.ReadFile(filepath.Join(root, ".claude/agents/ag.md")); string(got) == "USER\n" {
		t.Error("no Managed: the agent is not written over")
	}
}

// A symlink there is never what the Oracle compares (base_integrator.py:
// 342-343), also one to a file with the deployed bytes.
func TestRun_UnmanagedSymlinkNotAdopted(t *testing.T) {
	root := t.TempDir()
	writeKeptDirFiles(t, root, unmanagedSource)
	writeKeptDirFiles(t, root, map[string]string{".claude/agents/mine.md": unmanagedSource[".apm/agents/ag.agent.md"]})
	if err := os.Symlink("mine.md", filepath.Join(root, ".claude/agents/ag.md")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("this account cannot make a symlink: %v", err)
		}
		t.Fatal(err)
	}
	result, err := Run([]string{"claude"}, root, &manifest.Manifest{}, nil, nil, "", false, &Managed{Root: root})
	if err != nil {
		t.Fatal(err)
	}
	if got := result.Unmanaged[""]; !reflect.DeepEqual(got, []string{".claude/agents/ag.md"}) {
		t.Errorf("Unmanaged = %q", got)
	}
}
