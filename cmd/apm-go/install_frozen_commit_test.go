package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

type noFetchLoader struct{ t *testing.T }

func (l noFetchLoader) LoadPackage(ref *manifest.DependencyReference, _ string) (*manifest.Manifest, error) {
	l.t.Errorf("frozen install fetched %s before refusing the lock", ref.RepoURL)
	return &manifest.Manifest{}, nil
}

const frozenTagDep = `- repo_url: o/tag
  host: 127.0.0.1
  resolved_commit: c6944b8598336786a113c64c5c988b6067e4212d
  resolved_ref: 6dd92d3
  version: 1.0.0
  deployed_files:
  - .claude/agents/ag.md
  deployed_file_hashes:
    .claude/agents/ag.md: sha256:7812231f3b200e5b6c400b01fa70d43ef480f74aa30f3fd012920c132433d978
  source: git
  constraint: 6dd92d3
  tree_sha256: sha256:4e7bbeec2f597d285ce8464db427b79558c34f37952d701e8829f71cdee3a26c
`

// The expected text is Oracle 0.33.0's reason (issue #124 investigation,
// r1-or/tag/fz.out and r2-or/trtag/fz.out) with 'apm' as 'apm-go', behind the
// prefix of apm-go's frozen errors.
func TestFrozenInstall_RefusesTagNamedLikeCommit(t *testing.T) {
	tests := []struct {
		name, yml, lock, want string
	}{
		{
			name: "direct dependency",
			yml:  "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - git: https://127.0.0.1/o/tag.git\n      ref: 6dd92d3\n",
			lock: "lockfile_version: '1'\ndependencies:\n" + frozenTagDep,
			want: "frozen install: Cannot resolve 127.0.0.1/o/tag: incompatible immutable requirements.\n" +
				"  apm.lock.yaml -> 127.0.0.1/o/tag@6dd92d3 (commit c6944b8598336786a113c64c5c988b6067e4212d)\n" +
				"  proj -> 127.0.0.1/o/tag@6dd92d3 (commit 6dd92d3)\n" +
				"Align the requested refs in the root or parent manifests to the same commit, then run 'apm-go install' without --frozen to regenerate apm.lock.yaml.",
		},
		{
			name: "transitive dependency",
			yml:  "name: proj\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  apm:\n    - git: https://127.0.0.1/o/ptag.git\n      ref: main\n",
			lock: "lockfile_version: '1'\ndependencies:\n" +
				"- repo_url: o/ptag\n  host: 127.0.0.1\n  resolved_commit: 998b3e5f7d42cd29f2a1cb0a27b2ccaffc49b048\n  resolved_ref: main\n  version: 1.0.0\n" +
				"  deployed_files:\n  - .claude/agents/pa.md\n  deployed_file_hashes:\n    .claude/agents/pa.md: sha256:6ab5e0d5e2bcf721e50b968d02e7289dcedcbf564e0294ab3c231940e5f83ff8\n" +
				"  source: git\n  constraint: main\n  tree_sha256: sha256:69c0816bf5e8127918e1c7990445d9d799a2f6f2f9a9dc5bf9b7ea93b9b49e6e\n" +
				"- repo_url: o/tag\n  host: 127.0.0.1\n  resolved_commit: c6944b8598336786a113c64c5c988b6067e4212d\n  resolved_ref: 6dd92d3\n  version: 1.0.0\n  depth: 2\n  resolved_by: o/ptag\n" +
				"  source: git\n  constraint: 6dd92d3\n",
			want: "frozen install: Cannot resolve 127.0.0.1/o/tag: incompatible immutable requirements.\n" +
				"  apm.lock.yaml -> 127.0.0.1/o/tag@6dd92d3 (commit c6944b8598336786a113c64c5c988b6067e4212d)\n" +
				"  proj -> 127.0.0.1/o/ptag@main -> 127.0.0.1/o/tag@6dd92d3 (commit 6dd92d3)\n" +
				"Align the requested refs in the root or parent manifests to the same commit, then run 'apm-go install' without --frozen to regenerate apm.lock.yaml.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			project := chdirTemp(t)
			writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), tt.yml)
			writeGlobalStaleFile(t, filepath.Join(project, "apm.lock.yaml"), tt.lock)
			deps := &installDeps{tags: &mockInstallTagLister{}, loader: noFetchLoader{t}}

			var err error
			captureUninstallStdout(t, func() { err = runInstall(deps, true, true, "", "", nil, nil) })

			if err == nil || err.Error() != tt.want || exitCodeOf(err) != 1 {
				t.Fatalf("frozen install: error %v (exit %d)\nwant %s (exit 1)", err, exitCodeOf(err), tt.want)
			}
			entries, _ := os.ReadDir(project)
			if len(entries) != 2 {
				t.Errorf("frozen install wrote files: %v", entries)
			}
			if got, _ := os.ReadFile(filepath.Join(project, "apm.lock.yaml")); string(got) != tt.lock {
				t.Errorf("apm.lock.yaml changed:\n%s", got)
			}
		})
	}
}

// A commit ref locked to a commit it starts with passes, in either case
// (Oracle 0.33.0, scenarios short7 and upper: --frozen exits 0).
func TestCheckFrozenCommitPins_PassesCommitThatStartsWithRef(t *testing.T) {
	for _, ref := range []string{"1e5759f", "1E5759F", "1e5759f30f2f", "1e5759f30f2f4778a527ebd254bc8fb44d2d7c7e", "main", "v1.0.0"} {
		node, err := yamlcore.SafeLoad([]byte("lockfile_version: '1'\ndependencies:\n- repo_url: o/s\n  host: 127.0.0.1\n" +
			"  resolved_commit: 1e5759f30f2f4778a527ebd254bc8fb44d2d7c7e\n  resolved_ref: '" + ref + "'\n  source: git\n  constraint: '" + ref + "'\n"))
		if err != nil {
			t.Fatal(err)
		}
		lock, err := lockfile.ParseLockfile(node)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkFrozenCommitPins("proj", lock); err != nil {
			t.Errorf("ref %s: %v", ref, err)
		}
	}
}
