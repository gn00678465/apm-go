package gitops

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// junction makes an NTFS junction at link pointing at target, or skips the
// test where mklink cannot make one. A junction needs no privilege.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
}

// A junction in a local package is copied as a directory symlink is: as the
// directory it points at when that is inside the package
// (TestMaterializeLocalCopy_DereferencesInPackageSymlinks), and refused with
// the symlink's reason when it is outside
// (TestMaterializeLocalCopy_RefusesSymlinkItCannotDereference). Issue #116.
func TestMaterializeLocalCopy_Junction(t *testing.T) {
	t.Run("inside the package", func(t *testing.T) {
		root := localCopyRoot(t)
		dep := filepath.Join(root, "dep")
		writeAgent(t, filepath.Join(dep, "agents", "oracle.agent.md"))
		junction(t, filepath.Join(dep, ".apm", "agents"), filepath.Join(dep, "agents"))

		r := &RealPackageLoader{ModulesDir: "apm_modules"}
		if _, err := r.LoadPackage(localRef("../dep", dep), ""); err != nil {
			t.Fatal(err)
		}
		copied := filepath.Join("apm_modules", "_local", "dep", ".apm", "agents")
		if info, err := os.Lstat(copied); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%s in the copy: lstat %v, error %v; want a real directory", copied, info, err)
		}
		if got, err := os.ReadFile(filepath.Join(copied, "oracle.agent.md")); err != nil || string(got) != agentBody {
			t.Errorf("copied agent = %q, error %v; want %q", got, err, agentBody)
		}
	})
	t.Run("outside the package", func(t *testing.T) {
		root := localCopyRoot(t)
		dep := filepath.Join(root, "dep")
		outside := filepath.Join(root, "outside", "agents")
		writeAgent(t, filepath.Join(outside, "outer.agent.md"))
		link := filepath.Join(dep, ".apm", "agents")
		junction(t, link, outside)

		r := &RealPackageLoader{ModulesDir: "apm_modules"}
		_, err := r.LoadPackage(localRef("../dep", dep), "")
		want := "copy local dependency " + dep + ": Symlink '" + link + "' resolves to '" + outside +
			"', which escapes the package root '" + dep +
			"'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted."
		if err == nil || err.Error() != want {
			t.Errorf("error\n%v\nwant\n%s", err, want)
		}
		if _, err := os.Lstat(filepath.Join("apm_modules", "_local", "dep")); !os.IsNotExist(err) {
			t.Errorf("a copy is left after a refused copy (lstat error %v)", err)
		}
	})
}

func writeAgent(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(agentBody), 0o644); err != nil {
		t.Fatal(err)
	}
}
