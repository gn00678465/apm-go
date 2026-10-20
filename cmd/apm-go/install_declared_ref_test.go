package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	yamllib "go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #156: `install <pkg>#<ref>` of a package apm.yml already declares
// rewrites that entry's ref, as Oracle 0.33.0 does (install/package_resolution.py:345-378,
// commands/install.py:432,482-503), and prints "(updated ref in apm.yml)"
// (core/command_logger.py:289-291), whether or not the ref changed. The
// expected apm.yml entries and lock refs below are the measured ones of
// scenarios s1-s17 (investigation.md), except where a case says it keeps a
// field the Oracle drops (issue #156 comment 6290306546, item 1).

const declaredRefHead = "name: test\nversion: \"1.0.0\"\n"

func declaredRefProject(t *testing.T, body string) {
	t.Helper()
	chdirTemp(t)
	if err := os.WriteFile("apm.yml", []byte(declaredRefHead+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func declaredRefInstall(t *testing.T, skills []string, packages ...string) string {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	var err error
	out := captureInstallCombined(t, func() {
		err = runInstall(deps, false, true, "claude", "", skills, packages)
	})
	if err != nil {
		t.Fatalf("runInstall %v: %v\n%s", packages, err, out)
	}
	return out
}

func declaredRefLockRef(t *testing.T, path, repo string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range lock.Dependencies {
		if strings.EqualFold(d.RepoURL, repo) {
			return d.ResolvedRef
		}
	}
	t.Fatalf("no lock entry for %s in %s", repo, string(data))
	return ""
}

func readFileT(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestInstall_DeclaredRef_Rewrites(t *testing.T) {
	tests := []struct {
		name     string
		before   string // dependencies.apm body
		skills   []string
		pkg      string
		wantYML  string // dependencies.apm body after
		wantLock string
	}{
		{
			name:     "string entry changes ref (s2)",
			before:   "    - acme/foo#v1\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - acme/foo#v2\n",
			wantLock: "v2",
		},
		{
			name:     "object ref: changes ref (s4)",
			before:   "    - git: acme/foo\n      ref: v1\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - git: acme/foo\n      ref: v2\n",
			wantLock: "v2",
		},
		{
			// The Oracle drops skills here (s13); apm-go keeps it.
			name:     "skills kept when --skill is not given (s13)",
			before:   "    - git: acme/foo#v1\n      skills:\n        - a\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - git: acme/foo#v2\n      skills:\n        - a\n",
			wantLock: "v2",
		},
		{
			name:     "alias kept (s14)",
			before:   "    - git: acme/foo\n      ref: v1\n      alias: ghs\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - git: acme/foo\n      ref: v2\n      alias: ghs\n",
			wantLock: "v2",
		},
		{
			name:     "targets kept (s15)",
			before:   "    - git: acme/foo\n      ref: v1\n      targets:\n        - claude\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - git: acme/foo\n      ref: v2\n      targets:\n        - claude\n",
			wantLock: "v2",
		},
		{
			name:     "url spelling kept (s16)",
			before:   "    - git: https://github.com/acme/foo.git\n      ref: v1\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - git: https://github.com/acme/foo.git\n      ref: v2\n",
			wantLock: "v2",
		},
		{
			name:     "case of the declared repository kept (s12)",
			before:   "    - Acme/Foo#v1\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - Acme/Foo#v2\n",
			wantLock: "v2",
		},
		{
			name:     "no ref given removes the ref (s6)",
			before:   "    - acme/foo#v1\n",
			pkg:      "acme/foo",
			wantYML:  "    - acme/foo\n",
			wantLock: "",
		},
		{
			name:     "object ref: removed when none is given",
			before:   "    - git: acme/foo\n      ref: v1\n      alias: ghs\n",
			pkg:      "acme/foo",
			wantYML:  "    - git: acme/foo\n      alias: ghs\n",
			wantLock: "",
		},
		{
			name:     "ref added to a string entry with none (s11)",
			before:   "    - acme/foo\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - acme/foo#v2\n",
			wantLock: "v2",
		},
		{
			name:     "ref: added after git: when neither has one",
			before:   "    - git: acme/foo\n      alias: ghs\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - git: acme/foo\n      ref: v2\n      alias: ghs\n",
			wantLock: "v2",
		},
		{
			// ParseDepDict reads {name: owner/repo} as a git shorthand and never reads ref:.
			name:     "legacy name: entry changes the ref in its value",
			before:   "    - name: acme/foo#v1\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - name: acme/foo#v2\n",
			wantLock: "v2",
		},
		{
			name:     "legacy name: entry with no ref gets one",
			before:   "    - name: acme/foo\n      alias: ghs\n",
			pkg:      "acme/foo#v2",
			wantYML:  "    - name: acme/foo#v2\n      alias: ghs\n",
			wantLock: "v2",
		},
		{
			name:     "legacy name: entry loses its ref",
			before:   "    - name: acme/foo#v1\n",
			pkg:      "acme/foo",
			wantYML:  "    - name: acme/foo\n",
			wantLock: "",
		},
		{
			name:     "same ref changes no byte (s5)",
			before:   "    - acme/foo#v1 # keep me\n    - other/bar\n",
			pkg:      "acme/foo#v1",
			wantYML:  "    - acme/foo#v1 # keep me\n    - other/bar\n",
			wantLock: "v1",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			declaredRefProject(t, "dependencies:\n  apm:\n"+tc.before)
			declaredRefInstall(t, nil, "acme/foo#v1")
			if err := os.WriteFile("apm.yml", []byte(declaredRefHead+"dependencies:\n  apm:\n"+tc.before), 0o644); err != nil {
				t.Fatal(err)
			}
			out := declaredRefInstall(t, tc.skills, tc.pkg)
			if !strings.Contains(out, " + "+tc.pkg+" (updated ref in apm.yml)\n") {
				t.Errorf("missing the updated line for %q in:\n%s", tc.pkg, out)
			}
			if !strings.Contains(out, " + Updated apm.yml dependency entries\n") {
				t.Errorf("missing the dependency entries line in:\n%s", out)
			}
			for _, bad := range []string{"conflicts with already-declared", "No new packages to add", "(already in apm.yml)"} {
				if strings.Contains(out, bad) {
					t.Errorf("unexpected %q in:\n%s", bad, out)
				}
			}
			want := declaredRefHead + "dependencies:\n  apm:\n" + tc.wantYML
			if got := readFileT(t, "apm.yml"); got != want {
				t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
			}
			if got := declaredRefLockRef(t, "apm.lock.yaml", "acme/foo"); got != tc.wantLock {
				t.Errorf("lock resolved_ref = %q, want %q", got, tc.wantLock)
			}
		})
	}
}

func TestInstall_DeclaredRef_OtherSectionMovesAndRewrites(t *testing.T) {
	declaredRefProject(t, "devDependencies:\n  apm:\n    - git: acme/foo\n      ref: v1\n      alias: ghs\n")
	out := declaredRefInstall(t, nil, "acme/foo#v2")
	moved := strings.Index(out, " i acme/foo: moved from devDependencies.apm to dependencies.apm")
	updated := strings.Index(out, " + acme/foo#v2 (updated ref in apm.yml)")
	if moved < 0 || updated < 0 || moved > updated {
		t.Errorf("the move line comes first, then the updated line:\n%s", out)
	}
	m := readManifestParsed(t)
	if len(m.ParsedDeps) != 1 || len(m.ParsedDevDeps) != 0 {
		t.Fatalf("deps %d, devDeps %d; want 1, 0", len(m.ParsedDeps), len(m.ParsedDevDeps))
	}
	if d := m.ParsedDeps[0]; d.Reference != "v2" || d.Alias != "ghs" {
		t.Errorf("moved entry ref %q alias %q; want v2, ghs", d.Reference, d.Alias)
	}
	if got := declaredRefLockRef(t, "apm.lock.yaml", "acme/foo"); got != "v2" {
		t.Errorf("lock resolved_ref = %q, want v2", got)
	}
}

func TestInstall_DeclaredRef_NewPackageAndDeclaredPackage(t *testing.T) {
	declaredRefProject(t, "dependencies:\n  apm:\n    - acme/foo#v1\n")
	out := declaredRefInstall(t, nil, "acme/new#v1", "acme/foo#v2")
	if !strings.Contains(out, " + acme/foo#v2 (updated ref in apm.yml)\n") ||
		!strings.Contains(out, " + Updated apm.yml with 1 new package(s)\n") {
		t.Errorf("missing lines:\n%s", out)
	}
	if strings.Contains(out, "dependency entries") {
		t.Errorf("a call with a new package prints no dependency entries line:\n%s", out)
	}
	if got := readFileT(t, "apm.yml"); got != declaredRefHead+"dependencies:\n  apm:\n    - acme/foo#v2\n    - acme/new#v1\n" {
		t.Errorf("apm.yml:\n%s", got)
	}
}

func TestInstall_DeclaredRef_LocalPackageReinstall(t *testing.T) {
	declaredRefProject(t, "")
	if err := os.MkdirAll("a", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join("a", "apm.yml"), []byte("name: a\nversion: 1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	declaredRefInstall(t, nil, "./a")
	before := readFileT(t, "apm.yml")
	out := declaredRefInstall(t, nil, "./a")
	if !strings.Contains(out, " + ./a (updated ref in apm.yml)\n") || !strings.Contains(out, " + Updated apm.yml dependency entries\n") {
		t.Errorf("a local package reinstall reads as the Oracle's:\n%s", out)
	}
	if got := readFileT(t, "apm.yml"); got != before {
		t.Errorf("apm.yml changed:\n%s", got)
	}
}

func TestInstall_DeclaredRef_Global(t *testing.T) {
	home, _ := globalStaleScope(t, nil)
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	install := func(pkg string) {
		captureInstallCombined(t, func() {
			deployDir, err := enterGlobalScope()
			if err != nil {
				t.Fatal(err)
			}
			if err := runInstall(deps, false, true, "claude", deployDir, nil, []string{pkg}); err != nil {
				t.Fatalf("install -g %s: %v", pkg, err)
			}
		})
	}
	install("acme/foo#v1")
	install("acme/foo#v2")
	yml := filepath.Join(home, ".apm", "apm.yml")
	if got := readFileT(t, yml); !strings.Contains(got, "- acme/foo#v2\n") || strings.Contains(got, "#v1") {
		t.Errorf("~/.apm/apm.yml:\n%s", got)
	}
	if got := declaredRefLockRef(t, filepath.Join(home, ".apm", "apm.lock.yaml"), "acme/foo"); got != "v2" {
		t.Errorf("lock resolved_ref = %q, want v2", got)
	}
}

// `--skill` on a declared package changes ref and skills in one write; the
// ref sits in the git value, where install writes it (s1/s3, minus the
// Oracle's `ref:` key).
func TestInstall_DeclaredRef_WithSkill(t *testing.T) {
	chdirTemp(t)
	repo := gitSkillRepo(t, t.TempDir(), "skill-repo", map[string][]string{"a": {"SKILL.md"}})
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &fixtureLoader{modulesDir: "apm_modules", fixtureDir: repo}}
	if err := os.WriteFile("apm.yml", []byte(declaredRefHead+"dependencies:\n  apm:\n    - git: acme/foo#v1\n      skills:\n        - a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var err error
	out := captureInstallCombined(t, func() {
		err = runInstall(deps, false, true, "claude", "", []string{"a"}, []string{"acme/foo#v2"})
	})
	if err != nil {
		t.Fatalf("runInstall: %v\n%s", err, out)
	}
	if !strings.Contains(out, " + acme/foo#v2 (updated ref in apm.yml)\n") {
		t.Errorf("missing the updated line:\n%s", out)
	}
	want := declaredRefHead + "dependencies:\n  apm:\n    - git: acme/foo#v2\n      skills:\n        - a\n"
	if got := readFileT(t, "apm.yml"); got != want {
		t.Errorf("apm.yml:\n%s\nwant:\n%s", got, want)
	}
	if got := declaredRefLockRef(t, "apm.lock.yaml", "acme/foo"); got != "v2" {
		t.Errorf("lock resolved_ref = %q, want v2", got)
	}
}

// Two refs to one declared identity in one call: the first sets the ref, the
// second is "(already in apm.yml)" and changes nothing (s9).
func TestInstall_DeclaredRef_SameIdentityTwiceInOneCall(t *testing.T) {
	declaredRefProject(t, "dependencies:\n  apm:\n    - Acme/Foo#v1\n")
	out := declaredRefInstall(t, nil, "acme/foo#v2", "acme/foo#v3")
	if !strings.Contains(out, " + acme/foo#v2 (updated ref in apm.yml)\n") ||
		!strings.Contains(out, " + acme/foo#v3 (already in apm.yml)\n") {
		t.Errorf("missing lines:\n%s", out)
	}
	if strings.Contains(out, "conflicts with already-declared") {
		t.Errorf("no warning is printed:\n%s", out)
	}
	if got := readFileT(t, "apm.yml"); got != declaredRefHead+"dependencies:\n  apm:\n    - Acme/Foo#v2\n" {
		t.Errorf("apm.yml:\n%s", got)
	}
}

// A frozen install writes no apm.yml and keeps the declared ref with the
// warning it printed before issue #156 (issue #156 brief, item 5).
func TestInstall_DeclaredRef_FrozenKeepsDeclaredRef(t *testing.T) {
	declaredRefProject(t, "dependencies:\n  apm:\n    - acme/foo#v1\n")
	declaredRefInstall(t, nil, "acme/foo#v1")
	yml, lock := readFileT(t, "apm.yml"), readFileT(t, "apm.lock.yaml")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	var err error
	out := captureInstallCombined(t, func() {
		err = runInstall(deps, true, true, "claude", "", nil, []string{"acme/foo#v2"})
	})
	if err != nil {
		t.Fatalf("frozen install: %v\n%s", err, out)
	}
	if !strings.Contains(out, `ref "v2" conflicts with already-declared "acme/foo" (acme/foo#v1) -- keeping the first-declared ref`) {
		t.Errorf("missing the warning:\n%s", out)
	}
	if strings.Contains(out, "updated ref in apm.yml") {
		t.Errorf("a frozen install rewrites nothing:\n%s", out)
	}
	if readFileT(t, "apm.yml") != yml || readFileT(t, "apm.lock.yaml") != lock {
		t.Error("apm.yml or the lock changed")
	}
}

// Whenever runInstall changes the in-memory ref of a declared entry
// (declaredRefRewritable), the entry setEntryRef writes must read back with
// that ref, so apm.yml and the lock agree.
func TestSetEntryRef_ReadsBackForEveryRewritableForm(t *testing.T) {
	forms := []string{
		"- acme/foo#v1\n",
		"- acme/foo\n",
		"- https://github.com/acme/foo.git#v1\n",
		"- git: acme/foo\n",
		"- git: acme/foo#v1\n",
		"- git: acme/foo\n  ref: v1\n",
		"- git: acme/foo\n  ref: v1\n  alias: ghs\n  targets: [claude]\n",
		"- git: acme/mono\n  path: sub/pkg\n  ref: v1\n",
		"- git: http://example.com/acme/foo.git\n  ref: v1\n  allow_insecure: true\n",
		"- name: acme/foo#v1\n",
		"- name: acme/foo\n  alias: ghs\n",
		"- name: acme/foo#v1\n  path: ignored\n",
		// ParseDepDict reads only scalar values, so these `ref:` are no ref.
		"- git: acme/foo\n  ref: []\n",
		"- git: acme/foo\n  ref: {}\n",
		"- git: acme/foo\n  ref:\n",
		"- git: acme/foo#v1\n  ref: []\n",
		"- git: acme/foo#v1\n  ref: {}\n",
		"- git: acme/foo#v1\n  ref:\n",
		"- git: acme/foo\n  ref: [a, b]\n  alias: ghs\n",
	}
	for _, form := range forms {
		for _, newRef := range []string{"v2", ""} {
			doc, err := yamlcore.SafeLoad([]byte(form))
			if err != nil {
				t.Fatalf("%q: %v", form, err)
			}
			seq := doc
			if seq.Kind == yamllib.DocumentNode && len(seq.Content) > 0 {
				seq = seq.Content[0]
			}
			entry := seq.Content[0]
			before := parseEntryRef(t, entry)
			if !declaredRefRewritable(before) {
				continue
			}
			setEntryRef(seq, 0, newRef)
			if got := parseEntryRef(t, seq.Content[0]); got.Reference != newRef {
				t.Errorf("%q with ref %q reads back %q", form, newRef, got.Reference)
			}
			out, err := yamlcore.SafeDump(doc)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(out), "!!") {
				t.Errorf("%q with ref %q leaves a tag in:\n%s", form, newRef, out)
			}
			if m := seq.Content[0]; m.Kind == yamllib.MappingNode {
				for j := 0; j < len(m.Content)-1; j += 2 {
					if m.Content[j].Value == "ref" && m.Content[j+1].Kind == yamllib.SequenceNode && newRef != "" && !strings.Contains(form, "#") {
						t.Errorf("%q with ref %q keeps a non-scalar ref: node", form, newRef)
					}
				}
			}
		}
	}
}

func parseEntryRef(t *testing.T, entry *yamllib.Node) *manifest.DependencyReference {
	t.Helper()
	var d *manifest.DependencyReference
	var err error
	if entry.Kind == yamllib.ScalarNode {
		d, err = manifest.ParseDepString(entry.Value)
	} else {
		d, err = manifest.ParseDepDict(entry, 0)
	}
	if err != nil {
		t.Fatal(err)
	}
	normalizeLocalDep(d)
	return d
}
