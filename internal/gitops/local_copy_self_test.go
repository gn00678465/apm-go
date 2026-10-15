package gitops

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func writePkg(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apm.yml"), []byte("name: dep\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "important.txt"), []byte("must survive\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func localRef(declared, src string) *manifest.DependencyReference {
	return &manifest.DependencyReference{
		RepoURL: "_local/" + filepath.Base(src), Source: "git",
		LocalSourcePath: src, LocalDeclaredPath: declared,
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// snapshot lists every path under dir with its content or link target,
// without following symlinks.
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			target, err := os.Readlink(p)
			got[p] = "-> " + target
			return err
		case d.IsDir():
			got[p] = "dir"
		default:
			b, err := os.ReadFile(p)
			got[p] = string(b)
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// root/proj is the working directory; root/dep is a package beside it.
func localCopyRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "proj"), 0o755); err != nil {
		t.Fatal(err)
	}
	writePkg(t, filepath.Join(root, "dep"))
	t.Chdir(filepath.Join(root, "proj"))
	return root
}

// A copy that would not land inside apm_modules once symlinks are resolved,
// that overlaps its source, or whose source holds the project is refused
// before anything is removed or created.
func TestMaterializeLocalCopy_RefusesUnsafeCopy(t *testing.T) {
	const projectParent = " is the project directory or one of its parent directories; it cannot be copied into apm_modules/"
	tests := []struct {
		name     string
		setup    func(t *testing.T, root string) (declared, src string)
		wantFrom func(root string) string
	}{
		{
			name: "destination resolves to the source",
			setup: func(t *testing.T, root string) (string, string) {
				symlink(t, root, "apm_modules/_local")
				return "../dep", filepath.Join(root, "dep")
			},
			wantFrom: func(root string) string {
				return "local dependency ../dep would be copied to apm_modules/_local/dep, which resolves to " + filepath.Join(root, "dep") + " outside apm_modules/; refusing to replace it"
			},
		},
		{
			name: "destination resolves inside the source",
			setup: func(t *testing.T, root string) (string, string) {
				symlink(t, filepath.Join(root, "dep"), "apm_modules/_local")
				return "../dep", filepath.Join(root, "dep")
			},
			wantFrom: func(root string) string {
				return "local dependency ../dep would be copied to apm_modules/_local/dep, which resolves to " + filepath.Join(root, "dep", "dep") + " outside apm_modules/; refusing to replace it"
			},
		},
		{
			name: "destination resolves to a directory holding the source",
			setup: func(t *testing.T, root string) (string, string) {
				writePkg(t, filepath.Join(root, "dep", "x", "dep"))
				symlink(t, root, "apm_modules/_local")
				return "../dep/x/dep", filepath.Join(root, "dep", "x", "dep")
			},
			wantFrom: func(root string) string {
				return "local dependency ../dep/x/dep would be copied to apm_modules/_local/dep, which resolves to " + filepath.Join(root, "dep") + " outside apm_modules/; refusing to replace it"
			},
		},
		{
			name: "destination resolves to an unrelated directory",
			setup: func(t *testing.T, root string) (string, string) {
				writePkg(t, filepath.Join(root, "ext", "dep"))
				symlink(t, filepath.Join(root, "ext"), "apm_modules/_local")
				return "../dep", filepath.Join(root, "dep")
			},
			wantFrom: func(root string) string {
				return "local dependency ../dep would be copied to apm_modules/_local/dep, which resolves to " + filepath.Join(root, "ext", "dep") + " outside apm_modules/; refusing to replace it"
			},
		},
		{
			name: "source is its own copy in apm_modules",
			setup: func(t *testing.T, root string) (string, string) {
				writePkg(t, filepath.Join(root, "proj", "apm_modules", "_local", "dep"))
				return "./apm_modules/_local/dep", filepath.Join(root, "proj", "apm_modules", "_local", "dep")
			},
			wantFrom: func(string) string {
				return "local dependency ./apm_modules/_local/dep overlaps its copy apm_modules/_local/dep; a package cannot be copied into or over itself"
			},
		},
		{
			name: "source holds the project",
			setup: func(t *testing.T, root string) (string, string) {
				return "../../" + filepath.Base(root), root
			},
			wantFrom: func(root string) string { return "local dependency ../../" + filepath.Base(root) + projectParent },
		},
		{
			name: "source is the project",
			setup: func(t *testing.T, root string) (string, string) {
				return "../proj", filepath.Join(root, "proj")
			},
			wantFrom: func(string) string { return "local dependency ../proj" + projectParent },
		},
		{
			name: "symlink to a parent of the project",
			setup: func(t *testing.T, root string) (string, string) {
				symlink(t, root, filepath.Join(root, "up"))
				return "../up", filepath.Join(root, "up")
			},
			wantFrom: func(string) string { return "local dependency ../up" + projectParent },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := localCopyRoot(t)
			declared, src := tt.setup(t, root)
			before := snapshot(t, root)

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			_, err := r.LoadPackage(localRef(declared, src), "")

			if want := tt.wantFrom(root); err == nil || err.Error() != want {
				t.Fatalf("error %v, want %s", err, want)
			}
			if after := snapshot(t, root); !reflect.DeepEqual(before, after) {
				t.Errorf("files changed by a refused copy:\nbefore %v\nafter  %v", before, after)
			}
			if err := CheckLocalCopy("apm_modules", localRef(declared, src)); err == nil || err.Error() != tt.wantFrom(root) {
				t.Errorf("CheckLocalCopy: error %v, want %s", err, tt.wantFrom(root))
			}
		})
	}
}

// update --dry-run copies into a scratch directory outside the project; a
// source holding that directory overlaps its copy.
func TestMaterializeLocalCopy_RefusesSourceHoldingModulesDir(t *testing.T) {
	t.Chdir(t.TempDir())
	scratch, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(scratch, "pkgs")
	writePkg(t, src)
	modules := filepath.Join(src, "modules")
	r := &RealPackageLoader{ModulesDir: modules}

	_, err = r.LoadPackage(localRef("/x/pkgs", src), "")
	want := "local dependency /x/pkgs overlaps its copy " + filepath.Join(modules, "_local", "pkgs") + "; a package cannot be copied into or over itself"
	if err == nil || err.Error() != want {
		t.Fatalf("error %v, want %s", err, want)
	}
	if _, err := os.Lstat(modules); !os.IsNotExist(err) {
		t.Errorf("%s exists after a refused copy (lstat error %v)", modules, err)
	}
}

// A symlink at the destination itself is replaced, not followed: RemoveAll
// removes the link and leaves its target alone.
func TestMaterializeLocalCopy_ReplacesSymlinkAtDestination(t *testing.T) {
	root := localCopyRoot(t)
	writePkg(t, filepath.Join(root, "other"))
	symlink(t, filepath.Join(root, "other"), "apm_modules/_local/dep")
	other := snapshot(t, filepath.Join(root, "other"))

	r := &RealPackageLoader{ModulesDir: "apm_modules"}
	if _, err := r.LoadPackage(localRef("../dep", filepath.Join(root, "dep")), ""); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Lstat("apm_modules/_local/dep"); err != nil || !info.IsDir() {
		t.Errorf("apm_modules/_local/dep is not a directory after the copy (lstat %v, error %v)", info, err)
	}
	if got := snapshot(t, filepath.Join(root, "other")); !reflect.DeepEqual(got, other) {
		t.Errorf("the link's target changed: %v, want %v", got, other)
	}
}

func TestMaterializeLocalCopy_CopiesSiblingOfTheProject(t *testing.T) {
	root := localCopyRoot(t)

	r := &RealPackageLoader{ModulesDir: "apm_modules"}
	if _, err := r.LoadPackage(localRef("../dep", filepath.Join(root, "dep")), ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(root, "proj", "apm_modules", "_local", "dep", "important.txt"))
	if err != nil || string(got) != "must survive\n" {
		t.Errorf("copied important.txt %q, error %v", got, err)
	}
}

// A declared source that runs through the destination link is read where it
// pointed when it was checked, not through the link the copy removes first.
func TestMaterializeLocalCopy_SourceThroughDestinationLink(t *testing.T) {
	tests := []struct {
		name, declared string
		link, src      func(root string) string
	}{
		{
			name:     "source is the destination link",
			declared: "./apm_modules/_local/dep",
			link:     func(root string) string { return filepath.Join(root, "dep") },
			src:      func(root string) string { return filepath.Join(root, "dep") },
		},
		{
			name:     "source is below the destination link",
			declared: "./apm_modules/_local/dep/x/dep",
			link:     func(root string) string { return filepath.Join(root, "foo") },
			src:      func(root string) string { return filepath.Join(root, "foo", "x", "dep") },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := localCopyRoot(t)
			writePkg(t, filepath.Join(root, "foo", "x", "dep"))
			symlink(t, tt.link(root), "apm_modules/_local/dep")
			src := tt.src(root)
			want := snapshot(t, src)
			ref := localRef(tt.declared, filepath.Join(root, "proj", filepath.FromSlash(tt.declared)))

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			if _, err := r.LoadPackage(ref, ""); err != nil {
				t.Fatal(err)
			}
			if info, err := os.Lstat("apm_modules/_local/dep"); err != nil || !info.IsDir() {
				t.Fatalf("apm_modules/_local/dep is not a directory (lstat %v, error %v)", info, err)
			}
			for _, name := range []string{"apm.yml", "important.txt"} {
				got, err := os.ReadFile(filepath.Join("apm_modules", "_local", "dep", name))
				if err != nil || string(got) != want[filepath.Join(src, name)] {
					t.Errorf("copied %s = %q (err %v), want %q", name, got, err, want[filepath.Join(src, name)])
				}
			}
			if got := snapshot(t, src); !reflect.DeepEqual(got, want) {
				t.Errorf("the source changed: %v, want %v", got, want)
			}
		})
	}
}
