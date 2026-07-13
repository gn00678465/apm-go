package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/yamlcore"
)

// setupSurvivingLocalRootFixture is this task's (07-11-local-root-key-space)
// shared TDD reproduction fixture: a project with a SURVIVING local
// dependency A ("./dep-pkg", declaring its own transitive dependency on
// acme/transitive-of-a and its own MCP server srvA) and a git dependency B
// (acme/pkgB, being removed by every test in this file) that shares that
// same transitive dependency X. The lockfile deliberately records X's
// ResolvedBy as B (not A) -- the diamond shape commit 171fd87's own comment
// calls out -- so X's survival can ONLY come from reachableFromRemainingRoots
// walking A's real apm_modules/_local/... tree (resolver.ActualOrphans's own
// fallback union does not protect an orphan candidate, see
// research/local-root-key-space-gap.md #6), which in turn only works once
// uninstallRemainingRootKeys emits the correct "_local/<base>-<sha8>" key
// for A instead of the synthetic "local:<path>" identity. B also carries its
// own deployed file and its own self-defined MCP server srvB, so every
// assertion proves the fix is a translation, not a blanket "never clean up
// anything" regression: B's own file/module/lock/MCP entries must still be
// removed exactly as before.
func setupSurvivingLocalRootFixture(t *testing.T, aIsDev bool) (dir, localKey string) {
	t.Helper()
	dir = chdirTemp(t)

	var manifestYAML string
	if aIsDev {
		manifestYAML = "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/pkgB\ndevDependencies:\n  apm:\n    - ./dep-pkg\n"
	} else {
		manifestYAML = "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - ./dep-pkg\n    - acme/pkgB\n"
	}
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	localKey = localModulesKey(resolveLocalSourceAbs("./dep-pkg"))
	localModDir := filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))
	if err := os.MkdirAll(localModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	aManifest := "name: dep-pkg\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/transitive-of-a\n  mcp:\n    - name: srvA\n      registry: false\n      transport: stdio\n      command: srvA-server\n"
	if err := os.WriteFile(filepath.Join(localModDir, "apm.yml"), []byte(aManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	xModDir := filepath.Join(dir, "apm_modules", "acme", "transitive-of-a")
	if err := os.MkdirAll(xModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xModDir, "apm.yml"), []byte("name: transitive-of-a\nversion: \"1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bModDir := filepath.Join(dir, "apm_modules", "acme", "pkgB")
	if err := os.MkdirAll(bModDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// B also declares X as its own dependency -- a REAL diamond (both A and B
	// depend on X), not just a single-parent ResolvedBy pointer. This proves
	// the fix isn't merely "ignore ResolvedBy": X must survive precisely
	// because A's translated key is walked by reachableFromRemainingRoots,
	// even though B (which also legitimately depends on X) is being removed.
	bManifest := "name: pkgB\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/transitive-of-a\n  mcp:\n    - name: srvB\n      registry: false\n      transport: stdio\n      command: srvB-server\n"
	if err := os.WriteFile(filepath.Join(bModDir, "apm.yml"), []byte(bManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	bHash := writeUninstallDeployedFile(t, dir, ".claude/rules/pkgB.md", "pkgB rule")

	mcpJSON := `{"mcpServers":{"srvA":{"type":"stdio","command":"srvA-server"},"srvB":{"type":"stdio","command":"srvB-server"}}}`
	if err := os.WriteFile(filepath.Join(dir, ".mcp.json"), []byte(mcpJSON), 0644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{RepoURL: localKey, Source: "git"},
			{RepoURL: "acme/pkgB", Source: "git", DeployedFiles: []string{".claude/rules/pkgB.md"}, DeployedHashes: map[string]string{".claude/rules/pkgB.md": bHash}},
			// Diamond: X's recorded single parent is B (the root being
			// removed) even though A also depends on it -- only the
			// reachableFromRemainingRoots BFS, seeded from A's translated
			// key, can save it.
			{RepoURL: "acme/transitive-of-a", Source: "git", ResolvedBy: "acme/pkgB"},
		},
		MCPServers: []string{"srvA", "srvB"},
	}
	writeUninstallLockfileFixture(t, lock)

	return dir, localKey
}

// captureUninstallStdout redirects os.Stdout for the duration of fn and
// returns everything written to it, so a test can inspect runUninstall's
// printed --dry-run plan.
func captureUninstallStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// captureUninstallStderr redirects os.Stderr for the duration of fn and
// returns everything written to it, so a test can inspect runUninstall's
// "[!] ..." warnings (e.g. deploy.RemoveDeployedFiles diagnostics, stale-MCP
// reverse-removal notices) without those warnings only being eyeballed in a
// -v transcript.
func captureUninstallStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// snapshotDir reads every regular file under dir (recursively) into a
// path->bytes map, relative to dir, so a test can prove a whole module tree
// is byte-identical before and after an operation (e.g. --dry-run) rather
// than merely stat-ing that its root directory still exists.
func snapshotDir(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	snap := map[string][]byte{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		rel, rerr := filepath.Rel(dir, path)
		if rerr != nil {
			return rerr
		}
		snap[filepath.ToSlash(rel)] = data
		return nil
	})
	if err != nil {
		t.Fatalf("snapshotDir(%s): %v", dir, err)
	}
	return snap
}

// assertSnapshotsEqual fails the test if before and after (as produced by
// snapshotDir) differ in either file set or byte content.
func assertSnapshotsEqual(t *testing.T, label string, before, after map[string][]byte) {
	t.Helper()
	if len(before) != len(after) {
		t.Errorf("%s: file set changed, before=%v after=%v", label, keysOf(before), keysOf(after))
		return
	}
	for k, v := range before {
		av, ok := after[k]
		if !ok {
			t.Errorf("%s: %s missing after the operation", label, k)
			continue
		}
		if string(v) != string(av) {
			t.Errorf("%s: %s bytes changed, before=%q after=%q", label, k, v, av)
		}
	}
}

func keysOf(m map[string][]byte) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestRunUninstall_SurvivingLocalRootProtectsDiamondTransitive is this
// task's (07-11-local-root-key-space) end-to-end reproduction of "bad
// consequence A" from research/local-root-key-space-gap.md #3: uninstalling
// an unrelated git root (B) must not misclassify the diamond-shared
// transitive dependency X as an orphan just because ITS OWN recorded
// ResolvedBy happens to be B -- the surviving local root A still depends on
// it too, and only a correctly-keyed reachableFromRemainingRoots BFS can see
// that.
func TestRunUninstall_SurvivingLocalRootProtectsDiamondTransitive(t *testing.T) {
	dir, localKey := setupSurvivingLocalRootFixture(t, false)

	stdout := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{"acme/pkgB"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})

	// The real-run summary must report exactly the one targeted removal (B)
	// and zero transitive orphans: if X were still misclassified as an
	// orphan, printUninstallSummary would report "(+1 transitive orphan(s))"
	// instead. Neither the surviving local root nor the diamond-shared
	// transitive dependency's key may ever surface in the summary.
	if !strings.Contains(stdout, "Removed 1 package(s)") || strings.Contains(stdout, "transitive orphan") {
		t.Errorf("expected the summary to report exactly 1 removed package and 0 transitive orphans, got:\n%s", stdout)
	}
	if strings.Contains(stdout, localKey) || strings.Contains(stdout, "transitive-of-a") {
		t.Errorf("expected the summary to never mention the surviving local root %q or the diamond-shared transitive dependency, got:\n%s", localKey, stdout)
	}

	if _, err := os.Stat(filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))); err != nil {
		t.Errorf("expected the surviving local root's module dir to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "transitive-of-a")); err != nil {
		t.Errorf("expected the diamond-shared transitive dependency to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "pkgB")); !os.IsNotExist(err) {
		t.Errorf("expected acme/pkgB's module dir to be removed, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "rules", "pkgB.md")); !os.IsNotExist(err) {
		t.Errorf("expected acme/pkgB's deployed file to be removed, stat err=%v", err)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive (A and X still locked): %v", err)
	}
	lockNode, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	if newLock.FindByKey(localKey) == nil {
		t.Error("expected the local root's lockfile entry to survive")
	}
	if newLock.FindByKey("acme/transitive-of-a") == nil {
		t.Error("expected the diamond-shared transitive dependency's lockfile entry to survive")
	}
	if newLock.FindByKey("acme/pkgB") != nil {
		t.Error("expected acme/pkgB's lockfile entry to be removed")
	}

	fx := readManifestParsed(t)
	foundLocal, foundB := false, false
	for _, d := range fx.ParsedDeps {
		if d.IsLocal {
			foundLocal = true
		}
		if d.RepoURL == "acme/pkgB" {
			foundB = true
		}
	}
	if !foundLocal {
		t.Error("expected the local root's apm.yml entry to survive")
	}
	if foundB {
		t.Error("expected acme/pkgB's apm.yml entry to be removed")
	}
}

// TestRunUninstall_SurvivingLocalRootMCPServerSurvives is this task's
// (07-11-local-root-key-space) end-to-end reproduction of "bad consequence
// B" from research/local-root-key-space-gap.md #4: computeUninstallStaleMCP
// looks up dep.UniqueKey() (the real "_local/..." key) in remainingRootKeys
// -- if that map still carries the untranslated "local:<path>" identity, the
// lookup always misses, and the surviving local root's own MCP server gets
// misjudged stale and reverse-removed even though it was never touched.
func TestRunUninstall_SurvivingLocalRootMCPServerSurvives(t *testing.T) {
	dir, _ := setupSurvivingLocalRootFixture(t, false)

	stderr := captureUninstallStderr(t, func() {
		if err := runUninstall([]string{"acme/pkgB"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})
	if strings.Contains(stderr, "srvA") {
		t.Errorf("expected no stale/removal warning mentioning the surviving local root's own MCP server srvA, got stderr:\n%s", stderr)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers, _ := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["srvA"]; !ok {
		t.Errorf("expected the surviving local root's own MCP server srvA to survive in .mcp.json, got %v", servers)
	}
	if _, ok := servers["srvB"]; ok {
		t.Errorf("expected the removed acme/pkgB's own MCP server srvB to be reverse-removed as stale, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive: %v", err)
	}
	lockNode, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	foundA, foundB := false, false
	for _, s := range newLock.MCPServers {
		if s == "srvA" {
			foundA = true
		}
		if s == "srvB" {
			foundB = true
		}
	}
	if !foundA {
		t.Errorf("expected lock.MCPServers to still contain srvA, got %v", newLock.MCPServers)
	}
	if foundB {
		t.Errorf("expected lock.MCPServers to no longer contain srvB, got %v", newLock.MCPServers)
	}
}

// TestRunUninstall_SurvivingLocalRootDryRunKeepsSharedTransitive proves
// --dry-run's preview (which reuses prepareUninstallPlan's already-corrected
// orphan set) reflects the same fix as the real run, and makes zero changes
// on disk either way.
func TestRunUninstall_SurvivingLocalRootDryRunKeepsSharedTransitive(t *testing.T) {
	dir, localKey := setupSurvivingLocalRootFixture(t, false)

	beforeManifest, _ := os.ReadFile("apm.yml")
	beforeLock, _ := os.ReadFile("apm.lock.yaml")
	beforeMCP, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))

	// Snapshot every module tree involved -- A (surviving local root), B
	// (being removed) and X (diamond-shared transitive) -- BEFORE the
	// dry-run, so a byte-identical comparison after can prove --dry-run
	// wrote nothing anywhere, not just that A/X's root directories still
	// exist. B in particular is not otherwise stat-checked by this test, so
	// without this snapshot a --dry-run that actually deleted B's tree would
	// go unnoticed.
	aModDir := filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))
	bModDir := filepath.Join(dir, "apm_modules", "acme", "pkgB")
	xModDir := filepath.Join(dir, "apm_modules", "acme", "transitive-of-a")
	beforeA := snapshotDir(t, aModDir)
	beforeB := snapshotDir(t, bModDir)
	beforeX := snapshotDir(t, xModDir)

	stdout := captureUninstallStdout(t, func() {
		if err := runUninstall([]string{"acme/pkgB"}, uninstallOptions{DryRun: true}); err != nil {
			t.Fatalf("runUninstall --dry-run: %v", err)
		}
	})

	if !strings.Contains(stdout, "acme/pkgB") {
		t.Errorf("expected the dry-run plan to list acme/pkgB, got:\n%s", stdout)
	}
	if strings.Contains(stdout, localKey) {
		t.Errorf("expected the dry-run plan to NOT list the surviving local root %q as removed/orphaned, got:\n%s", localKey, stdout)
	}
	if strings.Contains(stdout, "acme/transitive-of-a") {
		t.Errorf("expected the dry-run plan to NOT list the diamond-shared transitive dependency as removed/orphaned, got:\n%s", stdout)
	}
	// The output must be the preview/plan style, never the success/deletion
	// style -- proves this is genuinely a --dry-run codepath, not a real run
	// that happens to have deleted nothing.
	if !strings.Contains(stdout, "[dry-run]") {
		t.Errorf("expected --dry-run output to be the preview/plan style, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[+] Removed") {
		t.Errorf("expected --dry-run output to never contain the real-run success summary, got:\n%s", stdout)
	}

	afterManifest, _ := os.ReadFile("apm.yml")
	afterLock, _ := os.ReadFile("apm.lock.yaml")
	afterMCP, _ := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if string(beforeManifest) != string(afterManifest) {
		t.Error("expected apm.yml to be unchanged by --dry-run")
	}
	if string(beforeLock) != string(afterLock) {
		t.Error("expected apm.lock.yaml to be unchanged by --dry-run")
	}
	if string(beforeMCP) != string(afterMCP) {
		t.Error("expected .mcp.json to be unchanged by --dry-run")
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))); err != nil {
		t.Errorf("expected the local root's module dir to survive --dry-run, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "transitive-of-a")); err != nil {
		t.Errorf("expected the transitive dependency's module dir to survive --dry-run, stat err=%v", err)
	}
	assertSnapshotsEqual(t, "surviving local root A's module tree", beforeA, snapshotDir(t, aModDir))
	assertSnapshotsEqual(t, "B's module tree (--dry-run must not delete it either)", beforeB, snapshotDir(t, bModDir))
	assertSnapshotsEqual(t, "diamond-shared transitive X's module tree", beforeX, snapshotDir(t, xModDir))
}

// TestRunUninstall_SurvivingLocalDevRootProtectsTransitiveAndMCP proves the
// fix covers devDependencies.apm local roots too -- uninstallRemainingRootKeys
// runs the identical addRemaining closure over m.ParsedDevDeps, so no
// separate code path exists to miss.
func TestRunUninstall_SurvivingLocalDevRootProtectsTransitiveAndMCP(t *testing.T) {
	dir, localKey := setupSurvivingLocalRootFixture(t, true)

	if err := runUninstall([]string{"acme/pkgB"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "apm_modules", filepath.FromSlash(localKey))); err != nil {
		t.Errorf("expected the surviving devDependencies local root's module dir to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "transitive-of-a")); err != nil {
		t.Errorf("expected the diamond-shared transitive dependency to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apm_modules", "acme", "pkgB")); !os.IsNotExist(err) {
		t.Errorf("expected acme/pkgB's module dir to be removed, stat err=%v", err)
	}

	mcpData, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mcpRoot map[string]any
	if err := json.Unmarshal(mcpData, &mcpRoot); err != nil {
		t.Fatal(err)
	}
	servers, _ := mcpRoot["mcpServers"].(map[string]any)
	if _, ok := servers["srvA"]; !ok {
		t.Errorf("expected the surviving devDependencies local root's own MCP server srvA to survive, got %v", servers)
	}
	if _, ok := servers["srvB"]; ok {
		t.Errorf("expected the removed acme/pkgB's own MCP server srvB to be reverse-removed as stale, got %v", servers)
	}

	lockData, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatalf("expected apm.lock.yaml to survive: %v", err)
	}
	lockNode, err := yamlcore.SafeLoad(lockData)
	if err != nil {
		t.Fatal(err)
	}
	newLock, err := lockfile.ParseLockfile(lockNode)
	if err != nil {
		t.Fatal(err)
	}
	if newLock.FindByKey(localKey) == nil {
		t.Error("expected the devDependencies local root's lockfile entry to survive")
	}
	if newLock.FindByKey("acme/transitive-of-a") == nil {
		t.Error("expected the diamond-shared transitive dependency's lockfile entry to survive")
	}
	if newLock.FindByKey("acme/pkgB") != nil {
		t.Error("expected acme/pkgB's lockfile entry to be removed")
	}
	foundLockA, foundLockB := false, false
	for _, s := range newLock.MCPServers {
		if s == "srvA" {
			foundLockA = true
		}
		if s == "srvB" {
			foundLockB = true
		}
	}
	if !foundLockA {
		t.Errorf("expected lock.MCPServers to still contain srvA, got %v", newLock.MCPServers)
	}
	if foundLockB {
		t.Errorf("expected lock.MCPServers to no longer contain srvB, got %v", newLock.MCPServers)
	}

	fx := readManifestParsed(t)
	foundLocalDev, foundB := false, false
	for _, d := range fx.ParsedDevDeps {
		if d.IsLocal {
			foundLocalDev = true
		}
	}
	for _, d := range fx.ParsedDeps {
		if d.RepoURL == "acme/pkgB" {
			foundB = true
		}
	}
	if foundB {
		t.Error("expected acme/pkgB's apm.yml entry to be removed")
	}
	if !foundLocalDev {
		t.Error("expected the devDependencies.apm local entry to survive")
	}
}
