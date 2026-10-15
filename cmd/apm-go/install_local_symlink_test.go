package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// writeSymlinkAgentDep writes the local package dep with agents/oracle.agent.md
// and the project apm.yml that declares it; it skips the test where symlinks
// cannot be made.
func writeSymlinkAgentDep(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(chdirTemp(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("dep", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("dep", ".apm"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"dep/apm.yml":                "name: dep\nversion: 1.0.0\n",
		"dep/agents/oracle.agent.md": "---\nname: oracle\ndescription: d\n---\nagent body\n",
		"apm.yml":                    "name: e\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./dep\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.FromSlash(name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("x", "probe"); err != nil {
		t.Skipf("symlink unsupported on this platform: %v", err)
	}
	if err := os.Remove("probe"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func installClaude(t *testing.T) error {
	t.Helper()
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &gitops.RealPackageLoader{ModulesDir: "apm_modules"}}
	return runInstall(deps, false, false, "claude", "", nil, nil)
}

// Issue #67: an agent directory a local package links in from elsewhere in
// the package is deployed, with the hash Oracle 0.33.0 records for it.
func TestRunInstall_LocalPackageSymlinkedAgentsDirIsDeployed(t *testing.T) {
	writeSymlinkAgentDep(t)
	if err := os.Symlink("../agents", filepath.Join("dep", ".apm", "agents")); err != nil {
		t.Fatal(err)
	}

	if err := installClaude(t); err != nil {
		t.Fatal(err)
	}

	if _, err := os.Stat(filepath.Join(".claude", "agents", "oracle.md")); err != nil {
		t.Fatalf(".claude/agents/oracle.md: %v", err)
	}
	lockBytes, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(lockBytes)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := lockfile.ParseLockfile(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(lock.Dependencies) != 1 {
		t.Fatalf("lock has %d dependencies, want 1", len(lock.Dependencies))
	}
	const want = "sha256:27942fd52d7fbfe035fc99f68a4b302fd5c77b3156f270ff825e02ace1e380ce"
	if got := lock.Dependencies[0].DeployedHashes[".claude/agents/oracle.md"]; got != want {
		t.Errorf("deployed_file_hashes[.claude/agents/oracle.md] = %q, want %q", got, want)
	}
}

// Issue #67: a symlink out of a local package fails the install with exit 1
// and the Oracle's reason, and leaves no file or directory behind, as Oracle
// 0.33.0 leaves none under apm_modules when it had to create it.
func TestRunInstall_LocalPackageSymlinkOutsideFailsBeforeWriting(t *testing.T) {
	dir := writeSymlinkAgentDep(t)
	outer := filepath.Join(dir, "outside", "outer.agent.md")
	if err := os.MkdirAll(filepath.Dir(outer), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outer, []byte("outside\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join("dep", ".apm", "agents"), 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "dep", ".apm", "agents", "outer.agent.md")
	if err := os.Symlink(outer, link); err != nil {
		t.Fatal(err)
	}

	err := installClaude(t)

	if code := exitCodeOf(err); code != 1 {
		t.Errorf("exit code %d (error %v), want 1", code, err)
	}
	want := "Symlink '" + link + "' resolves to '" + outer + "', which escapes the package root '" + filepath.Join(dir, "dep") +
		"'. Only in-package symlinks are dereferenced. Remove the symlink or replace it with a real file copy. Local install aborted."
	if err == nil || !strings.Contains(err.Error(), want) {
		t.Errorf("error %v\nwant it to contain\n%s", err, want)
	}
	for _, p := range []string{"apm.lock.yaml", ".claude", ".gitignore", "apm_modules"} {
		if _, err := os.Lstat(p); !os.IsNotExist(err) {
			t.Errorf("%s exists after the failed install (lstat error %v)", p, err)
		}
	}
}
