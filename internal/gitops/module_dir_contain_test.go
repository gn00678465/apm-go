package gitops

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

// Issue #86: apm_modules/o is a link out of apm_modules, and outside/o/r holds
// a file that is not a checkout. LoadPackage refuses before the stale-checkout
// repair removes it and before any clone, so the test needs no network.
func TestLoadPackage_RefusesModuleDirOutsideModules(t *testing.T) {
	tests := []struct {
		name string
		ref  *manifest.DependencyReference
		key  string
	}{
		{name: "repository", ref: &manifest.DependencyReference{RepoURL: "o/r", Source: "git"}, key: "o/r"},
		{name: "subdirectory", ref: &manifest.DependencyReference{RepoURL: "o/r", Source: "git", VirtualPath: "sub", VirtualType: "subdirectory"}, key: "o/r/sub"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := localCopyRoot(t)
			if err := os.Symlink("x", filepath.Join(root, "probe")); err != nil {
				t.Skipf("symlink unsupported on this platform: %v", err)
			}
			outsideDir := filepath.Join(root, "outside", filepath.FromSlash(tt.key))
			writePkg(t, outsideDir)
			if err := os.WriteFile(filepath.Join(outsideDir, "precious.txt"), []byte("precious\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			symlink(t, filepath.Join(root, "outside", "o"), "apm_modules/o")
			before := snapshot(t, root)

			r := &RealPackageLoader{ModulesDir: "apm_modules"}
			_, err := r.LoadPackage(tt.ref, "")

			base := filepath.Join(root, "proj", "apm_modules")
			abs := filepath.Join(base, filepath.FromSlash(tt.key))
			want := "Cannot verify containment of '" + abs + "' within '" + base + "': Path '" + abs +
				"' resolves to '" + outsideDir + "' which is outside the allowed base directory '" + base + "'"
			if err == nil || err.Error() != want {
				t.Fatalf("LoadPackage: %v\nwant %s", err, want)
			}
			if got := snapshot(t, root); !reflect.DeepEqual(got, before) {
				t.Errorf("tree changed:\n%v\nwant\n%v", got, before)
			}
		})
	}
}
