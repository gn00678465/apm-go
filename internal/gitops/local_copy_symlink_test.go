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

// Every failure of the copy of a local package carries the reason Oracle
// 0.33.0 prints for it under Python 3.12 (install/phases/local_content.py:91-181
// and the OSError or RuntimeError it lets through), and no copy is left in
// place of the stale one (:286-289). The expected reasons are Oracle output
// with the test root replaced by <ROOT>, and its parent by <PARENT>.
func TestMaterializeLocalCopy_RefusesSymlinkItCannotDereference(t *testing.T) {
	const dangling = ". Remove the dangling symlink or restore its target. Local install aborted."
	link := func(path, target string) func(*testing.T, string) {
		return func(t *testing.T, dep string) {
			symlink(t, target, filepath.Join(dep, filepath.FromSlash(path)))
		}
	}
	unreadable := func(path string, dir bool) func(*testing.T, string) {
		return func(t *testing.T, dep string) {
			if os.Geteuid() == 0 {
				t.Skip("root reads a mode 000 file")
			}
			p := filepath.Join(dep, filepath.FromSlash(path))
			if dir {
				if err := os.MkdirAll(filepath.Join(p, "sub"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(p, []byte("x\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Chmod(p, 0o755) })
		}
	}
	tests := []struct {
		name  string
		setup func(t *testing.T, dep string)
		want  string
	}{
		{
			name:  "outside, absolute",
			setup: link(".apm/agents/outer.agent.md", "../../../outside/outer.agent.md"),
			want:  "Symlink '<ROOT>/dep/.apm/agents/outer.agent.md' resolves to '<ROOT>/outside/outer.agent.md', which escapes the package root '<ROOT>/dep'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted.",
		},
		{
			name:  "outside, relative",
			setup: link(".apm/up", "../../.."),
			want:  "Symlink '<ROOT>/dep/.apm/up' resolves to '<PARENT>', which escapes the package root '<ROOT>/dep'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted.",
		},
		{
			name:  "dangling",
			setup: link(".apm/agents/x.agent.md", "missing"),
			want:  "Symlink '<ROOT>/dep/.apm/agents/x.agent.md' has a broken or unresolvable target: [Errno 2] No such file or directory: '<ROOT>/dep/.apm/agents/missing'" + dangling,
		},
		{
			name: "dangling at the end of a chain",
			setup: func(t *testing.T, dep string) {
				link(".apm/agents/a.agent.md", "b.agent.md")(t, dep)
				link(".apm/agents/b.agent.md", "missing")(t, dep)
			},
			want: "Symlink '<ROOT>/dep/.apm/agents/a.agent.md' has a broken or unresolvable target: [Errno 2] No such file or directory: '<ROOT>/dep/.apm/agents/missing'" + dangling,
		},
		{
			name:  "a file in the middle of the target",
			setup: link(".apm/agents/x.agent.md", "../../apm.yml/sub"),
			want:  "Symlink '<ROOT>/dep/.apm/agents/x.agent.md' has a broken or unresolvable target: [Errno 20] Not a directory: '<ROOT>/dep/apm.yml/sub'" + dangling,
		},
		{
			name:  "link to itself",
			setup: link(".apm/agents/x.agent.md", "x.agent.md"),
			want:  "Symlink loop from '<ROOT>/dep/.apm/agents/x.agent.md'",
		},
		{
			name: "two links to each other",
			setup: func(t *testing.T, dep string) {
				link(".apm/agents/a.agent.md", "b.agent.md")(t, dep)
				link(".apm/agents/b.agent.md", "a.agent.md")(t, dep)
			},
			want: "Symlink loop from '<ROOT>/dep/.apm/agents/a.agent.md'",
		},
		{
			name:  "directory cycle",
			setup: link("self", "."),
			want:  "Circular symlink detected: '<ROOT>/dep/self' resolves to '<ROOT>/dep', which was already visited during this install. Break the cycle by replacing one symlink with a real directory copy. Local install aborted.",
		},
		{
			name:  "unreadable directory",
			setup: unreadable(".apm/agents/sub", true),
			want:  "Cannot read package directory '<ROOT>/dep/.apm/agents/sub': [Errno 13] Permission denied: '<ROOT>/dep/.apm/agents/sub'. Check that the package directory has read permissions. Local install aborted.",
		},
		{
			name:  "unreadable file",
			setup: unreadable(".apm/agents/x.agent.md", false),
			want:  "[Errno 13] Permission denied: '<ROOT>/dep/.apm/agents/x.agent.md'",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, dep := symlinkDep(t)
			if err := os.MkdirAll(filepath.Join(root, "outside"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(root, "outside", "outer.agent.md"), []byte("must not be copied"), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(dep, ".apm", "agents"), 0o755); err != nil {
				t.Fatal(err)
			}
			tt.setup(t, dep)
			stale := filepath.Join("apm_modules", "_local", "dep")
			writePkg(t, stale)

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			_, err := r.LoadPackage(localRef("../dep", dep), "")
			want := "copy local dependency " + dep + ": " +
				strings.NewReplacer("<ROOT>", root, "<PARENT>", filepath.Dir(root)).Replace(tt.want)
			if err == nil || err.Error() != want {
				t.Errorf("error\n%v\nwant\n%s", err, want)
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

// pyRepr is Python 3.12's repr() of a str; the expected values are what
// repr() printed for each input.
func TestPyRepr(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain/path", `'plain/path'`},
		{"it's", `"it's"`},
		{`say "hi"`, `'say "hi"'`},
		{`both ' and "`, `'both \' and "'`},
		{`back\slash`, `'back\\slash'`},
		{"new\nline\ttab\r", `'new\nline\ttab\r'`},
		{"\x7f\x01", `'\x7f\x01'`},
		{"caf\u00e9", "'caf\u00e9'"},
		{"zw\u200bsp", `'zw\u200bsp'`},
		{"\U0001f600", "'\U0001f600'"},
		{"bad\xffbyte", `'bad\udcffbyte'`},
	}
	for _, tt := range tests {
		if got := pyRepr(tt.in); got != tt.want {
			t.Errorf("pyRepr(%q) = %s, want %s", tt.in, got, tt.want)
		}
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
