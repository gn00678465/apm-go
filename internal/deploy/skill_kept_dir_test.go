package deploy

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// Issue #43: a symlink deploy must not delete a real skill directory that
// holds content the package does not have.

var keptDirSkillSource = map[string]string{
	"SKILL.md":       "---\nname: s1\ndescription: d\n---\nskill\n",
	"refs/detail.md": "detail\n",
}

func writeKeptDirFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for rel, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// keptDirFiles returns every non-directory entry under dir (slash-separated
// relative path -> content; a symlink maps to "-> " plus its target).
func keptDirFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			target, err := os.Readlink(p)
			if err != nil {
				return err
			}
			got[filepath.ToSlash(rel)] = "-> " + target
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func keptDirIsSymlink(t *testing.T, full string) bool {
	t.Helper()
	info, err := os.Lstat(full)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode()&os.ModeSymlink != 0
}

func TestSymlinkSkillTo_RealDirectory(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte(keptDirSkillSource["SKILL.md"]), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		dest map[string]string
		// prepare changes the destination or the source after dest is written.
		prepare      func(t *testing.T, src, dest string)
		wantMismatch string // "" = the directory is replaced by the symlink
	}{
		{
			name: "same content with a file in a subdirectory is replaced",
			dest: keptDirSkillSource,
		},
		{
			name: "subset of the source is replaced",
			dest: map[string]string{"SKILL.md": keptDirSkillSource["SKILL.md"]},
		},
		{
			name: "empty directory is replaced",
			dest: map[string]string{},
		},
		{
			name: "empty subdirectory the source does not have is replaced",
			dest: keptDirSkillSource,
			prepare: func(t *testing.T, _, dest string) {
				if err := os.MkdirAll(filepath.Join(dest, "empty", "nested"), 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "different file mode is replaced",
			dest: keptDirSkillSource,
			prepare: func(t *testing.T, _, dest string) {
				if err := os.Chmod(filepath.Join(dest, "SKILL.md"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "extra file is kept",
			dest: map[string]string{
				"SKILL.md":       keptDirSkillSource["SKILL.md"],
				"refs/detail.md": keptDirSkillSource["refs/detail.md"],
				"user-notes.txt": "mine\n",
			},
			wantMismatch: "user-notes.txt",
		},
		{
			name: "file with different content of the same size is kept",
			dest: map[string]string{
				"SKILL.md":       keptDirSkillSource["SKILL.md"],
				"refs/detail.md": "DETAIL\n",
			},
			wantMismatch: "refs/detail.md",
		},
		{
			name: "file with a different size is kept",
			dest: map[string]string{
				"SKILL.md": keptDirSkillSource["SKILL.md"] + "my edit\n",
			},
			wantMismatch: "SKILL.md",
		},
		{
			name: "first mismatch in lexical order is reported",
			dest: map[string]string{
				"SKILL.md":      keptDirSkillSource["SKILL.md"],
				"zz.txt":        "mine\n",
				"refs/mine.txt": "mine\n",
				"aa/mine.txt":   "mine\n",
			},
			wantMismatch: "aa/mine.txt",
		},
		{
			name: "symlink entry is kept even when its target has the source content",
			dest: map[string]string{"refs/detail.md": keptDirSkillSource["refs/detail.md"]},
			prepare: func(t *testing.T, _, dest string) {
				if err := os.Symlink(outside, filepath.Join(dest, "SKILL.md")); err != nil {
					t.Skipf("cannot create a symlink: %v", err)
				}
			},
			wantMismatch: "SKILL.md",
		},
		{
			name:         "file whose source path is a directory is kept",
			dest:         map[string]string{"refs": "detail\n"},
			wantMismatch: "refs",
		},
		{
			name: "file whose source path is a symlink is kept",
			dest: map[string]string{"link.md": keptDirSkillSource["SKILL.md"]},
			prepare: func(t *testing.T, src, _ string) {
				if err := os.Symlink(filepath.Join(src, "SKILL.md"), filepath.Join(src, "link.md")); err != nil {
					t.Skipf("cannot create a symlink: %v", err)
				}
			},
			wantMismatch: "link.md",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "s1")
			writeKeptDirFiles(t, src, keptDirSkillSource)
			deployRoot := t.TempDir()
			dest := filepath.Join(deployRoot, ".claude", "skills", "s1")
			if err := os.MkdirAll(dest, 0o755); err != nil {
				t.Fatal(err)
			}
			writeKeptDirFiles(t, dest, tc.dest)
			if tc.prepare != nil {
				tc.prepare(t, src, dest)
			}
			before := keptDirFiles(t, dest)

			files, err := symlinkSkillTo(src, dest, ".claude/skills/s1")

			if tc.wantMismatch == "" {
				if err != nil {
					t.Fatalf("symlinkSkillTo: %v", err)
				}
				if !keptDirIsSymlink(t, dest) {
					t.Fatal("destination is not a symlink")
				}
				sort.Strings(files)
				want := []string{".claude/skills/s1/SKILL.md", ".claude/skills/s1/refs/detail.md"}
				if !reflect.DeepEqual(files, want) {
					t.Errorf("files = %v, want %v", files, want)
				}
				return
			}

			var kept *keptSkillDirError
			if !errors.As(err, &kept) {
				t.Fatalf("err = %v, want a keptSkillDirError", err)
			}
			if kept.dir != ".claude/skills/s1" || kept.mismatch != tc.wantMismatch {
				t.Errorf("kept = {dir: %q, mismatch: %q}, want {dir: %q, mismatch: %q}",
					kept.dir, kept.mismatch, ".claude/skills/s1", tc.wantMismatch)
			}
			if len(files) != 0 {
				t.Errorf("files = %v, want none", files)
			}
			if keptDirIsSymlink(t, dest) {
				t.Fatal("destination was replaced by a symlink")
			}
			if after := keptDirFiles(t, dest); !reflect.DeepEqual(after, before) {
				t.Errorf("directory content changed:\n got %v\nwant %v", after, before)
			}
		})
	}
}

func TestSymlinkSkillTo_ReplacesSymlinkAndRegularFile(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dest string)
	}{
		{"symlink the user pointed at another directory", func(t *testing.T, dest string) {
			other := t.TempDir()
			writeKeptDirFiles(t, other, map[string]string{"mine.txt": "mine\n"})
			if err := os.Symlink(other, dest); err != nil {
				t.Skipf("cannot create a symlink: %v", err)
			}
		}},
		{"regular file", func(t *testing.T, dest string) {
			if err := os.WriteFile(dest, []byte("MY OWN NOTES\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			src := filepath.Join(t.TempDir(), "s1")
			writeKeptDirFiles(t, src, keptDirSkillSource)
			dest := filepath.Join(t.TempDir(), ".claude", "skills", "s1")
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				t.Fatal(err)
			}
			tc.prepare(t, dest)

			if _, err := symlinkSkillTo(src, dest, ".claude/skills/s1"); err != nil {
				t.Fatalf("symlinkSkillTo: %v", err)
			}

			target, err := os.Readlink(dest)
			if err != nil {
				t.Fatalf("destination is not a symlink: %v", err)
			}
			if target != src {
				t.Errorf("symlink target = %q, want %q", target, src)
			}
		})
	}
}

// keptDirProject writes a project with one local skill s1 and a real
// directory with a user file at the skill's destination for each root.
func keptDirProject(t *testing.T, roots ...string) (projectDir, deployDir string) {
	t.Helper()
	projectDir, deployDir = t.TempDir(), t.TempDir()
	writeKeptDirFiles(t, filepath.Join(projectDir, ".apm", "skills", "s1"), keptDirSkillSource)
	for _, root := range roots {
		writeKeptDirFiles(t, filepath.Join(deployDir, filepath.FromSlash(root), "s1"), map[string]string{
			"SKILL.md":       keptDirSkillSource["SKILL.md"],
			"user-notes.txt": "mine\n",
		})
	}
	return projectDir, deployDir
}

func TestRun_SymlinkKeepsRealSkillDirectory(t *testing.T) {
	projectDir, deployDir := keptDirProject(t, ".claude/skills")

	result, err := Run([]string{"claude"}, projectDir, &manifest.Manifest{}, nil, nil, deployDir, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if want := []string{".claude/skills/s1"}; !reflect.DeepEqual(result.KeptDirs, want) {
		t.Errorf("KeptDirs = %v, want %v", result.KeptDirs, want)
	}
	wantDiags := []string{`keeping ".claude/skills/s1": the directory holds "user-notes.txt", which this package does not have with the same content; skill "s1" not deployed`}
	if !reflect.DeepEqual(result.Diags, wantDiags) {
		t.Errorf("Diags = %q, want %q", result.Diags, wantDiags)
	}
	if len(result.FailedBuckets) != 0 {
		t.Errorf("FailedBuckets = %v, want empty", result.FailedBuckets)
	}
	if len(result.PerDep) != 0 {
		t.Errorf("PerDep = %v, want empty", result.PerDep)
	}
	dest := filepath.Join(deployDir, ".claude", "skills", "s1")
	want := map[string]string{"SKILL.md": keptDirSkillSource["SKILL.md"], "user-notes.txt": "mine\n"}
	if got := keptDirFiles(t, dest); keptDirIsSymlink(t, dest) || !reflect.DeepEqual(got, want) {
		t.Errorf("kept directory = %v, want the real directory with %v", got, want)
	}
}

func TestRun_SymlinkKeepsSharedSkillDirectoryOnce(t *testing.T) {
	projectDir, deployDir := keptDirProject(t, ".agents/skills")

	result, err := Run([]string{"codex", "copilot", "opencode"}, projectDir, &manifest.Manifest{}, nil, nil, deployDir, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if want := []string{".agents/skills/s1"}; !reflect.DeepEqual(result.KeptDirs, want) {
		t.Errorf("KeptDirs = %v, want %v", result.KeptDirs, want)
	}
	wantDiags := []string{`keeping ".agents/skills/s1": the directory holds "user-notes.txt", which this package does not have with the same content; skill "s1" not deployed`}
	if !reflect.DeepEqual(result.Diags, wantDiags) {
		t.Errorf("Diags = %q, want %q", result.Diags, wantDiags)
	}
	if len(result.FailedBuckets) != 0 {
		t.Errorf("FailedBuckets = %v, want empty", result.FailedBuckets)
	}
}

func TestRun_SymlinkKeepsEachTargetNativeDirectory(t *testing.T) {
	projectDir, deployDir := keptDirProject(t, ".claude/skills", ".agents/skills")

	result, err := Run([]string{"claude", "codex"}, projectDir, &manifest.Manifest{}, nil, nil, deployDir, true)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if want := []string{".claude/skills/s1", ".agents/skills/s1"}; !reflect.DeepEqual(result.KeptDirs, want) {
		t.Errorf("KeptDirs = %v, want %v", result.KeptDirs, want)
	}
	if len(result.Diags) != 2 {
		t.Errorf("Diags = %q, want one line per kept directory", result.Diags)
	}
}

func TestUnderKeptDir(t *testing.T) {
	kept := []string{".claude/skills/s1"}
	tests := []struct {
		path string
		want bool
	}{
		{".claude/skills/s1", true},
		{".claude/skills/s1/SKILL.md", true},
		{".claude/skills/s1/refs/detail.md", true},
		{`.claude\skills\s1\SKILL.md`, true},
		{"./.claude/skills/s1/SKILL.md", true},
		{".claude/skills/s10/SKILL.md", false},
		{".claude/skills/s2/SKILL.md", false},
		{".claude/skills", false},
		{".agents/skills/s1/SKILL.md", false},
	}
	for _, tc := range tests {
		if got := UnderKeptDir(kept, tc.path); got != tc.want {
			t.Errorf("UnderKeptDir(%v, %q) = %v, want %v", kept, tc.path, got, tc.want)
		}
	}
	if UnderKeptDir(nil, ".claude/skills/s1/SKILL.md") {
		t.Error("UnderKeptDir(nil, …) = true, want false")
	}
}
