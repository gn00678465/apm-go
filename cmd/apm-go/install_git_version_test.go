package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/semver"
)

// Issue #82: the lock entry of a git dependency records a version, as the
// Oracle's LockedDependency.from_dependency_ref does (deps/lockfile.py:650-656).
// The expected values are the Oracle's: a git semver resolution renders the
// tag's version (deps/git_semver_resolver.py:324-331); otherwise the version
// of the package the resolver left in apm_modules, as CachedDependencySource
// reads it (install/sources.py:436-472), measured with Oracle 0.33.0 for an
// apm package, a root SKILL.md, a skill bundle and a Claude plugin with and
// without a version.

// gitVersionInstall installs one git dependency whose apm_modules/<key>
// already holds files, and returns its lock entry.
func gitVersionInstall(t *testing.T, dep, key string, files map[string]string, tags map[string][]semver.TagInfo) (lockText string, version, name, packageType string) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - "+dep+"\n")
	for rel, content := range files {
		writeGlobalStaleFile(t, filepath.Join(project, "apm_modules", filepath.FromSlash(key), filepath.FromSlash(rel)), content)
	}
	deps := &installDeps{tags: &mockInstallTagLister{tags: tags}, loader: &mockInstallLoader{}}
	var err error
	captureUninstallStdout(t, func() {
		err = runInstall(deps, false, true, "claude", "", nil, nil)
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	entry := subdirLockEntry(t, project, key)
	return globalStaleRead(t, filepath.Join(project, "apm.lock.yaml")), entry.Version, entry.Name, entry.PackageType
}

const gitVersionSkill = "---\nname: s\ndescription: d\n---\nbody\n"

func TestInstall_GitDependency_LockRecordsPackageVersion(t *testing.T) {
	cases := []struct {
		name  string
		dep   string
		key   string
		files map[string]string
		want  string
		// The name and package_type of the Oracle's lock (the comment of
		// gitPackageIdentity names the runs and the lines).
		wantName, wantType string
	}{
		{
			name: "apm package",
			dep:  "acme/pkg", key: "acme/pkg",
			files: map[string]string{
				"apm.yml":                             "name: pkg\nversion: 3.1.4\n",
				".apm/instructions/x.instructions.md": "# x\n",
			},
			want:     "3.1.4",
			wantName: "pkg", wantType: "apm_package",
		},
		{
			name: "dependency-only apm package",
			dep:  "acme/agg", key: "acme/agg",
			files: map[string]string{
				"apm.yml": "name: agg\nversion: 2.2.0\ndependencies:\n  mcp:\n    - name: m\n      registry: false\n      transport: stdio\n      command: m\n",
			},
			want:     "2.2.0",
			wantName: "agg", wantType: "apm_package",
		},
		{
			name: "root SKILL.md only",
			dep:  "acme/skill", key: "acme/skill",
			files:    map[string]string{"SKILL.md": gitVersionSkill},
			want:     "unknown",
			wantName: "s", wantType: "claude_skill",
		},
		{
			name: "root SKILL.md with a metadata apm.yml",
			dep:  "acme/hybrid", key: "acme/hybrid",
			files:    map[string]string{"SKILL.md": gitVersionSkill, "apm.yml": "name: hybrid\nversion: 0.4.0\n"},
			want:     "0.4.0",
			wantName: "hybrid", wantType: "hybrid",
		},
		{
			name: "hook package",
			dep:  "acme/hooks", key: "acme/hooks",
			files:    map[string]string{"hooks/hooks.json": "{}\n"},
			want:     "unknown",
			wantName: "", wantType: "",
		},
		{
			name: "skill bundle",
			dep:  "acme/bundle", key: "acme/bundle",
			files:    map[string]string{"skills/a/SKILL.md": gitVersionSkill},
			want:     "unknown",
			wantName: "bundle", wantType: "skill_bundle",
		},
		{
			name: "skill bundle with a metadata apm.yml",
			dep:  "acme/bundle", key: "acme/bundle",
			files:    map[string]string{"skills/a/SKILL.md": gitVersionSkill, "apm.yml": "name: bundle-name\nversion: 5.0.1\n"},
			want:     "5.0.1",
			wantName: "bundle-name", wantType: "skill_bundle",
		},
		{
			name: "claude plugin with a version",
			dep:  "acme/plugin", key: "acme/plugin",
			files: map[string]string{
				".claude-plugin/plugin.json": `{"name": "plugin", "version": "2.5.0"}`,
				"agents/a.md":                "# a\n",
			},
			want:     "2.5.0",
			wantName: "plugin", wantType: "apm_package",
		},
		{
			name: "claude plugin without a version or a commit",
			dep:  "acme/plugin", key: "acme/plugin",
			files: map[string]string{
				".claude-plugin/plugin.json": `{"name": "plugin"}`,
				"agents/a.md":                "# a\n",
			},
			want:     "0.0.0",
			wantName: "plugin", wantType: "apm_package",
		},
		{
			name: "root plugin.json",
			dep:  "acme/plugin", key: "acme/plugin",
			files:    map[string]string{"plugin.json": `{"name": "plugin", "version": "1.2.0"}`, "agents/a.md": "# a\n"},
			want:     "1.2.0",
			wantName: "plugin", wantType: "apm_package",
		},
		{
			name: "claude plugin without a version and a metadata apm.yml",
			dep:  "acme/plugin", key: "acme/plugin",
			files: map[string]string{
				".claude-plugin/plugin.json": `{"name": "plugin"}`,
				"apm.yml":                    "name: plugin\nversion: 4.0.0\n",
				"agents/a.md":                "# a\n",
			},
			want:     "4.0.0",
			wantName: "plugin", wantType: "apm_package",
		},
		{
			name: "plugin manifest beside an apm package",
			dep:  "acme/dual", key: "acme/dual",
			files: map[string]string{
				".claude-plugin/plugin.json":          `{"name": "dual", "version": "9.0.0"}`,
				"apm.yml":                             "name: dual\nversion: 1.1.0\n",
				".apm/instructions/x.instructions.md": "# x\n",
			},
			want:     "1.1.0",
			wantName: "dual", wantType: "apm_package",
		},
		{
			name: "subdirectory package",
			dep:  "acme/mono/skills/alpha", key: "acme/mono/skills/alpha",
			files:    map[string]string{"SKILL.md": gitVersionSkill},
			want:     "unknown",
			wantName: "s", wantType: "claude_skill",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lock, version, name, packageType := gitVersionInstall(t, tc.dep, tc.key, tc.files, nil)
			if version != tc.want {
				t.Errorf("version = %q, want %q\n%s", version, tc.want, lock)
			}
			if name != tc.wantName || packageType != tc.wantType {
				t.Errorf("name %q package_type %q, want %q %q\n%s", name, packageType, tc.wantName, tc.wantType, lock)
			}
		})
	}
}

// A Claude plugin whose version is missing or 0.0.0 records the first seven
// characters of its commit, as the Oracle's stamp_plugin_version does
// (deps/package_validator.py:261-301; measured: 823f9e1 for commit 823f9e13...).
func TestInstall_GitPluginWithoutVersion_LockRecordsShortCommit(t *testing.T) {
	for name, manifest := range map[string]string{
		"no version":    `{"name": "plugin"}`,
		"version 0.0.0": `{"name": "plugin", "version": "0.0.0"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("APM_CONFIG_DIR", t.TempDir())
			project := chdirTemp(t)
			commit := subdirGitRepo(t, filepath.Join(project, "acme", "plugin"), map[string]string{
				".claude-plugin/plugin.json": manifest,
				"agents/a.md":                "# a\n",
			})
			writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - git: acme/plugin\n")
			if err := subdirInstall(t, subdirPathLoader{subdirRealLoader()}, false); err != nil {
				t.Fatalf("install: %v", err)
			}
			entry := subdirLockEntry(t, project, "acme/plugin")
			if entry.ResolvedCommit != commit {
				t.Fatalf("resolved_commit = %q, want %q", entry.ResolvedCommit, commit)
			}
			if entry.Version != commit[:7] {
				t.Errorf("version = %q, want %q", entry.Version, commit[:7])
			}
		})
	}
}

func TestInstall_GitSemverDependency_LockRecordsTagVersion(t *testing.T) {
	cases := []struct {
		name string
		dep  string
		tags []semver.TagInfo
		want string
	}{
		{"v-prefixed tag", "acme/foo#^1.0.0", []semver.TagInfo{{Name: "v1.0.0"}, {Name: "v1.5.0"}}, "1.5.0"},
		{"prerelease and build", "acme/foo#^2.0.0-rc.1", []semver.TagInfo{{Name: "v2.0.0-rc.1+build.7"}}, "2.0.0-rc.1+build.7"},
		{"bare tag", "acme/foo#~1.2.0", []semver.TagInfo{{Name: "1.2.9"}}, "1.2.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{
				"apm.yml":                             "name: foo\nversion: 9.9.9\n",
				".apm/instructions/x.instructions.md": "# x\n",
			}
			lock, version, _, _ := gitVersionInstall(t, tc.dep, "acme/foo", files, map[string][]semver.TagInfo{"acme/foo": tc.tags})
			if version != tc.want {
				t.Errorf("version = %q, want %q\n%s", version, tc.want, lock)
			}
		})
	}
}

// A lock written before issue #82 has no version on a git entry: the next
// install writes it once, and the install after that changes nothing.
func TestInstall_GitDependency_OldLockGainsVersionOnce(t *testing.T) {
	files := map[string]string{
		"apm.yml":                             "name: pkg\nversion: 3.1.4\n",
		".apm/instructions/x.instructions.md": "# x\n",
	}
	lock, _, _, _ := gitVersionInstall(t, "acme/pkg", "acme/pkg", files, nil)
	old := strings.Replace(lock, "  version: 3.1.4\n", "", 1)
	if old == lock {
		t.Fatalf("lock has no version line to remove:\n%s", lock)
	}
	if err := os.WriteFile("apm.lock.yaml", []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	install := func(what string) {
		t.Helper()
		captureUninstallStdout(t, func() {
			if err := runInstall(deps, false, true, "claude", "", nil, nil); err != nil {
				t.Fatalf("%s: %v", what, err)
			}
		})
	}

	install("install over the old lock")
	if got := globalStaleRead(t, "apm.lock.yaml"); got != lock {
		t.Errorf("install over the old lock wrote:\n%s\nwant:\n%s", got, lock)
	}

	past := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes("apm.lock.yaml", past, past); err != nil {
		t.Fatal(err)
	}
	install("second install")
	info, err := os.Stat("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(past) {
		t.Errorf("second install wrote the lock again:\n%s", globalStaleRead(t, "apm.lock.yaml"))
	}
}

// A local package keeps what it recorded before issue #82.
func TestInstall_LocalDependency_LockVersionUnchanged(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: caller\nversion: 0.1.0\ndependencies:\n  apm:\n    - ./dep\n")
	writeGlobalStaleFile(t, filepath.Join(project, "dep", "apm.yml"), "name: dep\nversion: 2.0.0\n")
	writeGlobalStaleFile(t, filepath.Join(project, "dep", ".apm", "instructions", "x.instructions.md"), "# x\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: subdirRealLoader()}
	var err error
	captureUninstallStdout(t, func() {
		err = runInstall(deps, false, true, "claude", "", nil, nil)
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	entry := subdirLockEntry(t, project, "_local/dep")
	if entry.Version != "2.0.0" || entry.Name != "dep" || entry.PackageType != "apm_package" {
		t.Errorf("local entry version/name/package_type = %q/%q/%q, want 2.0.0/dep/apm_package",
			entry.Version, entry.Name, entry.PackageType)
	}
}

// A registry package records the version the registry resolved, not the one
// its own apm.yml carries.
func TestInstall_RegistryDependency_LockVersionUnchanged(t *testing.T) {
	enableRegistries(t)
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range map[string]string{
		"apm.yml":                    "name: sample\nversion: 7.7.7\n",
		".apm/skills/probe/SKILL.md": gitVersionSkill,
	} {
		if err := tw.WriteHeader(&tar.Header{Name: name, Size: int64(len(data)), Mode: 0o644}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	sum := sha256.Sum256(buf.Bytes())
	srv := startE2EServer(t, buf.Bytes(), "sha256:"+hex.EncodeToString(sum[:]))

	project := chdirTemp(t)
	writeGlobalStaleFile(t, filepath.Join(project, "apm.yml"), "name: consumer\nversion: 0.1.0\nregistries:\n  local:\n    url: "+srv.URL+
		"\n    insecure: true\n  default: local\ndependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}
	var err error
	captureUninstallStdout(t, func() {
		err = runInstall(deps, false, true, "claude", "", nil, nil)
	})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	entry := subdirLockEntry(t, project, "acme/sample")
	if entry.Version != "1.0.0" || entry.Name != "" || entry.PackageType != "" {
		t.Errorf("registry entry version/name/package_type = %q/%q/%q, want 1.0.0//",
			entry.Version, entry.Name, entry.PackageType)
	}
}
