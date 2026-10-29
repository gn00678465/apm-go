package main

import (
	"path/filepath"
	"testing"
)

// The Oracle places some packages where apm-go's install never puts them: an
// alias at apm_modules/<alias>, a local package another local package
// declares at _local/<12 hex of sha256(anchored_local_path)>/<name> (the
// slots below are those of /src/a/b and /src/c/b), and a virtual file package
// at <owner>/<repo>-<file name>. The expected output is Oracle 0.33.0's stdout
// for the same files.
const depsLock = "lockfile_version: '1'\ndependencies:\n"

var depsLayoutCases = []struct {
	name   string
	global bool
	files  []string
}{
	{name: "alias-yml", files: []string{
		"apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/pkg\n      alias: custom\n",
		"apm_modules/custom/apm.yml", "name: pkg\nversion: 1.0.0\n",
		"apm_modules/custom/SKILL.md", "x\n",
	}},
	{name: "alias-lock-global", global: true, files: []string{
		"apm.yml", "name: global\nversion: 1.0.0\n",
		"apm.lock.yaml", depsLock + "- repo_url: acme/pkg\n  alias: custom\n  host: gitlab.com\n  resolved_commit: abc1234def5678abc1234def5678abc1234def56\n",
		"apm_modules/custom/apm.yml", "name: pkg\nversion: 2.0.0\n",
		"apm_modules/custom/SKILL.md", "x\n",
	}},
	{name: "anchored", files: []string{
		"apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - ../a\n",
		"apm.lock.yaml", depsLock +
			"- repo_url: _local/a\n  version: 2.0.0\n  source: local\n  local_path: ../a\n" +
			"- repo_url: _local/b\n  version: 3.0.0\n  depth: 2\n  resolved_by: _local/a\n  source: local\n  local_path: ../b\n  declaring_parent: ../a\n  anchored_local_path: /src/a/b\n",
		"apm_modules/_local/a/apm.yml", "name: a\nversion: 2.0.0\n",
		"apm_modules/_local/a/.apm/agents/aa.agent.md", "x\n",
		"apm_modules/_local/7ad1eaec1405/b/apm.yml", "name: b\nversion: 3.0.0\n",
		"apm_modules/_local/7ad1eaec1405/b/.apm/instructions/bb.instructions.md", "x\n",
	}},
	{name: "anchored-global", global: true, files: []string{
		"apm.yml", "name: global\nversion: 1.0.0\ndependencies:\n  apm:\n    - /src/a\n    - /src/c\n",
		"apm.lock.yaml", depsLock +
			"- repo_url: _local/a\n  version: 1.0.0\n  source: local\n  local_path: /src/a\n" +
			"- repo_url: _local/c\n  version: 1.0.0\n  source: local\n  local_path: /src/c\n" +
			"- repo_url: _local/b\n  version: 0.1.0\n  depth: 2\n  resolved_by: _local/a\n  source: local\n  local_path: ./b\n  declaring_parent: /src/a\n  anchored_local_path: /src/a/b\n" +
			"- repo_url: _local/b\n  version: 0.2.0\n  depth: 2\n  resolved_by: _local/c\n  source: local\n  local_path: ./b\n  declaring_parent: /src/c\n  anchored_local_path: /src/c/b\n",
		"apm_modules/_local/a/apm.yml", "name: a\nversion: 1.0.0\n",
		"apm_modules/_local/c/apm.yml", "name: c\nversion: 1.0.0\n",
		"apm_modules/_local/7ad1eaec1405/b/apm.yml", "name: b-a\nversion: 0.1.0\n",
		"apm_modules/_local/7ad1eaec1405/b/.apm/agents/ba.agent.md", "x\n",
		"apm_modules/_local/6e43eb0fe867/b/apm.yml", "name: b-c\nversion: 0.2.0\n",
	}},
	{name: "vfile", files: []string{
		"apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/vf/prompts/x.prompt.md\n",
		"apm.lock.yaml", depsLock + "- repo_url: acme/vf\n  virtual_path: prompts/x.prompt.md\n  is_virtual: true\n  resolved_commit: abc1234def5678abc1234def5678abc1234def56\n  version: 1.0.0\n",
		"apm_modules/acme/vf-x/apm.yml", "name: vf-x\nversion: 1.0.0\ndescription: x\n",
		"apm_modules/acme/vf-x/.apm/prompts/x.prompt.md", "---\ndescription: x\n---\nhello\n",
	}},
	{name: "vfile-global", global: true, files: []string{
		"apm.yml", "name: global\nversion: 1.0.0\ndependencies:\n  apm:\n    - acme/vf/prompts/x.prompt.md\n",
		"apm_modules/acme/vf-x/apm.yml", "name: vf-x\nversion: 1.0.0\ndescription: x\n",
		"apm_modules/acme/vf-x/.apm/prompts/x.prompt.md", "---\ndescription: x\n---\nhello\n",
	}},
}

func TestDeps_OracleLayout(t *testing.T) {
	for _, tc := range depsLayoutCases {
		for _, sub := range []string{"list", "tree"} {
			t.Run(tc.name+"/"+sub, func(t *testing.T) {
				root := t.TempDir()
				proj, home := filepath.Join(root, "proj"), filepath.Join(root, "home")
				scope, args := proj, []string{"deps", sub}
				if tc.global {
					scope, args = filepath.Join(home, ".apm"), append(args, "-g")
				}
				depsWriteFiles(t, scope, tc.files...)
				depsWriteFiles(t, root, "proj/.keep", "", "home/.keep", "")
				out, err := runDeps(t, proj, home, args...)
				if err != nil {
					t.Fatalf("%v: %v", args, err)
				}
				want := depsGolden(t, tc.name+"-"+sub+".stdout")
				if sub == "list" {
					out, want = normalizeDepsList(out), normalizeDepsList(want)
				}
				if out != want {
					t.Errorf("%v stdout:\n%s\nwant (Oracle 0.33.0):\n%s", args, out, want)
				}
			})
		}
	}
}

// An alias package's root SKILL.md deploys under the alias, the last segment
// of its directory: Oracle 0.33.0 installed {git: .../acme/skill.git, alias:
// custom} to .claude/skills/custom.
func TestDepsList_SkillsAlias(t *testing.T) {
	root := t.TempDir()
	proj := filepath.Join(root, "proj")
	depsWriteFiles(t, proj, depsLayoutCases[0].files...)
	out, err := runDeps(t, proj, filepath.Join(root, "home"), "deps", "list", "--skills")
	if err != nil {
		t.Fatal(err)
	}
	want := depsGolden(t, "alias-yml-list.stdout") + " i Skills by package:\n  - acme/pkg: custom\n"
	if out != want {
		t.Errorf("stdout:\n%s\nwant:\n%s", out, want)
	}
}
