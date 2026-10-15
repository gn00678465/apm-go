package gitops

import (
	"os"
	"path/filepath"
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
}

func localRef(declared, src string) *manifest.DependencyReference {
	return &manifest.DependencyReference{
		RepoURL: "_local/" + filepath.Base(src), Source: "git",
		LocalSourcePath: src, LocalDeclaredPath: declared,
	}
}

// A source that holds the directory it is copied into would copy itself
// forever, so it is refused before anything is written.
func TestMaterializeLocalCopy_RefusesSourceHoldingTheCopy(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	writePkg(t, proj)
	if err := os.Symlink(root, filepath.Join(root, "up")); err != nil {
		t.Fatal(err)
	}
	t.Chdir(proj)

	const projectParent = " is the project directory or one of its parent directories; it cannot be copied into apm_modules/"
	tests := []struct{ name, declared, src, want string }{
		{"project directory", "../proj", proj, "local dependency ../proj" + projectParent},
		{"parent of the project", "../../" + filepath.Base(root), root, "local dependency ../../" + filepath.Base(root) + projectParent},
		{"symlink to a parent", "../up", filepath.Join(root, "up"), "local dependency ../up" + projectParent},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			_, err := r.LoadPackage(localRef(tt.declared, tt.src), "")
			if err == nil || err.Error() != tt.want {
				t.Fatalf("error %v, want %s", err, tt.want)
			}
			if _, err := os.Lstat(filepath.Join(proj, "apm_modules")); !os.IsNotExist(err) {
				t.Errorf("apm_modules exists after a refused copy (lstat error %v)", err)
			}
		})
	}
}

// update --dry-run copies into a scratch directory outside the project; a
// source holding that directory is refused the same way.
func TestMaterializeLocalCopy_RefusesSourceHoldingModulesDir(t *testing.T) {
	t.Chdir(t.TempDir())
	scratch := t.TempDir()
	src := filepath.Join(scratch, "pkgs")
	writePkg(t, src)
	modules := filepath.Join(src, "modules")
	r := &RealPackageLoader{ModulesDir: modules}

	_, err := r.LoadPackage(localRef("/x/pkgs", src), "")
	want := "local dependency /x/pkgs is " + modules + " or one of its parent directories; it cannot be copied into " + modules + "/"
	if err == nil || err.Error() != want {
		t.Fatalf("error %v, want %s", err, want)
	}
	if _, err := os.Lstat(modules); !os.IsNotExist(err) {
		t.Errorf("%s exists after a refused copy (lstat error %v)", modules, err)
	}
}

func TestMaterializeLocalCopy_CopiesSiblingOfTheProject(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	dep := filepath.Join(root, "dep")
	writePkg(t, proj)
	writePkg(t, dep)
	t.Chdir(proj)

	r := &RealPackageLoader{ModulesDir: "apm_modules"}
	if _, err := r.LoadPackage(localRef("../dep", dep), ""); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(proj, "apm_modules", "_local", "dep", "apm.yml"))
	if err != nil || string(got) != "name: dep\nversion: 1.0.0\n" {
		t.Errorf("copied apm.yml %q, error %v", got, err)
	}
}
