package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// testdata/oracle/git-*.lock.yaml are the locks Oracle 0.33.0 wrote for git
// dependencies (issue #151, measured: issue151-impl/r9-or-e/alias,
// r9-or-d/httpaiflag, r9-or-c/kinds): an alias, an http:// dependency with
// is_insecure and allow_insecure, and subdirectory packages with is_virtual.
func readOracleGitLock(t *testing.T, name string) ([]byte, *Lockfile) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "oracle", name+".lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(doc)
	if err != nil {
		t.Fatal(err)
	}
	return data, lf
}

func TestParseLockfile_OracleGitIdentityFields(t *testing.T) {
	_, alias := readOracleGitLock(t, "git-alias")
	if d := alias.FindByKey("o/skill"); d == nil || d.Alias != "custom" || d.ModuleDir() != "custom" || d.PackageType != "claude_skill" {
		t.Errorf("o/skill = %+v, want alias custom, claude_skill", d)
	}
	_, http := readOracleGitLock(t, "git-http-insecure")
	if d := http.FindByKey("o/pkg"); d == nil || !d.IsInsecure || !d.AllowInsecure {
		t.Errorf("o/pkg = %+v, want is_insecure and allow_insecure", d)
	}
	_, kinds := readOracleGitLock(t, "git-kinds")
	if d := kinds.FindByKey("o/mono/a"); d == nil || !d.IsVirtual || d.Name != "mono-a" {
		t.Errorf("o/mono/a = %+v, want is_virtual and name mono-a", d)
	}
}

// Every byte comes from the model, content_hash included.
func TestWriteLockfile_ReproducesOracleGitLocks(t *testing.T) {
	for _, name := range []string{"git-alias", "git-http-insecure", "git-kinds"} {
		t.Run(name, func(t *testing.T) {
			want, lf := readOracleGitLock(t, name)
			doc, _ := yamlcore.SafeLoad(want)
			got, err := WriteLockfile(lf, doc)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("lock differs from the Oracle's\n--- got\n%s\n--- want\n%s", got, want)
			}
		})
	}
}

// A lock that lacks one of the fields differs, so an install writes it again.
func TestIsSemanticEqual_GitIdentityFields(t *testing.T) {
	flips := map[string]func(*LockedDep){
		"alias":          func(d *LockedDep) { d.Alias = "" },
		"is_virtual":     func(d *LockedDep) { d.IsVirtual = !d.IsVirtual },
		"is_insecure":    func(d *LockedDep) { d.IsInsecure = !d.IsInsecure },
		"allow_insecure": func(d *LockedDep) { d.AllowInsecure = !d.AllowInsecure },
		"name":           func(d *LockedDep) { d.Name = "" },
		"package_type":   func(d *LockedDep) { d.PackageType = "" },
		"content_hash":   func(d *LockedDep) { d.ContentHash = "" },
	}
	for field, flip := range flips {
		t.Run(field, func(t *testing.T) {
			_, a := readOracleGitLock(t, "git-alias")
			_, b := readOracleGitLock(t, "git-alias")
			if !IsSemanticEqual(a, b) {
				t.Fatal("one lock read twice differs")
			}
			flip(&b.Dependencies[0])
			if IsSemanticEqual(a, b) {
				t.Errorf("a change of %s is not a difference", field)
			}
		})
	}
}

// A lock entry's alias is read as Oracle 0.33.0 reads it
// (deps/lockfile.py:286-296): one that names another package's directory or
// leaves apm_modules refuses the whole lock, with the Oracle's text; one with
// whitespace around it is that alias stripped. The expected texts are the
// Oracle's (measured: issue151-impl/r14-la-or/v<N>/frozen/out).
func TestParseLockfile_LockAlias(t *testing.T) {
	const tail = ". Restore a known-good apm.lock.yaml from version control or correct this entry's alias to its intended placement. Do not delete content at a rejected alias destination."
	cases := []struct{ yaml, want, err string }{
		{"o/skill", "", "Invalid lockfile dependency alias 'o/skill'" + tail},
		{"a/b", "", "Invalid lockfile dependency alias 'a/b'" + tail},
		{"../x", "", "Invalid lockfile dependency alias '../x'" + tail},
		{".", "", "Invalid lockfile dependency alias '.'" + tail},
		{"..", "", "Invalid lockfile dependency alias '..'" + tail},
		{"''", "", "Invalid lockfile dependency alias ''" + tail},
		{`o\skill`, "", `Invalid lockfile dependency alias 'o\\skill'` + tail},
		{"'  x  '", "x", ""},
		{"custom", "custom", ""},
	}
	for _, tc := range cases {
		t.Run(tc.yaml, func(t *testing.T) {
			doc, err := yamlcore.SafeLoad([]byte("lockfile_version: '1'\ndependencies:\n- repo_url: o/pkg\n  alias: " + tc.yaml + "\n  resolved_commit: b4e35f0dc0a022142cf5b403f9d1377d6f53c8d4\n"))
			if err != nil {
				t.Fatal(err)
			}
			lf, err := ParseLockfile(doc)
			if tc.err != "" {
				if err == nil || err.Error() != tc.err {
					t.Fatalf("err = %v, want %q", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := lf.Dependencies[0].Alias; got != tc.want {
				t.Errorf("alias = %q, want %q", got, tc.want)
			}
			out, err := WriteLockfile(lf, nil)
			if err != nil {
				t.Fatal(err)
			}
			if want := "  alias: " + tc.want + "\n"; !strings.Contains(string(out), want) {
				t.Errorf("written lock has no %q:\n%s", want, out)
			}
		})
	}
}
