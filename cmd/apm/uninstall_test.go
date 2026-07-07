package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
	"github.com/spf13/pflag"
)

// writeUninstallDeployedFile writes content at dir/relPath and returns the
// deploy-envelope hash for it, matching what install.go's deployAndFinalize
// would have recorded in a LockedDep's DeployedHashes.
func writeUninstallDeployedFile(t *testing.T, dir, relPath, content string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(relPath))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := lockfile.HashFileBytes(full)
	if err != nil {
		t.Fatal(err)
	}
	return hash
}

func writeUninstallLockfileFixture(t *testing.T, lock *lockfile.Lockfile) {
	t.Helper()
	if lock.Version == "" {
		lock.Version = "1"
	}
	out, err := lockfile.WriteLockfile(lock, nil)
	if err != nil {
		t.Fatalf("WriteLockfile: %v", err)
	}
	if err := os.WriteFile("apm.lock.yaml", out, 0644); err != nil {
		t.Fatal(err)
	}
}

func readManifestParsed(t *testing.T) *manifest.Manifest {
	t.Helper()
	data, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatalf("read apm.yml: %v", err)
	}
	node, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatalf("parse apm.yml: %v", err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatalf("validate apm.yml: %v", err)
	}
	return m
}

// TestRunUninstall_HelpFlagSetIsExact locks down un-003: the flag set is
// exactly --dry-run, -v/--verbose, -g/--global (plus cobra's own --help).
func TestRunUninstall_HelpFlagSetIsExact(t *testing.T) {
	cmd := uninstallCmd()
	got := map[string]bool{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) { got[f.Name] = true })
	want := map[string]bool{"dry-run": true, "verbose": true, "global": true}
	for name := range want {
		if !got[name] {
			t.Errorf("expected flag %q to be registered, got %v", name, got)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("unexpected extra flag %q registered, got %v", name, got)
		}
	}
	if cmd.Flags().ShorthandLookup("v") == nil {
		t.Error("expected -v shorthand for --verbose")
	}
	if cmd.Flags().ShorthandLookup("g") == nil {
		t.Error("expected -g shorthand for --global")
	}
}

func TestRunUninstall_GlobalFlagUnsupported(t *testing.T) {
	chdirTemp(t) // no apm.yml at all -- proves -g is checked before anything else
	err := runUninstall([]string{"acme/foo"}, uninstallOptions{Global: true})
	if err == nil {
		t.Fatal("expected an error for -g/--global")
	}
}

func TestRunUninstall_MissingApmYML_Errors(t *testing.T) {
	chdirTemp(t)
	err := runUninstall([]string{"acme/foo"}, uninstallOptions{})
	if err == nil {
		t.Fatal("expected an error when apm.yml doesn't exist")
	}
}

func TestRunUninstall_AllNotFound_NoChanges(t *testing.T) {
	chdirTemp(t)
	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	err := runUninstall([]string{"acme/does-not-exist"}, uninstallOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != manifestYAML {
		t.Errorf("apm.yml changed even though nothing matched:\ngot:  %q\nwant: %q", got, manifestYAML)
	}
}

// TestRunUninstall_RemovesPackageDeployedFilesModuleDirAndLockEntry is the
// core un-V01/un-V02 round trip: apm.yml/apm_modules/deployed target
// files/lockfile entry all disappear for the removed package, and a
// hand-written file living alongside a deployed one survives untouched.
func TestRunUninstall_RemovesPackageDeployedFilesModuleDirAndLockEntry(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	ruleHash := writeUninstallDeployedFile(t, dir, ".claude/rules/foo.md", "foo rule")
	skillHash := writeUninstallDeployedFile(t, dir, ".agents/skills/foo-skill/SKILL.md", "foo skill")
	claudeSkillHash := writeUninstallDeployedFile(t, dir, ".claude/skills/foo-skill/SKILL.md", "foo skill (claude copy)")
	cmdHash := writeUninstallDeployedFile(t, dir, ".agents/commands/foo-cmd.md", "foo command")
	// A user-authored file living in the same directory as a deployed one --
	// never listed in DeployedFiles, must survive untouched.
	userNotes := "these are my own notes"
	writeUninstallDeployedFile(t, dir, ".claude/rules/user-notes.md", userNotes)

	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "apm_modules", "acme", "foo", "apm.yml"), []byte("name: foo\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deployedFiles := []string{".claude/rules/foo.md", ".agents/skills/foo-skill/SKILL.md", ".claude/skills/foo-skill/SKILL.md", ".agents/commands/foo-cmd.md"}
	deployedHashes := map[string]string{
		".claude/rules/foo.md":              ruleHash,
		".agents/skills/foo-skill/SKILL.md": skillHash,
		".claude/skills/foo-skill/SKILL.md": claudeSkillHash,
		".agents/commands/foo-cmd.md":       cmdHash,
	}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git", ResolvedCommit: "0123456789abcdef0123456789abcdef01234567", DeployedFiles: deployedFiles, DeployedHashes: deployedHashes},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	for _, f := range deployedFiles {
		if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(f))); !os.IsNotExist(err) {
			t.Errorf("expected %s to be removed, stat err=%v", f, err)
		}
	}
	if got, err := os.ReadFile(filepath.Join(dir, ".claude", "rules", "user-notes.md")); err != nil || string(got) != userNotes {
		t.Errorf("expected user-authored file to survive untouched, got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "foo")); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/acme/foo to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules")); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules itself to be cleaned up (now empty), stat err=%v", err)
	}

	fx := readManifestParsed(t)
	if len(fx.ParsedDeps) != 0 {
		t.Errorf("expected apm.yml dependencies.apm to be empty, got %+v", fx.ParsedDeps)
	}

	if _, err := os.Stat("apm.lock.yaml"); !os.IsNotExist(err) {
		t.Errorf("expected apm.lock.yaml to be deleted once its only dependency was removed (un-071), stat err=%v", err)
	}
}

// TestRunUninstall_HashMismatchKeepsFileWithWarning is un-053/un-V03's
// non-negotiable safety line, exercised through the CLI, not just
// deploy.RemoveDeployedFiles directly.
func TestRunUninstall_HashMismatchKeepsFileWithWarning(t *testing.T) {
	dir := chdirTemp(t)

	if err := os.WriteFile("apm.yml", []byte("name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"), 0644); err != nil {
		t.Fatal(err)
	}
	origHash := writeUninstallDeployedFile(t, dir, ".claude/rules/foo.md", "original content")
	// Simulate a user hand-edit after deploy.
	editedPath := filepath.Join(dir, ".claude", "rules", "foo.md")
	if err := os.WriteFile(editedPath, []byte("user edited content"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git", DeployedFiles: []string{".claude/rules/foo.md"}, DeployedHashes: map[string]string{".claude/rules/foo.md": origHash}},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	got, err := os.ReadFile(editedPath)
	if err != nil {
		t.Fatalf("expected hand-edited file to survive, stat err=%v", err)
	}
	if string(got) != "user edited content" {
		t.Errorf("file content changed unexpectedly: %q", got)
	}
}

// TestRunUninstall_OnlyRemovesTargetedPackagesFiles is the Review Gate
// B/C-style negative proof: two packages deployed to the same target,
// uninstalling one leaves the other's apm.yml entry, apm_modules dir,
// deployed file, and lockfile entry completely intact.
func TestRunUninstall_OnlyRemovesTargetedPackagesFiles(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n    - acme/bar\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	fooHash := writeUninstallDeployedFile(t, dir, ".claude/rules/foo.md", "foo rule")
	barHash := writeUninstallDeployedFile(t, dir, ".claude/rules/bar.md", "bar rule")
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "bar"), 0o755); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git", DeployedFiles: []string{".claude/rules/foo.md"}, DeployedHashes: map[string]string{".claude/rules/foo.md": fooHash}},
			{RepoURL: "acme/bar", Source: "git", DeployedFiles: []string{".claude/rules/bar.md"}, DeployedHashes: map[string]string{".claude/rules/bar.md": barHash}},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "foo.md")); !os.IsNotExist(err) {
		t.Errorf("expected foo.md removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "bar.md")); err != nil {
		t.Errorf("expected bar.md (untouched package) to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "foo")); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/acme/foo removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "bar")); err != nil {
		t.Errorf("expected apm_modules/acme/bar to survive, stat err=%v", err)
	}

	fx := readManifestParsed(t)
	if len(fx.ParsedDeps) != 1 || fx.ParsedDeps[0].RepoURL != "acme/bar" {
		t.Errorf("expected only acme/bar to remain in apm.yml, got %+v", fx.ParsedDeps)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/bar still locked): %v", err)
	}
	lockNode, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	if newLock.FindByKey("acme/foo") != nil {
		t.Error("expected acme/foo to be gone from the lockfile")
	}
	if newLock.FindByKey("acme/bar") == nil {
		t.Error("expected acme/bar to remain in the lockfile")
	}
}

// TestRunUninstall_TransitiveOrphanRemoved is un-040/un-V04: a
// lockfile-only transitive dependency with no other referrer is pruned
// along with the package that pulled it in.
func TestRunUninstall_TransitiveOrphanRemoved(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git"},
			{RepoURL: "acme/child", Source: "git", ResolvedBy: "acme/foo"},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "child")); !os.IsNotExist(err) {
		t.Errorf("expected orphaned acme/child to be removed, stat err=%v", err)
	}
	if _, err := os.Stat("apm.lock.yaml"); !os.IsNotExist(err) {
		t.Errorf("expected apm.lock.yaml to be deleted (both entries gone), stat err=%v", err)
	}
}

// TestRunUninstall_OrphanKeptWhenStillReferenced is un-041's other half: a
// transitive child that is ALSO still declared directly in apm.yml is not
// treated as an orphan.
func TestRunUninstall_OrphanKeptWhenStillReferenced(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n    - acme/child\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "child"), 0o755); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git"},
			{RepoURL: "acme/child", Source: "git", ResolvedBy: "acme/foo"},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "child")); err != nil {
		t.Errorf("expected acme/child (still a direct apm.yml dependency) to survive, stat err=%v", err)
	}
	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive: %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, _ := lockfile.ParseLockfile(lockNode)
	if newLock.FindByKey("acme/child") == nil {
		t.Error("expected acme/child to remain locked")
	}
}

// TestRunUninstall_DryRunMakesNoChanges is un-080/081/un-V-style: --dry-run
// must not touch apm.yml, apm.lock.yaml, apm_modules, or any deployed file.
func TestRunUninstall_DryRunMakesNoChanges(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	hash := writeUninstallDeployedFile(t, dir, ".claude/rules/foo.md", "foo rule")
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "foo"), 0o755); err != nil {
		t.Fatal(err)
	}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git", DeployedFiles: []string{".claude/rules/foo.md"}, DeployedHashes: map[string]string{".claude/rules/foo.md": hash}},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	beforeManifest, _ := os.ReadFile("apm.yml")
	beforeLock, _ := os.ReadFile("apm.lock.yaml")

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{DryRun: true}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	afterManifest, _ := os.ReadFile("apm.yml")
	afterLock, _ := os.ReadFile("apm.lock.yaml")
	if string(beforeManifest) != string(afterManifest) {
		t.Error("expected apm.yml to be unchanged by --dry-run")
	}
	if string(beforeLock) != string(afterLock) {
		t.Error("expected apm.lock.yaml to be unchanged by --dry-run")
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "foo.md")); err != nil {
		t.Errorf("expected deployed file to survive --dry-run, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "foo")); err != nil {
		t.Errorf("expected apm_modules/acme/foo to survive --dry-run, stat err=%v", err)
	}
}

// TestRunUninstall_StandaloneMCPRoundTrip is un-019/064/065/un-V10: a
// standalone `install --mcp NAME` server (dependencies.mcp, no matching apm
// package) is removed from apm.yml, every target's MCP config file, and
// lock.MCPServers, while an unrelated MCP server and an unrelated apm
// dependency are left untouched.
func TestRunUninstall_StandaloneMCPRoundTrip(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := `name: test
version: "1.0.0"
target: [claude]
dependencies:
  apm:
    - acme/baz
  mcp:
    - name: foo
      registry: false
      transport: stdio
      command: foo-server
    - name: bar
      registry: false
      transport: stdio
      command: bar-server
`
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{"mcpServers":{"foo":{"type":"stdio","command":"foo-server"},"bar":{"type":"stdio","command":"bar-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{{RepoURL: "acme/baz", Source: "git"}},
		MCPServers:   []string{"foo", "bar"},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	fx := readManifestParsed(t)
	for _, s := range fx.MCPServers {
		if s.Name == "foo" {
			t.Errorf("expected apm.yml dependencies.mcp to no longer contain foo, got %+v", fx.MCPServers)
		}
	}
	foundBar := false
	for _, s := range fx.MCPServers {
		if s.Name == "bar" {
			foundBar = true
		}
	}
	if !foundBar {
		t.Errorf("expected apm.yml dependencies.mcp to still contain bar, got %+v", fx.MCPServers)
	}
	if len(fx.ParsedDeps) != 1 || fx.ParsedDeps[0].RepoURL != "acme/baz" {
		t.Errorf("expected acme/baz apm dependency to be untouched, got %+v", fx.ParsedDeps)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["foo"]; ok {
		t.Errorf("expected foo removed from .mcp.json, got %v", servers)
	}
	if _, ok := servers["bar"]; !ok {
		t.Errorf("expected bar to remain in .mcp.json, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/baz still locked): %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, _ := lockfile.ParseLockfile(lockNode)
	foundFooInLock, foundBarInLock := false, false
	for _, s := range newLock.MCPServers {
		if s == "foo" {
			foundFooInLock = true
		}
		if s == "bar" {
			foundBarInLock = true
		}
	}
	if foundFooInLock {
		t.Errorf("expected lock.MCPServers to no longer contain foo, got %v", newLock.MCPServers)
	}
	if !foundBarInLock {
		t.Errorf("expected lock.MCPServers to still contain bar, got %v", newLock.MCPServers)
	}
}

// TestRunUninstall_TransitiveMCPStaleDiff_RemovesOnlyDroppedServer is
// un-060/061/062/063's transitive half: two dependencies each contribute
// their own self-defined MCP server (recorded in lock.MCPServers, already
// deployed to a target config file); uninstalling the package that
// contributed one of them removes only that server from the target and from
// lock.MCPServers, leaving the other package's server (and the other package
// itself) untouched.
func TestRunUninstall_TransitiveMCPStaleDiff_RemovesOnlyDroppedServer(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n    - acme/bar\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	fooModDir := filepath.Join(dir, "apm_modules", "acme", "foo")
	if err := os.MkdirAll(fooModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fooManifest := "name: foo\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: srvA\n      registry: false\n      transport: stdio\n      command: srvA-server\n"
	if err := os.WriteFile(filepath.Join(fooModDir, "apm.yml"), []byte(fooManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	barModDir := filepath.Join(dir, "apm_modules", "acme", "bar")
	if err := os.MkdirAll(barModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	barManifest := "name: bar\nversion: \"1.0.0\"\ndependencies:\n  mcp:\n    - name: srvB\n      registry: false\n      transport: stdio\n      command: srvB-server\n"
	if err := os.WriteFile(filepath.Join(barModDir, "apm.yml"), []byte(barManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{"mcpServers":{"srvA":{"type":"stdio","command":"srvA-server"},"srvB":{"type":"stdio","command":"srvB-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git"},
			{RepoURL: "acme/bar", Source: "git"},
		},
		MCPServers: []string{"srvA", "srvB"},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["srvA"]; ok {
		t.Errorf("expected srvA (dropped along with acme/foo) removed from .mcp.json, got %v", servers)
	}
	if _, ok := servers["srvB"]; !ok {
		t.Errorf("expected srvB (still contributed by acme/bar) to remain in .mcp.json, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/bar still locked): %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, _ := lockfile.ParseLockfile(lockNode)
	if len(newLock.MCPServers) != 1 || newLock.MCPServers[0] != "srvB" {
		t.Errorf("expected lock.MCPServers to be [srvB], got %v", newLock.MCPServers)
	}
	if newLock.FindByKey("acme/bar") == nil {
		t.Error("expected acme/bar to remain locked")
	}
}

// TestRunUninstall_TransitiveMCPStaleDiff_RootMCPUntouchedAndPersisted is
// un-061's negative-and-persistence half: a root-declared (dependencies.mcp)
// MCP server not named on the command line is never treated as stale just
// because an unrelated apm package was uninstalled, and lock.MCPServers is
// re-serialized correctly across the write/re-read round trip.
func TestRunUninstall_TransitiveMCPStaleDiff_RootMCPUntouchedAndPersisted(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := `name: test
version: "1.0.0"
dependencies:
  apm:
    - acme/foo
    - acme/keep
  mcp:
    - name: srvC
      registry: false
      transport: stdio
      command: srvC-server
`
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	mcpJSON := `{"mcpServers":{"srvC":{"type":"stdio","command":"srvC-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/foo", Source: "git"},
			{RepoURL: "acme/keep", Source: "git"},
		},
		MCPServers: []string{"srvC"},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["srvC"]; !ok {
		t.Errorf("expected root-declared srvC to remain untouched in .mcp.json, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/keep still locked): %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(newLock.MCPServers) != 1 || newLock.MCPServers[0] != "srvC" {
		t.Errorf("expected lock.MCPServers to remain [srvC] across the write/re-read round trip, got %v", newLock.MCPServers)
	}
	if newLock.FindByKey("acme/keep") == nil {
		t.Error("expected acme/keep to remain locked")
	}
}

// TestRunUninstall_MixedApmAndMcpTargetsInOneCall proves
// writeUninstallManifest's reparse-between-structural-edits correctness: a
// single `uninstall` call naming BOTH an apm package (dependencies.apm,
// listed BEFORE dependencies.mcp in the file) and a standalone MCP server
// must remove both, and must not corrupt the untouched sibling entries in
// either sequence -- without the reparse, splicing the physically-earlier
// apm.yml section first would leave the MCP removal call locating
// dependencies.mcp using stale (pre-edit) line numbers.
func TestRunUninstall_MixedApmAndMcpTargetsInOneCall(t *testing.T) {
	chdirTemp(t)

	manifestYAML := `name: test
version: "1.0.0"
dependencies:
  apm:
    - acme/foo
    - acme/keep
  mcp:
    - name: foo-mcp
      registry: false
      transport: stdio
      command: foo-mcp-server
    - name: bar-mcp
      registry: false
      transport: stdio
      command: bar-mcp-server
`
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	if err := runUninstall([]string{"acme/foo", "foo-mcp"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	fx := readManifestParsed(t)
	if len(fx.ParsedDeps) != 1 || fx.ParsedDeps[0].RepoURL != "acme/keep" {
		t.Errorf("expected only acme/keep to remain in dependencies.apm, got %+v", fx.ParsedDeps)
	}
	if len(fx.MCPServers) != 1 || fx.MCPServers[0].Name != "bar-mcp" {
		t.Errorf("expected only bar-mcp to remain in dependencies.mcp, got %+v", fx.MCPServers)
	}
}

// TestRunUninstall_MarketplaceRefDryRunSkipped is un-081: a marketplace ref
// with no lockfile anchor can't be previewed under --dry-run (it would need
// a registry call), so it folds into NotFound instead of erroring the whole
// command -- also exercises printUninstallNotFound's dry-run-skipped branch.
func TestRunUninstall_MarketplaceRefDryRunSkipped(t *testing.T) {
	chdirTemp(t)
	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/foo\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	err := runUninstall([]string{"some-plugin@some-marketplace"}, uninstallOptions{DryRun: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != manifestYAML {
		t.Error("expected apm.yml to be unchanged")
	}
}

// TestCollectUninstallDeployedProvenance_DeterministicHashMergeAcrossKeys is
// Bug B: collectUninstallDeployedProvenance used to range directly over the
// removalKeys map (unordered iteration) while merging every removed
// dependency's DeployedHashes into a single map, so a path recorded by more
// than one removal key with two different hash values would resolve to
// whichever key Go's runtime happened to visit last -- a different, random
// answer on every call. Iterating a sorted key order makes the merge
// deterministic (last-sorted-key-wins), matching collectUninstallDeployedProvenance's
// own contract that removalKeys is just an unordered set.
func TestCollectUninstallDeployedProvenance_DeterministicHashMergeAcrossKeys(t *testing.T) {
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/aaa", DeployedFiles: []string{"shared.md"}, DeployedHashes: map[string]string{"shared.md": "hash-from-aaa"}},
			{RepoURL: "acme/zzz", DeployedFiles: []string{"shared.md"}, DeployedHashes: map[string]string{"shared.md": "hash-from-zzz"}},
		},
	}
	removalKeys := map[string]bool{"acme/aaa": true, "acme/zzz": true}

	var want string
	for i := 0; i < 200; i++ {
		_, hashes := collectUninstallDeployedProvenance(lock, removalKeys)
		got := hashes["shared.md"]
		if i == 0 {
			want = got
			continue
		}
		if got != want {
			t.Fatalf("collectUninstallDeployedProvenance merged shared.md's hash non-deterministically across repeated calls with identical input: call 0 got %q, call %d got %q", want, i, got)
		}
	}
	if want != "hash-from-zzz" {
		t.Errorf("expected sorted-key-order merge to settle on acme/zzz's hash (last in ascending sort order), got %q", want)
	}
}

// TestUninstallCmd_RequiresAtLeastOnePackage is un-002: zero PACKAGE
// arguments is a usage error (cobra.MinimumNArgs(1)), not a silent no-op.
func TestUninstallCmd_RequiresAtLeastOnePackage(t *testing.T) {
	cmd := uninstallCmd()
	cmd.SetOut(new(strings.Builder))
	cmd.SetErr(new(strings.Builder))
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected an error when no PACKAGE arguments are given")
	}
}
