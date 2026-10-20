package main

import (
	"os"
	"strings"
	"testing"
)

// frozenSyncLock is the lock an install of `git: https://127.0.0.1/o/pkg.git`
// with `ref: main` and of `o/skill` with no ref writes.
const frozenSyncLock = `lockfile_version: '1'
dependencies:
- repo_url: o/pkg
  host: 127.0.0.1
  resolved_commit: b4e35f0dc0a022142cf5b403f9d1377d6f53c8d4
  resolved_ref: main
- repo_url: o/skill
  host: 127.0.0.1
  resolved_commit: 78301d38cad465475b02ba05d6678aa4d8399e46
`

// TestFrozenInstall_ManifestOutOfSync holds the lines Oracle 0.33.0 prints
// for each case (~/.cache/apm-go-verify/issue151/r3-or/<case>/fz.out), with
// the Oracle's tip as apm-go words it.
func TestFrozenInstall_ManifestOutOfSync(t *testing.T) {
	const head = " x --frozen: apm.lock.yaml is out of sync with apm.yml.\n"
	const tip = " i Tip: rerun 'apm-go install' without --frozen to create or repair package lock state, retaining the original options, including --global if specified.\n"
	pkg := "    - git: https://127.0.0.1/o/pkg.git\n"
	skill := "    - git: https://127.0.0.1/o/skill.git\n"
	cases := []struct {
		name, deps, dev, want string
	}{
		{"retag", pkg + "      ref: v1.0.0\n" + skill, "",
			"  - 127.0.0.1/o/pkg: declared source, ref, host provider, or transport differs from apm.lock.yaml\n"},
		{"refdel", pkg + skill, "",
			"  - 127.0.0.1/o/pkg: declared source, ref, host provider, or transport differs from apm.lock.yaml\n"},
		{"refadd", pkg + "      ref: main\n" + skill + "      ref: main\n", "",
			"  - 127.0.0.1/o/skill: declared source, ref, host provider, or transport differs from apm.lock.yaml\n"},
		{"fullsha", pkg + "      ref: 44a18f4023cf5abc5f951e471f3065e1b586f318\n" + skill, "",
			"  - 127.0.0.1/o/pkg: manifest commit '44a18f4023cf5abc5f951e471f3065e1b586f318' != lockfile resolved_commit 'b4e35f0dc0a022142cf5b403f9d1377d6f53c8d4'\n"},
		{"missing", pkg + "      ref: main\n" + skill + "    - git: https://127.0.0.1/o/bundle.git\n", "",
			"  - 127.0.0.1/o/bundle is declared in apm.yml but missing from apm.lock.yaml\n"},
		{"two", pkg + "      ref: v1.0.0\n" + skill + "      ref: main\n", "",
			"  - 127.0.0.1/o/pkg: declared source, ref, host provider, or transport differs from apm.lock.yaml\n" +
				"  - 127.0.0.1/o/skill: declared source, ref, host provider, or transport differs from apm.lock.yaml\n"},
		{"dev", pkg + "      ref: main\n", skill + "      ref: v2\n",
			"  - 127.0.0.1/o/skill: declared source, ref, host provider, or transport differs from apm.lock.yaml\n"},
		{"http", "    - git: http://127.0.0.1/o/pkg.git\n      ref: main\n      allow_insecure: true\n" + skill, "",
			"  - 127.0.0.1/o/pkg: declared source, ref, host provider, or transport differs from apm.lock.yaml\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Chdir(t.TempDir())
			yml := "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n" + tc.deps
			if tc.dev != "" {
				yml += "devDependencies:\n  apm:\n" + tc.dev
			}
			os.WriteFile("apm.yml", []byte(yml), 0o644)
			os.WriteFile("apm.lock.yaml", []byte(frozenSyncLock), 0o644)
			spy := &spyLoader{}
			var err error
			out := captureInstallStdout(t, func() {
				err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: spy, allowInsecure: true}, true, false, "", "", nil, nil)
			})
			if exitCodeOf(err) != 1 {
				t.Fatalf("exit code %d (%v), want 1", exitCodeOf(err), err)
			}
			if want := head + tc.want + tip; out != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
			}
			if len(spy.calls) != 0 {
				t.Errorf("fetched %v before refusing", spy.calls)
			}
			if got, _ := os.ReadFile("apm.lock.yaml"); string(got) != frozenSyncLock {
				t.Errorf("the lock changed:\n%s", got)
			}
		})
	}
}

// TestFrozenInstall_ManifestInSync is the declared dependencies the lock
// pins: a ref that did not change, a local package the Oracle does not check.
func TestFrozenInstall_ManifestInSync(t *testing.T) {
	t.Chdir(t.TempDir())
	os.MkdirAll("dep", 0o755)
	os.WriteFile("dep/apm.yml", []byte("name: dep\nversion: 1.0.0\n"), 0o644)
	os.WriteFile("apm.yml", []byte("name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n"+
		"    - git: https://127.0.0.1/o/pkg.git\n      ref: main\n    - git: https://127.0.0.1/o/skill.git\n    - ./dep\n"), 0o644)
	os.WriteFile("apm.lock.yaml", []byte(frozenSyncLock), 0o644)
	var err error
	out := captureInstallStdout(t, func() {
		err = runInstall(&installDeps{tags: &mockInstallTagLister{}, loader: &spyLoader{}}, true, false, "", "", nil, nil)
	})
	if strings.Contains(out, "out of sync") {
		t.Errorf("refused a manifest the lock pins:\n%s (%v)", out, err)
	}
}
