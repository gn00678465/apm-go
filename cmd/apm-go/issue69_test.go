package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// Issue #69: a dependency's `targets:` limits it to the active targets it
// names. Every wanted tree, lock value and line is what Oracle 0.33.0 wrote
// and printed for the same project (~/.cache/apm-go-verify/issue69), with
// apm-go's status symbols. The MCP config files are left out of the trees:
// apm-go names Copilot's .github/mcp-config.json, the Oracle .github/mcp.json,
// with or without targets:.

const (
	issue69SkillYML = "name: my-local-skill\nversion: 1.0.0\n"
	issue69SkillMD  = "---\nname: my-local-skill\ndescription: d\n---\nx\n"
)

// issue69SkillProject is the project of the issue: a local skill package and
// an apm.yml for claude and copilot whose entry carries targets.
func issue69SkillProject(t *testing.T, targets string) {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "skills/my-local-skill/apm.yml", issue69SkillYML)
	writeCompileFile(t, dir, "skills/my-local-skill/SKILL.md", issue69SkillMD)
	writeCompileFile(t, dir, "apm.yml", "name: repro\nversion: 1.0.0\ntargets:\n  - claude\n  - copilot\ndependencies:\n  apm:\n    - path: ./skills/my-local-skill\n      targets: "+targets+"\n")
}

// issue69AgentProject is agent.sh's package ./dep (an agent, an instruction
// and an MCP server) with apm.yml's dependency entry ending in entryTail.
func issue69AgentProject(t *testing.T, entryTail string) string {
	t.Helper()
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "dep/apm.yml", "name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n    - name: depmcp\n      registry: false\n      transport: stdio\n      command: echo\n")
	writeCompileFile(t, dir, "dep/.apm/agents/dagent.agent.md", "---\nname: dagent\ndescription: d\n---\nD\n")
	writeCompileFile(t, dir, "dep/.apm/instructions/di.instructions.md", "---\napplyTo: \"**\"\n---\nDI\n")
	issue69SetEntry(t, entryTail)
	return dir
}

func issue69SetEntry(t *testing.T, entryTail string) {
	t.Helper()
	writeCompileFile(t, ".", "apm.yml", "name: repro\nversion: 1.0.0\ntargets:\n  - claude\n  - copilot\ndependencies:\n  apm:\n    - path: ./dep\n"+entryTail)
}

// issue69Deployed lists the files below the three target roots, MCP config
// files left out.
func issue69Deployed(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, root := range []string{".agents", ".claude", ".github"} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}
			if !d.IsDir() && p != filepath.Join(".github", "mcp-config.json") {
				files = append(files, filepath.ToSlash(p))
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	slices.Sort(files)
	return files
}

func issue69Lock(t *testing.T) *lockfile.Lockfile {
	t.Helper()
	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		t.Fatal(err)
	}
	lf, err := lockfile.ParseLockfile(doc)
	if err != nil {
		t.Fatal(err)
	}
	return lf
}

func issue69LockDep(t *testing.T, repoURL string) lockfile.LockedDep {
	t.Helper()
	lf := issue69Lock(t)
	for _, d := range lf.Dependencies {
		if d.RepoURL == repoURL {
			return d
		}
	}
	t.Fatalf("lock has no %s", repoURL)
	return lockfile.LockedDep{}
}

// issue69OracleLock reads an Oracle lock fixture; call it before chdirTemp.
func issue69OracleLock(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "internal", "lockfile", "testdata", "oracle", name+".lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// assertIssue69Lock compares apm.lock.yaml with want, apm_version aside.
func assertIssue69Lock(t *testing.T, want string) {
	t.Helper()
	got, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	const version = "apm_version: X"
	if g, w := issue50APMVersion.ReplaceAllString(string(got), version), issue50APMVersion.ReplaceAllString(want, version); g != w {
		t.Errorf("apm.lock.yaml:\n%s\nwant the Oracle's:\n%s", g, w)
	}
}

func assertIssue69Deployed(t *testing.T, want ...string) {
	t.Helper()
	if got := issue69Deployed(t); !slices.Equal(got, want) {
		t.Errorf("deployed files = %q\nwant %q", got, want)
	}
}

func TestInstall_Issue69_IssueReproLock(t *testing.T) {
	want := issue69OracleLock(t, "local-targets")
	issue69SkillProject(t, "[claude]")

	mustRunCLI(t, "install")

	assertIssue69Deployed(t, ".claude/skills/my-local-skill/SKILL.md", ".claude/skills/my-local-skill/apm.yml")
	assertIssue69Lock(t, want)
}

func TestInstall_Issue69_TargetsIntersectActiveTargets(t *testing.T) {
	for _, tc := range []struct {
		name, targets string
		deployed      []string
		subset        []string
	}{
		{"y8 copilot", "[copilot]", []string{".agents/skills/my-local-skill/SKILL.md", ".agents/skills/my-local-skill/apm.yml"}, []string{"copilot"}},
		{"y3 inactive codex", "[claude, codex]", []string{".claude/skills/my-local-skill/SKILL.md", ".claude/skills/my-local-skill/apm.yml"}, []string{"claude", "codex"}},
		{"y4 spelling", "[' Claude ', claude]", []string{".claude/skills/my-local-skill/SKILL.md", ".claude/skills/my-local-skill/apm.yml"}, []string{"claude"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue69SkillProject(t, tc.targets)

			mustRunCLI(t, "install")

			assertIssue69Deployed(t, tc.deployed...)
			if got := issue69LockDep(t, "_local/my-local-skill").TargetSubset; !slices.Equal(got, tc.subset) {
				t.Errorf("target_subset = %q, want %q", got, tc.subset)
			}
		})
	}
}

func TestInstall_Issue69_NoActiveTargetWarnsAndDeploysNothing(t *testing.T) {
	wantLock := issue69OracleLock(t, "local-targets-skipped")
	issue69SkillProject(t, "[codex]")

	got := mustRunCLI(t, "install")

	want := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./skills/my-local-skill...\n" +
		" i Targets: claude, copilot  (source: apm.yml)\n" +
		"   + ./skills/my-local-skill (local)\n" +
		" i Added apm_modules/ to .gitignore\n" +
		"   ! [./skills/my-local-skill] Per-dependency targets [codex] do not overlap active install targets; skipping\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got != want {
		t.Errorf("install:\n%s\nwant:\n%s", got, want)
	}
	assertIssue69Deployed(t)
	assertIssue69Lock(t, wantLock)
}

func TestInstall_Issue69_ChangedTargetsCleanTheOtherTarget(t *testing.T) {
	for _, tc := range []struct {
		name, first, second string
		deployed            []string
		subset              []string
	}{
		{"none to claude", "", "      targets: [claude]\n",
			[]string{".claude/agents/dagent.md", ".claude/rules/di.md"}, []string{"claude"}},
		{"claude to copilot", "      targets: [claude]\n", "      targets: [copilot]\n",
			[]string{".github/agents/dagent.agent.md", ".github/instructions/di.instructions.md"}, []string{"copilot"}},
		{"claude to none", "      targets: [claude]\n", "",
			[]string{".claude/agents/dagent.md", ".claude/rules/di.md", ".github/agents/dagent.agent.md", ".github/instructions/di.instructions.md"}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			issue69AgentProject(t, tc.first)
			mustRunCLI(t, "install")
			issue69SetEntry(t, tc.second)

			mustRunCLI(t, "install")

			assertIssue69Deployed(t, tc.deployed...)
			dep := issue69LockDep(t, "_local/dep")
			if !slices.Equal(dep.DeployedFiles, tc.deployed) {
				t.Errorf("deployed_files = %q, want %q", dep.DeployedFiles, tc.deployed)
			}
			if !slices.Equal(dep.TargetSubset, tc.subset) {
				t.Errorf("target_subset = %q, want %q", dep.TargetSubset, tc.subset)
			}
		})
	}
}

func TestInstall_Issue69_FrozenRestoresOnlyAllowedTarget(t *testing.T) {
	dir := issue69AgentProject(t, "      targets: [claude]\n")
	mustRunCLI(t, "install")
	if err := os.RemoveAll(filepath.Join(dir, ".claude")); err != nil {
		t.Fatal(err)
	}

	if got := mustRunCLI(t, "install", "--frozen"); got != " + Frozen install: all dependencies pinned and verified\n" {
		t.Errorf("frozen install:\n%s", got)
	}

	assertIssue69Deployed(t, ".claude/agents/dagent.md", ".claude/rules/di.md")
}

func TestInstall_Issue69_InvalidTargetsWriteNothing(t *testing.T) {
	issue69SkillProject(t, "[claud]")

	stdout, _, code := runCompileCLI(t, "install")

	if code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(stdout, "Unknown target 'claud'. Valid targets: agent-skills, antigravity, claude, codex, copilot, copilot-app, copilot-cowork, cursor, gemini, grok-build, grok-cloud, hermes, kiro, openclaw, opencode, windsurf. Did you mean 'claude'?") {
		t.Errorf("stdout lacks the Oracle's reason:\n%s", stdout)
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !slices.Equal(names, []string{"apm.yml", "skills"}) {
		t.Errorf("project holds %q, want only apm.yml and the package source", names)
	}
}

// Uninstalling dep1 hands its claude file over to dep2, which allows only
// claude; its copilot file goes. The hand-over is apm-go's recorded
// deviation (PRODUCT.md, uninstall); Oracle 0.33.0 deletes both files.
func TestUninstall_Issue69_HandOverOnlyToAllowedTarget(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := chdirTemp(t)
	for _, n := range []string{"dep1", "dep2"} {
		writeCompileFile(t, dir, n+"/apm.yml", "name: "+n+"\nversion: 1.0.0\n")
		writeCompileFile(t, dir, n+"/.apm/agents/dagent.agent.md", "---\nname: dagent\ndescription: d\n---\nfrom "+n+"\n")
	}
	writeCompileFile(t, dir, "apm.yml", "name: repro\nversion: 1.0.0\ntargets: [claude, copilot]\ndependencies:\n  apm:\n    - path: ./dep1\n    - path: ./dep2\n      targets: [claude]\n")
	mustRunCLI(t, "install")

	mustRunCLI(t, "uninstall", "./dep1")

	assertIssue69Deployed(t, ".claude/agents/dagent.md")
	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "agents", "dagent.md")); !strings.HasSuffix(got, "from dep2\n") {
		t.Errorf(".claude/agents/dagent.md = %q, want dep2's agent", got)
	}
	dep := issue69LockDep(t, "_local/dep2")
	if !slices.Equal(dep.DeployedFiles, []string{".claude/agents/dagent.md"}) || !slices.Equal(dep.TargetSubset, []string{"claude"}) {
		t.Errorf("dep2 lock entry: deployed_files %q, target_subset %q", dep.DeployedFiles, dep.TargetSubset)
	}
}

// issue69GitLoader puts a copy of the checkout of the repository a reference
// names at apm_modules/<repo URL>, as the real loader does by cloning, and
// returns the package's apm.yml. A local package goes to the real loader.
type issue69GitLoader struct {
	t         *testing.T
	checkouts map[string]string
}

func (l *issue69GitLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	checkout, ok := l.checkouts[ref.RepoURL]
	if !ok {
		return (&gitops.RealPackageLoader{ModulesDir: "apm_modules"}).LoadPackage(ref, resolvedRef)
	}
	dest := filepath.Join("apm_modules", filepath.FromSlash(ref.RepoURL))
	if _, err := os.Stat(dest); err != nil {
		if err := os.CopyFS(dest, os.DirFS(checkout)); err != nil {
			l.t.Fatal(err)
		}
	}
	data, err := os.ReadFile(filepath.Join(checkout, "apm.yml"))
	if err != nil {
		l.t.Fatal(err)
	}
	doc, err := yamlcore.SafeLoad(data)
	if err != nil {
		l.t.Fatal(err)
	}
	m, _, err := manifest.ParseManifest(doc)
	return m, err
}

func issue69Checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for rel, content := range files {
		writeCompileFile(t, dir, rel, content)
	}
	gitCmd(t, dir, "init")
	gitCmd(t, dir, "config", "user.name", "test")
	gitCmd(t, dir, "config", "user.email", "test@test.com")
	gitCmd(t, dir, "add", ".")
	gitCmd(t, dir, "commit", "-m", "init")
	return dir
}

// g1: a git package for claude whose own apm.yml declares a git package for
// copilot. The Oracle deploys each to its own target and records both
// subsets; a frozen install that rebuilds the transitive one from the lock
// keeps it on copilot.
func TestInstall_Issue69_TransitiveTargetsAndFrozen(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	loader := &issue69GitLoader{t: t, checkouts: map[string]string{
		"acme/a": issue69Checkout(t, map[string]string{
			"apm.yml":                     "name: a\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/b\n      ref: main\n      targets: [copilot]\n",
			".apm/agents/aagent.agent.md": "---\nname: aagent\ndescription: d\n---\nA\n",
		}),
		"acme/b": issue69Checkout(t, map[string]string{
			"apm.yml":                     "name: b\nversion: 1.0.0\n",
			".apm/agents/bagent.agent.md": "---\nname: bagent\ndescription: d\n---\nB\n",
		}),
	}}
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: repro\nversion: 1.0.0\ntargets: [claude, copilot]\ndependencies:\n  apm:\n    - git: acme/a\n      ref: main\n      targets: [claude]\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, false, "", "", nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})

	assertIssue69Deployed(t, ".claude/agents/aagent.md", ".github/agents/bagent.agent.md")
	if got := issue69LockDep(t, "acme/a").TargetSubset; !slices.Equal(got, []string{"claude"}) {
		t.Errorf("acme/a target_subset = %q", got)
	}
	if got := issue69LockDep(t, "acme/b").TargetSubset; !slices.Equal(got, []string{"copilot"}) {
		t.Errorf("acme/b target_subset = %q", got)
	}

	for _, root := range []string{".claude", ".github"} {
		if err := os.RemoveAll(filepath.Join(dir, root)); err != nil {
			t.Fatal(err)
		}
	}
	var err error
	out := captureUninstallStdout(t, func() {
		err = runInstall(deps, true, false, "", "", nil, nil)
	})
	if err != nil {
		t.Fatalf("frozen install: %v\n%s", err, out)
	}
	assertIssue69Deployed(t, ".claude/agents/aagent.md", ".github/agents/bagent.agent.md")
}

// The hand-over trial of uninstall rebuilds a transitive package from the
// lock: acme/b, which acme/a declares for claude only, takes over the claude
// file of the removed ./dep1 and not its copilot file.
func TestUninstall_Issue69_TransitiveHandOverKeepsLockedTargets(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	const agent = "---\nname: bagent\ndescription: d\n---\nfrom %s\n"
	loader := &issue69GitLoader{t: t, checkouts: map[string]string{
		"acme/a": issue69Checkout(t, map[string]string{
			"apm.yml": "name: a\nversion: 1.0.0\ndependencies:\n  apm:\n    - git: acme/b\n      ref: main\n      targets: [claude]\n",
		}),
		"acme/b": issue69Checkout(t, map[string]string{
			"apm.yml":                     "name: b\nversion: 1.0.0\n",
			".apm/agents/bagent.agent.md": strings.Replace(agent, "%s", "b", 1),
		}),
	}}
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "dep1/apm.yml", "name: dep1\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "dep1/.apm/agents/bagent.agent.md", strings.Replace(agent, "%s", "dep1", 1))
	writeCompileFile(t, dir, "apm.yml", "name: repro\nversion: 1.0.0\ntargets: [claude, copilot]\ndependencies:\n  apm:\n    - path: ./dep1\n    - git: acme/a\n      ref: main\n")
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: loader}
	captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, false, "", "", nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	assertIssue69Deployed(t, ".claude/agents/bagent.md", ".github/agents/bagent.agent.md")

	mustRunCLI(t, "uninstall", "./dep1")

	assertIssue69Deployed(t, ".claude/agents/bagent.md")
	if got := globalStaleRead(t, filepath.Join(dir, ".claude", "agents", "bagent.md")); !strings.HasSuffix(got, "from b\n") {
		t.Errorf(".claude/agents/bagent.md = %q, want acme/b's agent", got)
	}
	dep := issue69LockDep(t, "acme/b")
	if !slices.Equal(dep.DeployedFiles, []string{".claude/agents/bagent.md"}) || !slices.Equal(dep.TargetSubset, []string{"claude"}) {
		t.Errorf("acme/b lock entry: deployed_files %q, target_subset %q", dep.DeployedFiles, dep.TargetSubset)
	}
}

// s1-or: copilot and codex share .agents/skills/sk. The Oracle keeps dep1's
// copy and records it under dep1, so the frozen install that follows finds
// the bytes the lock records.
func TestInstall_Issue69_SharedSkillDirThenFrozen(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	dir := chdirTemp(t)
	for _, n := range []string{"dep1", "dep2"} {
		writeCompileFile(t, dir, n+"/apm.yml", "name: "+n+"\nversion: 1.0.0\n")
		writeCompileFile(t, dir, n+"/.apm/skills/sk/SKILL.md", "---\nname: sk\ndescription: d\n---\nfrom "+n+"\n")
	}
	writeCompileFile(t, dir, "apm.yml", "name: repro\nversion: 1.0.0\ntargets: [copilot, codex]\ndependencies:\n  apm:\n    - path: ./dep1\n      targets: [copilot]\n    - path: ./dep2\n")

	mustRunCLI(t, "install")

	assertIssue69Deployed(t, ".agents/skills/sk/SKILL.md")
	if got := globalStaleRead(t, filepath.Join(dir, ".agents", "skills", "sk", "SKILL.md")); !strings.HasSuffix(got, "from dep1\n") {
		t.Errorf(".agents/skills/sk/SKILL.md = %q, want dep1's skill", got)
	}
	dep1 := issue69LockDep(t, "_local/dep1")
	if !slices.Equal(dep1.DeployedFiles, []string{".agents/skills/sk", ".agents/skills/sk/SKILL.md"}) ||
		dep1.DeployedHashes[".agents/skills/sk/SKILL.md"] != "sha256:bd5850cc21c98a0cb25805de652cc5692570d5c1be6381d417d92a3971a09c1f" {
		t.Errorf("dep1 lock entry: deployed_files %q, hashes %v", dep1.DeployedFiles, dep1.DeployedHashes)
	}
	if dep2 := issue69LockDep(t, "_local/dep2"); len(dep2.DeployedFiles) != 0 {
		t.Errorf("dep2 deployed_files = %q, want none", dep2.DeployedFiles)
	}

	if got := mustRunCLI(t, "install", "--frozen"); got != " + Frozen install: all dependencies pinned and verified\n" {
		t.Errorf("frozen install:\n%s", got)
	}
}

// A registry package declared with targets: [claude] in a project for claude
// and copilot deploys to .claude/ only and records the list.
func TestRegistryInstall_Issue69_TargetsLimitDeploy(t *testing.T) {
	enableRegistries(t)
	t.Setenv("HOME", t.TempDir())
	fx := buildRegistryFixture(t)
	sum := sha256.Sum256(fx)
	srv := startE2EServer(t, fx, "sha256:"+hex.EncodeToString(sum[:]))
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", fmt.Sprintf("name: consumer\nversion: 0.1.0\ntargets: [claude, copilot]\nregistries:\n  local:\n    url: %s\n    insecure: true\n  default: local\ndependencies:\n  apm:\n    - id: acme/sample\n      version: 1.0.0\n      targets: [claude]\n", srv.URL))
	deps := &installDeps{tags: &mockInstallTagLister{}, loader: &mockInstallLoader{}}

	captureUninstallStdout(t, func() {
		if err := runInstall(deps, false, false, "", "", nil, nil); err != nil {
			t.Fatalf("install: %v", err)
		}
	})

	assertIssue69Deployed(t, ".claude/skills/probe/SKILL.md")
	dep := issue69LockDep(t, "acme/sample")
	if dep.Source != "registry" || !slices.Equal(dep.TargetSubset, []string{"claude"}) {
		t.Errorf("lock entry: source %q, target_subset %q", dep.Source, dep.TargetSubset)
	}
}
