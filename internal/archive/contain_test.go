package archive

import (
	"os"
	"path/filepath"
	"testing"
)

// containRoot is a new directory, symlinks resolved, holding proj/ and
// outside/; the test runs in proj/.
func containRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{"proj", "outside/dep"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("x", filepath.Join(root, "probe")); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	t.Chdir(filepath.Join(root, "proj"))
	return root
}

func containLink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func TestCheckWithin(t *testing.T) {
	tests := []struct {
		name   string
		key    string
		setup  func(t *testing.T, root string)
		refuse func(root string) *ContainmentError
	}{
		{
			name: "parent symlink leads outside",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				containLink(t, filepath.Join(root, "outside"), "apm_modules/_local")
			},
			refuse: func(root string) *ContainmentError {
				return &ContainmentError{filepath.Join(root, "proj/apm_modules/_local/dep"), filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "outside/dep")}
			},
		},
		{
			name: "last component symlink leads outside",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				containLink(t, filepath.Join(root, "outside/dep"), "apm_modules/_local/dep")
			},
			refuse: func(root string) *ContainmentError {
				return &ContainmentError{filepath.Join(root, "proj/apm_modules/_local/dep"), filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "outside/dep")}
			},
		},
		{
			name: "last component dangles outside",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				containLink(t, "../../../outside/gone/x", "apm_modules/_local/dep")
			},
			refuse: func(root string) *ContainmentError {
				return &ContainmentError{filepath.Join(root, "proj/apm_modules/_local/dep"), filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "outside/gone/x")}
			},
		},
		{
			name: "dangling link through a dangling link",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				containLink(t, filepath.Join(root, "outside/hop"), "apm_modules/_local/dep")
				containLink(t, filepath.Join(root, "outside/gone"), filepath.Join(root, "outside/hop"))
			},
			refuse: func(root string) *ContainmentError {
				return &ContainmentError{filepath.Join(root, "proj/apm_modules/_local/dep"), filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "outside/gone")}
			},
		},
		{
			name: "last component links to another directory in apm_modules",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll("apm_modules/other", 0o755); err != nil {
					t.Fatal(err)
				}
				containLink(t, "../other", "apm_modules/_local/dep")
			},
		},
		{
			name: "apm_modules is a symlink",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll(filepath.Join(root, "outside/modules/_local/dep"), 0o755); err != nil {
					t.Fatal(err)
				}
				containLink(t, filepath.Join(root, "outside/modules"), "apm_modules")
			},
		},
		{
			name:  "apm_modules does not exist",
			key:   "o/r",
			setup: func(*testing.T, string) {},
		},
		{
			name: "key does not exist",
			key:  "o/r",
			setup: func(t *testing.T, root string) {
				if err := os.MkdirAll("apm_modules", 0o755); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "resolves to apm_modules itself",
			key:  "_local/dep",
			setup: func(t *testing.T, root string) {
				containLink(t, "..", "apm_modules/_local/dep")
			},
			refuse: func(root string) *ContainmentError {
				return &ContainmentError{filepath.Join(root, "proj/apm_modules/_local/dep"), filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "proj/apm_modules")}
			},
		},
		{
			name:  "dot-dot segment",
			key:   "o/..",
			setup: func(*testing.T, string) {},
			refuse: func(root string) *ContainmentError {
				return &ContainmentError{filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "proj/apm_modules"), filepath.Join(root, "proj/apm_modules")}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := containRoot(t)
			tt.setup(t, root)

			err := CheckWithin("apm_modules", filepath.Join("apm_modules", filepath.FromSlash(tt.key)))

			if tt.refuse == nil {
				if err != nil {
					t.Fatalf("CheckWithin: %v, want nil", err)
				}
				return
			}
			want := tt.refuse(root)
			if err == nil || err.Error() != want.Error() {
				t.Fatalf("CheckWithin: %v\nwant %s", err, want)
			}
		})
	}
}

func TestContainmentError_Text(t *testing.T) {
	err := &ContainmentError{Path: "/p/apm_modules/_local/dep", Base: "/p/apm_modules", Resolved: "/r/outside/dep"}
	want := "Cannot verify containment of '/p/apm_modules/_local/dep' within '/p/apm_modules': Path '/p/apm_modules/_local/dep' resolves to '/r/outside/dep' which is outside the allowed base directory '/p/apm_modules'"
	if err.Error() != want {
		t.Errorf("Error() = %s\nwant %s", err.Error(), want)
	}
}

// ContainedKey's ".." check still comes first: the caller refuses the key
// before CheckWithin ever runs.
func TestContainedKey_DotDotBeforeResolve(t *testing.T) {
	if ContainedKey("apm_modules", "o/../r") {
		t.Error(`ContainedKey("apm_modules", "o/../r") = true, want false`)
	}
}
