package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
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

	// opts.Verbose is deliberately left false: the warning must not be
	// gated behind --verbose (only removedFiles's "[-] ..." transcript is).
	stderr := captureUninstallStderr(t, func() {
		if err := runUninstall([]string{"acme/foo"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})
	if !strings.Contains(stderr, "modified since deploy (hash mismatch)") {
		t.Errorf(`expected a stderr warning containing "modified since deploy (hash mismatch)" even without --verbose, got:\n%s`, stderr)
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
	const barContent = "bar rule"
	fooHash := writeUninstallDeployedFile(t, dir, ".claude/rules/foo.md", "foo rule")
	barHash := writeUninstallDeployedFile(t, dir, ".claude/rules/bar.md", barContent)
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
	if got, err := os.ReadFile(filepath.Join(dir, ".claude", "rules", "bar.md")); err != nil || string(got) != barContent {
		t.Errorf("expected bar.md (untouched package) to survive byte-identical, got=%q err=%v", got, err)
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

// TestRunUninstall_DiamondSharedDependencyNotOrphaned is CRITICAL #1: a
// transitive dependency shared by two ROOT packages (a diamond) must not be
// deleted as an "orphan" just because LockedDep.ResolvedBy -- which only
// records a single parent per dependency -- happens to point at the package
// being removed. acme/a and acme/b are both root apm.yml dependencies;
// acme/x is a transitive dependency of BOTH (apm_modules/acme/b/apm.yml
// declares it too), but the lockfile's ResolvedBy for acme/x only recorded
// acme/a (the last writer). Uninstalling acme/a must NOT delete acme/x --
// acme/b still depends on it.
func TestRunUninstall_DiamondSharedDependencyNotOrphaned(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/a\n    - acme/b\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	// acme/b's own apm.yml declares a dependency on acme/x -- the fact that
	// makes this a diamond: acme/x is actually reachable through acme/b too,
	// not just through the ResolvedBy value recorded for acme/a.
	bModDir := filepath.Join(dir, "apm_modules", "acme", "b")
	if err := os.MkdirAll(bModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bManifest := "name: b\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/x\n"
	if err := os.WriteFile(filepath.Join(bModDir, "apm.yml"), []byte(bManifest), 0o644); err != nil {
		t.Fatal(err)
	}
	xHash := writeUninstallDeployedFile(t, dir, ".claude/rules/x.md", "x rule")
	if err := os.MkdirAll(filepath.Join(dir, "apm_modules", "acme", "x"), 0o755); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/a", Source: "git"},
			{RepoURL: "acme/b", Source: "git"},
			// ResolvedBy only records acme/a -- the diamond's other parent
			// (acme/b) is lost, exactly like the real ResolvedBy field.
			{RepoURL: "acme/x", Source: "git", ResolvedBy: "acme/a", DeployedFiles: []string{".claude/rules/x.md"}, DeployedHashes: map[string]string{".claude/rules/x.md": xHash}},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/a"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "x")); err != nil {
		t.Errorf("expected apm_modules/acme/x to survive (acme/b still depends on it), stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "x.md")); err != nil {
		t.Errorf("expected acme/x's deployed file to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "a")); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/acme/a to be removed, stat err=%v", err)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/b, acme/x still locked): %v", err)
	}
	lockNode, _ := yamlcore.SafeLoad(lockData)
	newLock, _ := lockfile.ParseLockfile(lockNode)
	if newLock.FindByKey("acme/x") == nil {
		t.Error("expected acme/x to remain locked (still reachable via acme/b)")
	}
	if newLock.FindByKey("acme/a") != nil {
		t.Error("expected acme/a to be gone from the lockfile")
	}

	fx := readManifestParsed(t)
	if len(fx.ParsedDeps) != 1 || fx.ParsedDeps[0].RepoURL != "acme/b" {
		t.Errorf("expected only acme/b to remain in apm.yml, got %+v", fx.ParsedDeps)
	}
}

// TestReachableFromRemainingRoots_MultiLevelChain proves the BFS walks
// transitively, not just one hop: root -> mid -> leaf, where only root is a
// remaining root key.
func TestReachableFromRemainingRoots_MultiLevelChain(t *testing.T) {
	dir := chdirTemp(t)

	rootDir := filepath.Join(dir, "apm_modules", "acme", "root")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "apm.yml"), []byte("name: root\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/mid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	midDir := filepath.Join(dir, "apm_modules", "acme", "mid")
	if err := os.MkdirAll(midDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(midDir, "apm.yml"), []byte("name: mid\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/root", Source: "git"},
			{RepoURL: "acme/mid", Source: "git", ResolvedBy: "acme/root"},
			{RepoURL: "acme/leaf", Source: "git", ResolvedBy: "acme/mid"},
		},
	}

	reachable := reachableFromRemainingRoots(map[string]bool{"acme/root": true}, lock, dir)
	for _, want := range []string{"acme/root", "acme/mid", "acme/leaf"} {
		if !reachable[want] {
			t.Errorf("expected %s to be reachable, got %v", want, reachable)
		}
	}
}

// TestReachableFromRemainingRoots_NilLockfile proves the function fails
// open (returns just the roots themselves, no panic) when there is no
// lockfile to cross-reference against.
func TestReachableFromRemainingRoots_NilLockfile(t *testing.T) {
	reachable := reachableFromRemainingRoots(map[string]bool{"acme/root": true}, nil, ".")
	if len(reachable) != 1 || !reachable["acme/root"] {
		t.Errorf("expected only the root itself, got %v", reachable)
	}
}

// TestReachableFromRemainingRoots_DepNotInLockfileIsSkipped proves a
// dependency declared on disk but absent from the lockfile is never added to
// reachable (and never enqueued), since it has no deployed state to protect.
func TestReachableFromRemainingRoots_DepNotInLockfileIsSkipped(t *testing.T) {
	dir := chdirTemp(t)

	rootDir := filepath.Join(dir, "apm_modules", "acme", "root")
	if err := os.MkdirAll(rootDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rootDir, "apm.yml"), []byte("name: root\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/unlocked\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{{RepoURL: "acme/root", Source: "git"}},
	}

	reachable := reachableFromRemainingRoots(map[string]bool{"acme/root": true}, lock, dir)
	if reachable["acme/unlocked"] {
		t.Error("expected acme/unlocked (not in lockfile) to not be marked reachable")
	}
	if len(reachable) != 1 {
		t.Errorf("expected only acme/root to be reachable, got %v", reachable)
	}
}

// TestReachableFromRemainingRoots_LocalModulesKeyWalksTransitive is this
// task's (07-11-local-root-key-space) reproduction: the BFS must be fed the
// translated "_local/<base>-<sha8>" module key -- the same key
// apm_modules/lockfile actually use for a local root -- to walk into its
// real on-disk apm.yml and discover its transitive dependency. Feeding it
// the untranslated synthetic "local:<path>" identity (this bug's shape)
// finds no such directory, so the walk never proceeds past the root itself.
func TestReachableFromRemainingRoots_LocalModulesKeyWalksTransitive(t *testing.T) {
	dir := chdirTemp(t)

	localKey := localModulesKey(resolveLocalSourceAbs("./dep-pkg"))
	localModDir := filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))
	if err := os.MkdirAll(localModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localModDir, "apm.yml"), []byte("name: dep-pkg\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/transitive-of-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: localKey, Source: "git"},
			{RepoURL: "acme/transitive-of-a", Source: "git", ResolvedBy: localKey},
		},
	}

	reachable := reachableFromRemainingRoots(map[string]bool{localKey: true}, lock, dir)

	if !reachable[localKey] || !reachable["acme/transitive-of-a"] {
		t.Errorf("expected the local root and its transitive dependency to be reachable, got %v", reachable)
	}
	if len(reachable) != 2 {
		t.Errorf("expected exactly the local root and its transitive dependency to be reachable, got %v", reachable)
	}
	if reachable["local:./dep-pkg"] {
		t.Errorf("expected the synthetic \"local:\" identity to never be probed, got %v", reachable)
	}
}

// TestUninstallRemainingRootKeys_LocalRootUsesModulesKey is this task's
// (07-11-local-root-key-space) main TDD reproduction: uninstallRemainingRootKeys
// must translate a SURVIVING local root's synthetic uninstallIdentity
// "local:<path>" matching key into the same "_local/<base>-<sha8>"
// apm_modules/lockfile key space uninstallRemovalKey already produces for
// REMOVED local roots (commit 171fd87) -- reachableFromRemainingRoots and
// computeUninstallStaleMCP both only understand that key space. Covers
// dependencies.apm/devDependencies.apm x relative/absolute local paths --
// four cases, all translating the same way.
func TestUninstallRemainingRootKeys_LocalRootUsesModulesKey(t *testing.T) {
	localKeyRe := regexp.MustCompile(`^_local/[^/]+-[0-9a-f]{8}$`)

	tests := []struct {
		name string
		dev  bool
		path func(dir string) string
	}{
		{name: "prod relative", dev: false, path: func(dir string) string { return "./dep-pkg" }},
		{name: "prod absolute", dev: false, path: func(dir string) string { return filepath.Join(dir, "dep-pkg") }},
		{name: "dev relative", dev: true, path: func(dir string) string { return "./dep-pkg" }},
		{name: "dev absolute", dev: true, path: func(dir string) string { return filepath.Join(dir, "dep-pkg") }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := chdirTemp(t)
			path := tc.path(dir)
			wantKey := localModulesKey(resolveLocalSourceAbs(path))
			if !localKeyRe.MatchString(wantKey) {
				t.Fatalf("test setup produced an unexpected key shape %q", wantKey)
			}

			ref := &manifest.DependencyReference{IsLocal: true, LocalPath: path, Source: "local"}
			m := &manifest.Manifest{}
			if tc.dev {
				m.ParsedDevDeps = []*manifest.DependencyReference{ref}
			} else {
				m.ParsedDeps = []*manifest.DependencyReference{ref}
			}

			got := uninstallRemainingRootKeys(m, map[string]bool{})

			if len(got) != 1 {
				t.Fatalf("expected exactly one remaining root key, got %v", got)
			}
			if !got[wantKey] {
				t.Errorf("expected remaining root key %q, got %v", wantKey, got)
			}
			for k := range got {
				if strings.HasPrefix(k, "local:") {
					t.Errorf("expected no synthetic \"local:\" key in remaining root keys, got %v", got)
				}
			}
		})
	}
}

// TestUninstallRemainingRootKeys_RemovedLocalRootExcluded proves the
// removedIdentities filter still runs in the SAME identity space
// uninstallIdentity produces (before translation) -- a local root that is
// itself being removed must never reappear, translated or not, in the
// remaining set.
func TestUninstallRemainingRootKeys_RemovedLocalRootExcluded(t *testing.T) {
	chdirTemp(t)
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{IsLocal: true, LocalPath: "./dep-pkg", Source: "local"},
		},
	}

	got := uninstallRemainingRootKeys(m, map[string]bool{"local:./dep-pkg": true})

	if len(got) != 0 {
		t.Errorf("expected the removed local root to be excluded entirely, got %v", got)
	}
}

// TestUninstallRemainingRootKeys_NonLocalKeyUnchanged proves
// uninstallRemovalKey's translation is a no-op for git/marketplace roots --
// their remaining-root key must stay byte-identical to their
// uninstallIdentity.
func TestUninstallRemainingRootKeys_NonLocalKeyUnchanged(t *testing.T) {
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{RepoURL: "acme/foo", Source: "git"},
		},
	}

	got := uninstallRemainingRootKeys(m, map[string]bool{})

	if len(got) != 1 || !got["acme/foo"] {
		t.Errorf("expected the git root key to pass through unchanged, got %v", got)
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

// TestPrepareUninstallPlan_LocalDepRemovalKeysUseModulesKey is the ag-23
// defect's plan-level regression: a local-path dependency matches by
// uninstallIdentity's synthetic "local:<path>" key, but install
// (normalizeLocalDep) materializes it under apm_modules/_local/<base>-<sha8>
// and records that same "_local/..." key as the lockfile repo_url -- so the
// REMOVAL key space must carry the translated modules key, or every lockfile
// lookup misses and SafeRemoveModuleDir is handed an invalid
// "apm_modules/local:./..." path.
func TestPrepareUninstallPlan_LocalDepRemovalKeysUseModulesKey(t *testing.T) {
	chdirTemp(t)

	localKey := localModulesKey(resolveLocalSourceAbs("./dep-pkg"))
	m := &manifest.Manifest{
		ParsedDeps: []*manifest.DependencyReference{
			{IsLocal: true, LocalPath: "./dep-pkg", Source: "local"},
		},
	}
	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{
				RepoURL:        localKey,
				Source:         "git",
				DeployedFiles:  []string{".agents/agents/depagent/agent.md"},
				DeployedHashes: map[string]string{".agents/agents/depagent/agent.md": "sha256:0000"},
			},
		},
	}

	plan := prepareUninstallPlan([]string{"./dep-pkg"}, m, lock, false)

	if len(plan.resolution.APMTargets) != 1 {
		t.Fatalf("expected the local dep to match, got %+v", plan.resolution)
	}
	if !plan.allRemovalKeys[localKey] {
		t.Errorf("expected removal keys to contain modules key %q, got %v", localKey, plan.allRemovalKeys)
	}
	if plan.allRemovalKeys["local:./dep-pkg"] {
		t.Errorf("expected the synthetic matching key to stay out of the removal key space, got %v", plan.allRemovalKeys)
	}

	files, _ := collectUninstallDeployedProvenance(lock, plan.allRemovalKeys)
	if len(files) != 1 || files[0] != ".agents/agents/depagent/agent.md" {
		t.Errorf("expected deployed provenance to be found via the modules key, got %v", files)
	}
}

// TestRunUninstall_LocalPathDependencyRemovesModulesLockAndDeployedFiles is
// the ag-23 end-to-end round trip against constructed post-install state:
// apm.yml declares "- ./dep-pkg", the lockfile keys it as
// "_local/dep-pkg-<sha8>", and its deployed antigravity agent file exists on
// disk. Uninstalling "./dep-pkg" must remove the deployed file (pruning the
// now-empty per-agent directory), the apm_modules/_local/... checkout, the
// lockfile entry, and the apm.yml entry -- while a sibling agent file and an
// unrelated git dependency survive untouched.
func TestRunUninstall_LocalPathDependencyRemovesModulesLockAndDeployedFiles(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/other\n    - ./dep-pkg\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	// The local source directory itself (never touched by uninstall).
	if err := os.MkdirAll(filepath.Join(dir, "dep-pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "dep-pkg", "apm.yml"), []byte("name: dep-pkg\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	localKey := localModulesKey(resolveLocalSourceAbs("./dep-pkg"))
	localModuleDir := filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))
	if err := os.MkdirAll(localModuleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(localModuleDir, "apm.yml"), []byte("name: dep-pkg\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	otherModuleDir := filepath.Join(dir, "apm_modules", "acme", "other")
	if err := os.MkdirAll(otherModuleDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(otherModuleDir, "apm.yml"), []byte("name: other\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	agentHash := writeUninstallDeployedFile(t, dir, ".agents/agents/depagent/agent.md", "dep agent")
	// Sibling agent, not owned by the removed dep -- must survive, and must
	// keep .agents/agents/ itself from being pruned.
	writeUninstallDeployedFile(t, dir, ".agents/agents/reviewer/agent.md", "reviewer agent")
	// A file the removed local dep itself deployed, but that the user then
	// hand-edited -- this task's SEC-09 non-regression: even when the local
	// dep root is removed outright (not just an orphan), un-053's hash-check
	// safety line must still keep a hand-modified file instead of deleting
	// it, exercised through uninstallRemovalKey's translated key space
	// (rather than SEC-07's git-dep fixture).
	origNotesHash := writeUninstallDeployedFile(t, dir, ".claude/rules/dep-pkg-notes.md", "original notes")
	editedNotesPath := filepath.Join(dir, ".claude", "rules", "dep-pkg-notes.md")
	const editedNotesContent = "hand edited notes"
	if err := os.WriteFile(editedNotesPath, []byte(editedNotesContent), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: "acme/other", Source: "git"},
			{
				RepoURL:       localKey,
				Source:        "git",
				DeployedFiles: []string{".agents/agents/depagent/agent.md", ".claude/rules/dep-pkg-notes.md"},
				DeployedHashes: map[string]string{
					".agents/agents/depagent/agent.md": agentHash,
					".claude/rules/dep-pkg-notes.md":   origNotesHash,
				},
			},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	stderr := captureUninstallStderr(t, func() {
		if err := runUninstall([]string{"./dep-pkg"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})
	if !strings.Contains(stderr, "modified since deploy (hash mismatch)") {
		t.Errorf(`expected a stderr warning containing "modified since deploy (hash mismatch)" for the hand-edited local-dep file, got:\n%s`, stderr)
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents", "agents", "depagent")); !os.IsNotExist(err) {
		t.Errorf("expected deployed agent file and its now-empty directory to be removed, stat err=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, ".agents", "agents", "reviewer", "agent.md")); err != nil || string(got) != "reviewer agent" {
		t.Errorf("expected sibling agent to survive untouched, got=%q err=%v", got, err)
	}
	if got, err := os.ReadFile(editedNotesPath); err != nil || string(got) != editedNotesContent {
		t.Errorf("expected the hand-edited local-dep file to survive byte-identical, got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "_local")); !os.IsNotExist(err) {
		t.Errorf("expected apm_modules/_local (now empty) to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(otherModuleDir, "apm.yml")); err != nil {
		t.Errorf("expected apm_modules/acme/other to survive, stat err=%v", err)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (acme/other still locked): %v", err)
	}
	lockNode, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatalf("parse surviving lockfile: %v", err)
	}
	survived, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatalf("validate surviving lockfile: %v", err)
	}
	if survived.FindByKey(localKey) != nil {
		t.Errorf("expected lockfile entry %q to be removed", localKey)
	}
	if survived.FindByKey("acme/other") == nil {
		t.Error("expected lockfile entry acme/other to survive")
	}

	fx := readManifestParsed(t)
	if len(fx.ParsedDeps) != 1 || fx.ParsedDeps[0].RepoURL != "acme/other" {
		t.Errorf("expected apm.yml to keep only acme/other, got %+v", fx.ParsedDeps)
	}
	for _, d := range fx.ParsedDeps {
		if d.IsLocal {
			t.Errorf("expected the local apm.yml entry to be removed, got %+v", d)
		}
	}
}
