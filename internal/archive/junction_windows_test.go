package archive

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

// junction makes an NTFS junction at link pointing at target. A junction
// needs no privilege, which is why it is the link a user is most likely to
// have; the test is skipped where mklink cannot make one.
func junction(t *testing.T, link, target string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Skipf("mklink /J %s %s: %v: %s", link, target, err, out)
	}
}

// junctionRoot is a new directory, symlinks resolved, holding proj/ and
// outside/dep; the test runs in proj/.
func junctionRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"proj/apm_modules", "outside/dep"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(filepath.Join(root, "proj"))
	return root
}

// go.mod's godebug winsymlink=0 is what makes every check below see a
// junction: without it os.Lstat reports ModeIrregular (issue #116).
func TestJunction_LstatReportsSymlink(t *testing.T) {
	root := junctionRoot(t)
	link := filepath.Join(root, "proj", "j")
	junction(t, link, filepath.Join(root, "outside"))

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("os.Lstat(junction).Mode() = %v, want ModeSymlink", info.Mode())
	}
}

// A junction gets the refusal TestCheckWithin expects for a symlink.
func TestCheckWithin_Junction(t *testing.T) {
	for _, tt := range []struct {
		name string
		link string
		to   string
	}{
		{name: "parent junction leads outside", link: "apm_modules/_local", to: "outside"},
		{name: "last component junction leads outside", link: "apm_modules/_local/dep", to: "outside/dep"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			root := junctionRoot(t)
			junction(t, filepath.Join(root, "proj", filepath.FromSlash(tt.link)), filepath.Join(root, filepath.FromSlash(tt.to)))

			err := CheckWithin("apm_modules", filepath.Join("apm_modules", "_local", "dep"))

			var ce *ContainmentError
			if !errors.As(err, &ce) {
				t.Fatalf("CheckWithin = %v, want *ContainmentError", err)
			}
			want := &ContainmentError{
				Path:     filepath.Join(root, "proj", "apm_modules", "_local", "dep"),
				Base:     filepath.Join(root, "proj", "apm_modules"),
				Resolved: filepath.Join(root, "outside", "dep"),
			}
			if !reflect.DeepEqual(ce, want) {
				t.Errorf("CheckWithin = %+v, want %+v", ce, want)
			}
		})
	}
}

// A junction in a package gets the verdict TestResolveIn and TestCheckTree
// expect for a directory symlink.
func TestResolveIn_Junction(t *testing.T) {
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	root, outside := filepath.Join(base, "pkg"), filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "a"), filepath.Join(outside, "dir")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "a", "b.md"), []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	junction(t, filepath.Join(root, "in"), filepath.Join(root, "a"))
	junction(t, filepath.Join(root, "out"), filepath.Join(outside, "dir"))

	if got, err := ResolveIn(root, "in/b.md"); err != nil || got != "a/b.md" {
		t.Errorf("ResolveIn(in/b.md) = %q, %v; want a/b.md", got, err)
	}
	want := "symlink out resolves to " + filepath.Join(outside, "dir") + ", outside the package"
	if got, err := ResolveIn(root, "out/b.md"); err == nil || err.Error() != want {
		t.Errorf("ResolveIn(out/b.md) = %q, %v; want error %q", got, err, want)
	}
	if err := CheckTree(root, "."); err == nil || err.Error() != want {
		t.Errorf("CheckTree = %v; want %q", err, want)
	}
}
