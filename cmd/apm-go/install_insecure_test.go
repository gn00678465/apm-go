package main

import (
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// recordingLoader returns the manifest given for a repository and records
// every reference it is asked to fetch.
type recordingLoader struct {
	packages map[string]string // repo_url -> apm.yml
	fetched  []*manifest.DependencyReference
}

func (l *recordingLoader) LoadPackage(ref *manifest.DependencyReference, _ string) (*manifest.Manifest, error) {
	l.fetched = append(l.fetched, ref)
	src, ok := l.packages[ref.RepoURL]
	if !ok {
		return nil, nil
	}
	node, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		return nil, err
	}
	m, _, err := manifest.ParseManifest(node)
	return m, err
}

func (l *recordingLoader) approved() map[string]bool {
	got := map[string]bool{}
	for _, r := range l.fetched {
		got[r.RepoURL] = r.HTTPApproved
	}
	return got
}

// TestInstall_HTTPPolicy holds the lines of Oracle 0.33.0
// (issue151-impl/m6-or/<case>/i1.out): an approved http dependency is fetched
// with http allowed for it alone and warned about before the Targets line, a
// transitive one from an unapproved host is not fetched and refuses the
// install (install/insecure_policy.py).
func TestInstall_HTTPPolicy(t *testing.T) {
	parent := "name: parent\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: http://127.0.0.1/o/skill.git\n      allow_insecure: true\n"
	cases := []struct {
		name, deps string
		flag       bool
		hosts      []string
		wantExit   int
		want       []string // lines of stdout, in order
		approved   map[string]bool
	}{
		{"httpgh", "    - git: http://127.0.0.1/o/pkg.git\n      ref: main\n      allow_insecure: true\n    - git: https://127.0.0.1/o/skill.git\n",
			true, nil, 0,
			[]string{" ! Insecure HTTP fetch (unencrypted): http://127.0.0.1/o/pkg#main\n i Targets:"},
			map[string]bool{"o/pkg": true, "o/skill": false}},
		{"httptrans", "    - git: https://127.0.0.1/o/parent.git\n", false, nil, 1,
			[]string{" ! Insecure HTTP fetch (unencrypted): http://127.0.0.1/o/skill (transitive, introduced by o/parent)\n x Re-run with --allow-insecure-host 127.0.0.1 to allow transitive HTTP dependencies from unapproved host(s): 127.0.0.1.\n"},
			map[string]bool{"o/parent": false}},
		{"httptr3", "    - git: https://127.0.0.1/o/parent.git\n", false, []string{"127.0.0.1"}, 0,
			[]string{" ! Insecure HTTP fetch (unencrypted): http://127.0.0.1/o/skill (transitive, introduced by o/parent)\n i Targets:"},
			map[string]bool{"o/parent": false, "o/skill": true}},
		{"host of a direct one", "    - git: https://127.0.0.1/o/parent.git\n    - git: http://127.0.0.1/o/pkg.git\n      allow_insecure: true\n", true, nil, 0,
			[]string{" ! Insecure HTTP fetch (unencrypted): http://127.0.0.1/o/pkg\n ! Insecure HTTP fetch (unencrypted): http://127.0.0.1/o/skill (transitive, introduced by o/parent)\n i Targets:"},
			map[string]bool{"o/parent": false, "o/pkg": true, "o/skill": true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			os.WriteFile("apm.yml", []byte("name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"+tc.deps), 0o644)
			loader := &recordingLoader{packages: map[string]string{"o/parent": parent}}
			var err error
			out := captureInstallStdout(t, func() {
				err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: loader, allowInsecure: tc.flag, allowInsecureHosts: tc.hosts}, false, true, "", "", nil, nil)
			})
			if exitCodeOf(err) != tc.wantExit && !(tc.wantExit == 0 && err == nil) {
				t.Fatalf("exit %d (%v), want %d\n%s", exitCodeOf(err), err, tc.wantExit, out)
			}
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("stdout lacks %q:\n%s", w, out)
				}
			}
			if got := loader.approved(); len(got) != len(tc.approved) {
				t.Errorf("fetched %v, want %v", got, tc.approved)
			} else {
				for k, v := range tc.approved {
					if got[k] != v {
						t.Errorf("%s fetched with http approved %v, want %v", k, got[k], v)
					}
				}
			}
		})
	}
}

// TestInstall_HTTPDependencyLockFields: the lock records is_insecure and
// allow_insecure of an http dependency (deps/lockfile.py:408-411;
// issue151/r6-or/httpok/i1.lock), and reads them back.
func TestInstall_HTTPDependencyLockFields(t *testing.T) {
	t.Chdir(t.TempDir())
	os.WriteFile("apm.yml", []byte("name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - git: http://127.0.0.1/o/pkg.git\n      ref: main\n      allow_insecure: true\n"), 0o644)
	var err error
	captureInstallStdout(t, func() {
		err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: &recordingLoader{}, allowInsecure: true}, false, true, "", "", nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	lock, _ := os.ReadFile("apm.lock.yaml")
	if !strings.Contains(string(lock), "  is_insecure: true\n  allow_insecure: true\n") {
		t.Errorf("lock:\n%s", lock)
	}
	if entry := subdirLockEntry(t, ".", "o/pkg"); !entry.IsInsecure || !entry.AllowInsecure {
		t.Errorf("read back is_insecure %v allow_insecure %v", entry.IsInsecure, entry.AllowInsecure)
	}
}
