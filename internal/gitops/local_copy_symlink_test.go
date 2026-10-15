package gitops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const agentBody = "---\nname: oracle\ndescription: d\n---\nagent body\n"

// symlinkDep is localCopyRoot with dep/agents/oracle.agent.md; it skips the
// test where symlinks cannot be made.
func symlinkDep(t *testing.T) (root, dep string) {
	t.Helper()
	root = localCopyRoot(t)
	dep = filepath.Join(root, "dep")
	if err := os.MkdirAll(filepath.Join(dep, "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dep, "agents", "oracle.agent.md"), []byte(agentBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("x", filepath.Join(root, "probe")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	return root, dep
}

// A symlink in a local package that resolves inside the package is copied as
// the file or directory it points at, as Oracle 0.33.0 copies it
// (install/phases/local_content.py:91-181).
func TestMaterializeLocalCopy_DereferencesInPackageSymlinks(t *testing.T) {
	tests := []struct {
		name  string
		links map[string]string // path under dep -> link target
		want  string            // path under the copy that must be a regular file with agentBody
	}{
		{
			name:  "directory",
			links: map[string]string{".apm/agents": "../agents"},
			want:  ".apm/agents/oracle.agent.md",
		},
		{
			name:  "file",
			links: map[string]string{".apm/agents/oracle.agent.md": "../../agents/oracle.agent.md"},
			want:  ".apm/agents/oracle.agent.md",
		},
		{
			name: "chain",
			links: map[string]string{
				".apm/agents/oracle.agent.md": "../../links/hop",
				"links/hop":                   "../agents/oracle.agent.md",
			},
			want: ".apm/agents/oracle.agent.md",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, dep := symlinkDep(t)
			for link, target := range tt.links {
				symlink(t, target, filepath.Join(dep, filepath.FromSlash(link)))
			}

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			if _, err := r.LoadPackage(localRef("../dep", dep), ""); err != nil {
				t.Fatal(err)
			}
			copied := filepath.Join("apm_modules", "_local", "dep", filepath.FromSlash(tt.want))
			for p := copied; p != filepath.Join("apm_modules", "_local", "dep"); p = filepath.Dir(p) {
				if info, err := os.Lstat(p); err != nil || info.Mode()&os.ModeSymlink != 0 {
					t.Fatalf("%s in the copy: lstat %v, error %v; want a real file or directory", p, info, err)
				}
			}
			if got, err := os.ReadFile(copied); err != nil || string(got) != agentBody {
				t.Errorf("%s = %q, error %v; want %q", copied, got, err, agentBody)
			}
		})
	}
}

// A dangling symlink, one that resolves outside the package, or one that
// closes a directory cycle aborts the copy with the Oracle's reason
// (install/phases/local_content.py:142-169), and no copy is left in place of
// the stale one (:286-289).
func TestMaterializeLocalCopy_RefusesSymlinkItCannotDereference(t *testing.T) {
	const (
		secret       = "must not be copied"
		escapeTail   = "Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted."
		danglingTail = ". Remove the dangling symlink or restore its target. Local install aborted."
	)
	tests := []struct {
		name string
		link string // path under dep
		// target is the link target; wantPrefix and wantSuffix bound the
		// reason after "copy local dependency <dep>: ".
		target                 func(root string) string
		wantPrefix, wantSuffix func(root string) string
	}{
		{
			name:   "outside, absolute",
			link:   ".apm/agents/outer.agent.md",
			target: func(root string) string { return filepath.Join(root, "outside", "outer.agent.md") },
			wantPrefix: func(root string) string {
				return "Symlink '" + filepath.Join(root, "dep", ".apm", "agents", "outer.agent.md") + "' resolves to '" +
					filepath.Join(root, "outside", "outer.agent.md") + "', which escapes the package root '" +
					filepath.Join(root, "dep") + "'. " + escapeTail
			},
		},
		{
			name:   "outside, relative",
			link:   ".apm/up",
			target: func(string) string { return "../../.." },
			wantPrefix: func(root string) string {
				return "Symlink '" + filepath.Join(root, "dep", ".apm", "up") + "' resolves to '" +
					filepath.Dir(root) + "', which escapes the package root '" +
					filepath.Join(root, "dep") + "'. " + escapeTail
			},
		},
		{
			name:   "dangling",
			link:   ".apm/agents/x.agent.md",
			target: func(string) string { return "../../nope.agent.md" },
			wantPrefix: func(root string) string {
				return "Symlink '" + filepath.Join(root, "dep", ".apm", "agents", "x.agent.md") + "' has a broken or unresolvable target: "
			},
			wantSuffix: func(string) string { return danglingTail },
		},
		{
			name:   "directory cycle",
			link:   "self",
			target: func(string) string { return "." },
			wantPrefix: func(root string) string {
				return "Circular symlink detected: '" + filepath.Join(root, "dep", "self") + "' resolves to '" +
					filepath.Join(root, "dep") + "', which was already visited during this install. " +
					"Break the cycle by replacing one symlink with a real directory copy. Local install aborted."
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, dep := symlinkDep(t)
			if err := os.MkdirAll(filepath.Join(root, "outside"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "outside", "outer.agent.md"), []byte(secret), 0o644); err != nil {
				t.Fatal(err)
			}
			symlink(t, tt.target(root), filepath.Join(dep, filepath.FromSlash(tt.link)))
			stale := filepath.Join("apm_modules", "_local", "dep")
			writePkg(t, stale)

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			_, err := r.LoadPackage(localRef("../dep", dep), "")
			if err == nil {
				t.Fatal("LoadPackage succeeded, want the copy refused")
			}
			wrap := "copy local dependency " + dep + ": "
			msg := err.Error()
			if want := wrap + tt.wantPrefix(root); !strings.HasPrefix(msg, want) {
				t.Errorf("error\n%s\nwant prefix\n%s", msg, want)
			}
			if tt.wantSuffix == nil && msg != wrap+tt.wantPrefix(root) {
				t.Errorf("error\n%s\nwant\n%s", msg, wrap+tt.wantPrefix(root))
			}
			if tt.wantSuffix != nil && !strings.HasSuffix(msg, tt.wantSuffix(root)) {
				t.Errorf("error\n%s\nwant suffix\n%s", msg, tt.wantSuffix(root))
			}
			if _, err := os.Lstat(stale); !os.IsNotExist(err) {
				t.Errorf("%s exists after a refused copy (lstat error %v)", stale, err)
			}
			if info, err := os.Stat(filepath.Dir(stale)); err != nil || !info.IsDir() {
				t.Errorf("%s is not left as a directory (error %v)", filepath.Dir(stale), err)
			}
		})
	}
}

// A refused copy into a project with no apm_modules yet removes the empty
// directories the copy created for it, as Oracle 0.33.0 does
// (install/resolution_staging.py:195-200).
func TestMaterializeLocalCopy_RefusedFirstCopyLeavesNoModulesDir(t *testing.T) {
	_, dep := symlinkDep(t)
	symlink(t, "../nope", filepath.Join(dep, "dangling"))

	r := &RealPackageLoader{ModulesDir: "apm_modules"}
	if _, err := r.LoadPackage(localRef("../dep", dep), ""); err == nil {
		t.Fatal("LoadPackage succeeded, want the copy refused")
	}
	if _, err := os.Lstat("apm_modules"); !os.IsNotExist(err) {
		t.Errorf("apm_modules exists after a refused first copy (lstat error %v)", err)
	}
}
