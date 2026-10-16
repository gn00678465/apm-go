package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/deps holds Oracle 0.33.0's stdout for the same files, created in
// the same order, with apm-go's status symbols and without the Oracle's
// "Run 'apm prune'" line. deps list keeps the order the file system lists a
// directory in, which for these small directories is the order of creation.
var depsTestdata, _ = filepath.Abs(filepath.Join("testdata", "deps"))

func depsGolden(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(depsTestdata, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// requireCreationOrder skips a test whose expected rows follow the order of
// creation when the temporary directory's file system lists a directory in
// another order (tmpfs lists the newest entry first). The Oracle reads the
// same order from the same file system, so only the fixture's assumption
// fails there.
func requireCreationOrder(t *testing.T, root string) {
	t.Helper()
	probe := filepath.Join(root, ".order-probe")
	for _, name := range []string{"b", "a"} {
		if err := os.MkdirAll(filepath.Join(probe, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	entries := readDirInOrder(probe)
	if len(entries) != 2 || entries[0].Name() != "b" {
		t.Skipf("%s does not list a directory in creation order", root)
	}
	if err := os.RemoveAll(probe); err != nil {
		t.Fatal(err)
	}
}

// depsWriteFiles writes files in the order given, path then content.
func depsWriteFiles(t *testing.T, root string, files ...string) {
	t.Helper()
	for i := 0; i+1 < len(files); i += 2 {
		rel, content := files[i], files[i+1]
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// runDeps runs `apm-go <args>` in proj with HOME set to home and returns
// stdout.
func runDeps(t *testing.T, proj, home string, args ...string) (string, error) {
	t.Helper()
	t.Chdir(proj)
	t.Setenv("HOME", home)
	t.Setenv("COLUMNS", "")
	t.Setenv("TTY_FORCE", "")
	var err error
	out := captureStdout(t, func() {
		root := buildRootCmd()
		root.SetArgs(args)
		_, err = root.ExecuteC()
	})
	return out, err
}

// depsOrphanProject is a project whose apm_modules holds its local package, a
// package with only SKILL.md that nothing declares, and three apm.yml files
// the Oracle refuses (no version, broken YAML, empty version).
func depsOrphanProject(t *testing.T) (proj, home string) {
	root := t.TempDir()
	requireCreationOrder(t, root)
	proj, home = filepath.Join(root, "proj"), filepath.Join(root, "home")
	depsWriteFiles(t, proj,
		"apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./dep\n",
		"apm_modules/_local/dep/apm.yml", "name: dep\nversion: 2.3.4\n",
		"apm_modules/_local/dep/.apm/agents/a.agent.md", "x\n",
		"apm_modules/_local/dep/.apm/skills/sk1/SKILL.md", "x\n",
		"apm_modules/_local/dep/.apm/prompts/p.prompt.md", "x\n",
		"apm_modules/_local/dep/.apm/instructions/i.instructions.md", "x\n",
		"apm_modules/_local/dep/hooks/h.json", "{}\n",
		"apm_modules/acme/noversion/apm.yml", "name: nov\n",
		"apm_modules/acme/badyaml/apm.yml", "name: [x\n",
		"apm_modules/acme/emptyver/apm.yml", "name: ev\nversion: \"\"\n",
		"apm_modules/acme/skillonly/SKILL.md", "x\n",
	)
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	return proj, home
}

func TestDepsList_OrphanAndFailedInspect(t *testing.T) {
	proj, home := depsOrphanProject(t)
	out, err := runDeps(t, proj, home, "deps", "list")
	if err != nil {
		t.Fatalf("deps list: %v", err)
	}
	want := depsGolden(t, "orphan-project.stdout")
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

func TestDepsList_SkillsProject(t *testing.T) {
	proj, home := depsOrphanProject(t)
	out, err := runDeps(t, proj, home, "deps", "list", "--skills")
	if err != nil {
		t.Fatalf("deps list --skills: %v", err)
	}
	want := strings.Replace(depsGolden(t, "orphan-project.stdout"), " ! 1 orphaned",
		" i Skills by package:\n  - _local/dep: sk1\n  - acme/skillonly: skillonly\n ! 1 orphaned", 1)
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// globalHome is ~/.apm with acme/pkg declared and its transitive acme/child,
// which an Oracle lock marks is_insecure.
func globalHome(t *testing.T) (proj, home string) {
	root := t.TempDir()
	proj, home = filepath.Join(root, "proj"), filepath.Join(root, "home")
	depsWriteFiles(t, home,
		".apm/apm.yml", "name: home\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/pkg\n",
		".apm/apm.lock.yaml", "lockfile_version: '1'\ndependencies:\n"+
			"- repo_url: acme/pkg\n  resolved_commit: abc1234def5678abc1234def5678abc1234def56\n"+
			"- repo_url: acme/child\n  resolved_commit: fed9876cba5432fed9876cba5432fed9876cba54\n  depth: 2\n  resolved_by: acme/pkg\n  is_insecure: true\n",
		".apm/apm_modules/acme/pkg/apm.yml", "name: pkg\nversion: 1.2.0\n",
		".apm/apm_modules/acme/pkg/.apm/agents/a.agent.md", "x\n",
		".apm/apm_modules/acme/pkg/.apm/prompts/p.prompt.md", "x\n",
		".apm/apm_modules/acme/pkg/.apm/skills/s1/SKILL.md", "x\n",
		".apm/apm_modules/acme/child/SKILL.md", "x\n",
	)
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	return proj, home
}

const noProjectDeps = " i No APM dependencies installed (Project scope)\n"

func TestDepsList_Global(t *testing.T) {
	skills := " i Skills by package:\n  - acme/pkg: s1\n  - acme/child: child\n"
	for _, tc := range []struct {
		name, golden, extra string
		args                []string
	}{
		{"global", "global.stdout", "", []string{"-g"}},
		{"all wins over global", "global-all.stdout", "", []string{"-g", "--all"}},
		{"insecure origin", "global-insecure.stdout", "", []string{"-g", "--insecure"}},
		{"all insecure", "global-all-insecure.stdout", "", []string{"--all", "--insecure"}},
		{"global skills", "global.stdout", skills, []string{"-g", "--skills"}},
		{"all skills", "global-all.stdout", skills, []string{"--all", "--skills"}},
		{"insecure skills", "global-insecure.stdout", " i Skills by package:\n  - acme/child: child\n", []string{"-g", "--insecure", "--skills"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := depsGolden(t, tc.golden) + tc.extra
			proj, home := globalHome(t)
			if !strings.Contains(tc.golden, "insecure") {
				requireCreationOrder(t, filepath.Dir(home))
			}
			out, err := runDeps(t, proj, home, append([]string{"deps", "list"}, tc.args...)...)
			if err != nil {
				t.Fatalf("deps list: %v", err)
			}
			if out != want {
				t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

func TestDepsTree_Global(t *testing.T) {
	proj, home := globalHome(t)
	out, err := runDeps(t, proj, home, "deps", "tree", "-g")
	if err != nil {
		t.Fatalf("deps tree -g: %v", err)
	}
	want := depsGolden(t, "global-tree.stdout")
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

func TestDepsTree_Unresolved(t *testing.T) {
	root := t.TempDir()
	proj, home := filepath.Join(root, "proj"), filepath.Join(root, "home")
	depsWriteFiles(t, proj,
		"apm.yml", "name: tproj\nversion: 1.0.0\n",
		"apm.lock.yaml", `lockfile_version: '1'
dependencies:
- repo_url: a/one
  version: 3.0.0
  resolved_commit: aaaa111bbbbccccddddeeeeffff0000111122223
- repo_url: x/dup
  virtual_path: p1
  is_virtual: true
  resolved_ref: main
- repo_url: x/dup
  virtual_path: p2
  is_virtual: true
- repo_url: b/two
  depth: 2
  resolved_by: nobody/here
  resolved_commit: bbbb222ccccddddeeeeffff00001111222233334
- repo_url: c/three
  depth: 2
  resolved_by: x/dup
- repo_url: d/four
  depth: 3
  resolved_by: b/two
- repo_url: e/five
  depth: 2
  resolved_by: a/one
- repo_url: f/six
  depth: 3
  resolved_by: e/five
`,
	)
	out, err := runDeps(t, proj, home, "deps", "tree")
	if err != nil {
		t.Fatalf("deps tree: %v", err)
	}
	want := depsGolden(t, "unresolved-tree.stdout")
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// An absolute local_path replaces apm_modules in Python's apm_modules / key,
// so the Oracle summarizes the source directory; a relative one names no
// directory and gets no summary.
func TestDepsTree_LocalDependencySummary(t *testing.T) {
	root := t.TempDir()
	proj, home := filepath.Join(root, "proj"), filepath.Join(root, "home")
	src := filepath.Join(root, "src", "absdep")
	depsWriteFiles(t, src,
		"apm.yml", "name: absdep\nversion: 0.1.0\n",
		".apm/agents/a.agent.md", "x\n",
		"SKILL.md", "x\n",
	)
	depsWriteFiles(t, proj,
		"apm.yml", "name: lproj\nversion: 1.0.0\n",
		"apm.lock.yaml", "lockfile_version: '1'\ndependencies:\n"+
			"- repo_url: _local/absdep\n  source: local\n  local_path: "+src+"\n  version: 0.1.0\n"+
			"- repo_url: _local/reldep\n  source: local\n  local_path: ./reldep\n  version: 0.2.0\n",
		"apm_modules/_local/reldep/apm.yml", "name: reldep\nversion: 0.2.0\n",
		"apm_modules/_local/reldep/.apm/agents/a.agent.md", "x\n",
	)
	out, err := runDeps(t, proj, home, "deps", "tree")
	if err != nil {
		t.Fatalf("deps tree: %v", err)
	}
	want := depsGolden(t, "local-tree.stdout")
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}

// Without a lock the tree comes from a scan of apm_modules: name@version from
// each apm.yml, @error for one that is not YAML, @unknown without a version.
func TestDepsTree_ScanWithoutLock(t *testing.T) {
	root := t.TempDir()
	requireCreationOrder(t, root)
	proj, home := filepath.Join(root, "proj"), filepath.Join(root, "home")
	depsWriteFiles(t, proj,
		"apm_modules/acme/good/apm.yml", "name: goodname\nversion: 1.0.0\n",
		"apm_modules/acme/good/.apm/prompts/p.prompt.md", "x\n",
		"apm_modules/acme/bad/apm.yml", "name: [x\n",
		"apm_modules/acme/nov/apm.yml", "name: novname\n",
		"apm_modules/acme/sk/SKILL.md", "x\n",
		"apm_modules/.hidden/pkg/apm.yml", "name: h\nversion: 1.0.0\n",
		"apm_modules/acme-b/z/apm.yml", "name: z\nversion: 9.9.9\n",
	)
	out, err := runDeps(t, proj, home, "deps", "tree")
	if err != nil {
		t.Fatalf("deps tree: %v", err)
	}
	want := depsGolden(t, "scan-tree.stdout")
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}

	out, err = runDeps(t, proj, home, "deps", "list")
	if err != nil {
		t.Fatalf("deps list: %v", err)
	}
	wantList := depsGolden(t, "scan-project.stdout")
	if out != wantList {
		t.Errorf("deps list stdout:\n%s\nwant:\n%s", out, wantList)
	}
}

// deps reads and never writes: with no ~/.apm, -g does not create it.
func TestDeps_GlobalWritesNothing(t *testing.T) {
	root := t.TempDir()
	proj, home := filepath.Join(root, "proj"), filepath.Join(root, "home")
	for _, d := range []string{proj, home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"deps", "list", "-g"}, {"deps", "list", "--all"}, {"deps", "tree", "-g"}} {
		out, err := runDeps(t, proj, home, args...)
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		want := map[string]string{
			"list": " i No APM dependencies installed (Global scope)\n",
			"tree": "my-project (local)\n└── No dependencies installed\n",
		}[args[1]]
		if args[2] == "--all" {
			want = noProjectDeps + "\n" + want
		}
		if out != want {
			t.Errorf("%v stdout:\n%s\nwant:\n%s", args, out, want)
		}
	}
	for _, d := range []string{proj, home} {
		entries, err := os.ReadDir(d)
		if err != nil || len(entries) != 0 {
			t.Errorf("%s: entries %v, err %v; want an empty directory", d, entries, err)
		}
	}
}

func TestDeps_NoSubcommandIsUsageError(t *testing.T) {
	root := t.TempDir()
	_, err := runDeps(t, root, root, "deps")
	if err == nil || exitCodeOf(err) != 2 {
		t.Fatalf("deps: err %v, exit %d; want exit 2", err, exitCodeOf(err))
	}
	_, err = runDeps(t, root, root, "deps", "nope")
	if err == nil || exitCodeOf(err) != 2 || !strings.Contains(err.Error(), "No such command 'nope'.") {
		t.Fatalf("deps nope: err %v; want the usage error No such command 'nope'.", err)
	}
}
