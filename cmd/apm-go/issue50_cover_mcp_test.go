package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Issue #50: MCP servers in the lock, through `install --mcp`, `install` and
// `uninstall`. Every lock and transcript below is what Oracle v0.33.0 wrote
// or printed for the same steps, with apm-go's status symbols; a test says so
// where apm-go has no Oracle counterpart.

const mcpCoverManifest = "name: proj\nversion: 1.0.0\ntarget: claude\n"

// mcpCoverDepEntry is the lock's entry and deployment row for the local
// package of mcpCoverProject.
const (
	mcpCoverDepEntry = `- repo_url: _local/dep
  name: dep
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .claude/rules/dep.md
  deployed_file_hashes:
    .claude/rules/dep.md: sha256:b65d2cf2095c072332c85702c9cdcade660bba79de9a67ea5bb12c1878a7217a
  source: local
  local_path: ./dep
`
	mcpCoverDepRow = `- kind: project-relative
  target: claude
  value: .claude/rules/dep.md
  runtime: null
  scope: project
  owners:
  - ./dep
  active_owner: ./dep
  content_hash: sha256:b65d2cf2095c072332c85702c9cdcade660bba79de9a67ea5bb12c1878a7217a
`
	mcpCoverConfigOne = `  one:
    name: one
    transport: stdio
    args:
    - -y
    - foo
    registry: false
    command: npx
`
	mcpCoverConfigTwo = `  two:
    name: two
    transport: stdio
    env:
      B: '2'
      A: '1'
    args:
    - srv.js
    registry: false
    command: node
`
	// A server installed with `--mcp <name> -- npx` and nothing else.
	mcpCoverBareLock = `lockfile_version: '1'
apm_version: X
dependencies: []
deployments:
- kind: uri
  target: mcp
  value: NAME
  runtime: RUNTIME
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- NAME
mcp_configs:
  NAME:
    name: NAME
    transport: stdio
    registry: false
    command: npx
mcp_target_servers:
  RUNTIME:
  - NAME
`
)

// mcpCoverServerRow is the deployments row of one MCP server.
func mcpCoverServerRow(server, runtime string) string {
	return "- kind: uri\n  target: mcp\n  value: " + server + "\n  runtime: " + runtime +
		"\n  scope: project\n  owners:\n  - .\n  active_owner: .\n  content_hash: null\n"
}

func mcpCoverBare(server, runtime string) string {
	return strings.NewReplacer("NAME", server, "RUNTIME", runtime).Replace(mcpCoverBareLock)
}

// mcpCoverProject is a project with the local package ./dep (one rule).
func mcpCoverProject(t *testing.T, manifestYAML, depManifest string) string {
	t.Helper()
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", manifestYAML)
	writeCompileFile(t, dir, "dep/apm.yml", depManifest)
	writeCompileFile(t, dir, "dep/.apm/instructions/dep.instructions.md", legacyDepRule)
	return dir
}

// mcpCoverLock is apm.lock.yaml with the apm_version value replaced, the one
// line the two tools write differently.
func mcpCoverLock(t *testing.T) string {
	t.Helper()
	lock, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return issue50APMVersion.ReplaceAllString(string(lock), "apm_version: X")
}

func assertMCPCoverLock(t *testing.T, step, want string) {
	t.Helper()
	if got := mcpCoverLock(t); got != want {
		t.Errorf("apm.lock.yaml after %s:\n%s\nwant the Oracle's:\n%s", step, got, want)
	}
}

func mcpCoverManifestPath(t *testing.T) string {
	t.Helper()
	path, err := filepath.Abs("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	return path
}

// twoServerProject runs `install ./dep`, `install --mcp one` and
// `install --mcp two`, and returns the transcript of the last one.
func twoServerProject(t *testing.T) string {
	t.Helper()
	mcpCoverProject(t, mcpCoverManifest, "name: dep\nversion: 1.0.0\n")
	mustRunCLI(t, "install", "./dep")
	mustRunCLI(t, "install", "--mcp", "one", "--target", "claude", "--", "npx", "-y", "foo")
	return mustRunCLI(t, "install", "--mcp", "two", "--target", "claude", "--env", "B=2", "--env", "A=1", "--", "node", "srv.js")
}

// A second `install --mcp` in a project that already has a package lock adds
// its server to the lock and keeps the package entry and the first server.
func TestInstallMCP_SecondServer_AddedToExistingLock(t *testing.T) {
	got := twoServerProject(t)

	want := "+- MCP Servers (1)\n" +
		" i Targeting specific runtime: claude\n" +
		"|   >  two (self-defined, stdio)\n" +
		"|     +- Configuring for Claude...\n" +
		"Successfully configured MCP server 'two' for Claude Code\n" +
		"  + two\n" +
		"|   +  two -> Claude (configured)\n" +
		" + Configured 1 server\n" +
		" + Added MCP server 'two'\n" +
		"  transport: stdio\n" +
		"  apm.yml: " + mcpCoverManifestPath(t) + "\n"
	if got != want {
		t.Errorf("install --mcp two:\n%s\nwant:\n%s", got, want)
	}
	assertMCPCoverLock(t, "install --mcp two", "lockfile_version: '1'\napm_version: X\ndependencies:\n"+mcpCoverDepEntry+
		"deployments:\n"+mcpCoverDepRow+mcpCoverServerRow("one", "claude")+mcpCoverServerRow("two", "claude")+
		"mcp_servers:\n- one\n- two\nmcp_configs:\n"+mcpCoverConfigOne+mcpCoverConfigTwo+
		"mcp_target_servers:\n  claude:\n  - one\n  - two\n")

	const wantMCPList = `  mcp:
  - name: one
    registry: false
    transport: stdio
    command: npx
    args:
    - -y
    - foo
  - name: two
    registry: false
    transport: stdio
    command: node
    args:
    - srv.js
    env:
      B: '2'
      A: '1'
`
	if manifestBytes, err := os.ReadFile("apm.yml"); err != nil || !strings.HasSuffix(string(manifestBytes), wantMCPList) {
		t.Errorf("apm.yml (err %v):\n%s\nwant it to end with:\n%s", err, manifestBytes, wantMCPList)
	}
	const wantMCPJSON = `{
  "mcpServers": {
    "one": {
      "type": "stdio",
      "command": "npx",
      "args": [
        "-y",
        "foo"
      ]
    },
    "two": {
      "type": "stdio",
      "command": "node",
      "env": {
        "B": "2",
        "A": "1"
      },
      "args": [
        "srv.js"
      ]
    }
  }
}
`
	if mcpJSON, err := os.ReadFile(".mcp.json"); err != nil || string(mcpJSON) != wantMCPJSON {
		t.Errorf(".mcp.json (err %v):\n%s\nwant:\n%s", err, mcpJSON, wantMCPJSON)
	}
}

// Uninstalling the package leaves both servers in the lock. Uninstalling a
// server by name is apm-go's own (the Oracle rejects the name): the lock is
// then the one the Oracle writes for the same project with server two alone
// (`install ./dep`, `install --mcp two`, `uninstall ./dep`).
func TestUninstall_MCPServer_LeavesLockOfRemainingServer(t *testing.T) {
	twoServerProject(t)
	manifestPath := mcpCoverManifestPath(t)

	wantPackage := " > Uninstalling 1 package(s)...\n" +
		" + _local/dep - found in apm.yml\n" +
		" i Removed _local/dep from apm_modules/\n" +
		" i Cleaned 1 stale file from ./dep\n" +
		" i Removed _local/dep from dependencies.apm in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if got := mustRunCLI(t, "uninstall", "./dep"); got != wantPackage {
		t.Errorf("uninstall ./dep:\n%s\nwant:\n%s", got, wantPackage)
	}
	assertMCPCoverLock(t, "uninstall ./dep", "lockfile_version: '1'\napm_version: X\ndependencies: []\ndeployments:\n"+
		mcpCoverServerRow("one", "claude")+mcpCoverServerRow("two", "claude")+
		"mcp_servers:\n- one\n- two\nmcp_configs:\n"+mcpCoverConfigOne+mcpCoverConfigTwo+
		"mcp_target_servers:\n  claude:\n  - one\n  - two\n")

	// The dry-run lines have the shape of the Oracle's package lines.
	wantDryRun := " > Uninstalling 1 package(s)...\n" +
		" + one - found in apm.yml\n" +
		" i Dry run: Would remove 1 package(s):\n" +
		" i   - one from apm.yml\n" +
		" + Dry run complete - no changes made\n"
	before := mcpCoverLock(t)
	if got := mustRunCLI(t, "uninstall", "one", "--dry-run"); got != wantDryRun {
		t.Errorf("uninstall one --dry-run:\n%s\nwant:\n%s", got, wantDryRun)
	}
	if after := mcpCoverLock(t); after != before {
		t.Errorf("the dry run changed apm.lock.yaml:\n%s", after)
	}

	mustRunCLI(t, "uninstall", "one")

	assertMCPCoverLock(t, "uninstall one", "lockfile_version: '1'\napm_version: X\ndependencies: []\ndeployments:\n"+
		mcpCoverServerRow("two", "claude")+
		"mcp_servers:\n- two\nmcp_configs:\n"+mcpCoverConfigTwo+
		"mcp_target_servers:\n  claude:\n  - two\n")
}

// A server a package declares is configured after the project's own, and
// leaves the lock when the package is uninstalled. The Oracle also prints a
// "Trusting direct dependency MCP" line and records the package's server in
// mcp_configs; apm-go does neither, so only the shared lines are compared
// for the install.
func TestInstall_PackageDeclaredMCPServer_RecordedAndRemovedWithPackage(t *testing.T) {
	mcpCoverProject(t,
		mcpCoverManifest+"dependencies:\n  apm:\n  - ./dep\n  mcp:\n  - name: own\n    registry: false\n    transport: stdio\n    command: node\n    args: []\n",
		"name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n  - name: depsrv\n    registry: false\n    transport: stdio\n    command: npx\n    args:\n    - -y\n    - bar\n")

	got := mustRunCLI(t, "install")

	for _, want := range []string{
		"+- MCP Servers (2)\n i Targeting declared target from apm.yml: claude\n|   >  own (self-defined, stdio)\n",
		"|   +  own -> Claude (configured)\n|   >  depsrv",
		"|   +  depsrv -> Claude (configured)\n + Configured 2 servers\n + Installed 1 APM dependency and 2 MCP servers in <DUR>.\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("install:\n%s\nwant it to contain:\n%s", got, want)
		}
	}
	const ownConfig = "  own:\n    name: own\n    transport: stdio\n    args: []\n    registry: false\n    command: node\n"
	lock := mcpCoverLock(t)
	for _, want := range []string{
		"mcp_servers:\n- depsrv\n- own\nmcp_configs:\n",
		ownConfig + "mcp_target_servers:\n  claude:\n  - depsrv\n  - own\n",
	} {
		if !strings.Contains(lock, want) {
			t.Errorf("apm.lock.yaml:\n%s\nwant it to contain:\n%s", lock, want)
		}
	}

	mustRunCLI(t, "uninstall", "./dep")

	assertMCPCoverLock(t, "uninstall ./dep", "lockfile_version: '1'\napm_version: X\ndependencies: []\ndeployments:\n"+
		mcpCoverServerRow("own", "claude")+
		"mcp_servers:\n- own\nmcp_configs:\n"+ownConfig+
		"mcp_target_servers:\n  claude:\n  - own\n")
}

// When the package's server was the only thing left to record, uninstalling
// the package removes the lock.
func TestUninstall_PackageWithOnlyMCPServer_RemovesLock(t *testing.T) {
	mcpCoverProject(t, mcpCoverManifest+"dependencies:\n  apm:\n  - ./dep\n",
		"name: dep\nversion: 1.0.0\ndependencies:\n  mcp:\n  - name: depsrv\n    registry: false\n    transport: stdio\n    command: npx\n")
	mustRunCLI(t, "install")
	if lock := readLockfile(t); len(lock.MCPTargetServers["claude"]) != 1 {
		t.Fatalf("mcp_target_servers = %v, want depsrv under claude", lock.MCPTargetServers)
	}

	mustRunCLI(t, "uninstall", "./dep")

	assertIssue50Exists(t, false, "apm.lock.yaml")
}

// A registry server keeps the version and the registry URL apm.yml gives it
// in mcp_configs, and a self-defined server its version.
func TestInstall_RegistryMCPServer_ConfigKeepsVersionAndRegistry(t *testing.T) {
	registry := mcpRegistryServer(t, map[string]mockMCPServer{
		"io.example/remote": {
			Name:    "io.example/remote",
			Remotes: []map[string]any{{"type": "streamable-http", "url": "https://remote.example.com/mcp"}},
		},
	})
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", mcpCoverManifest+"dependencies:\n  mcp:\n"+
		"  - name: io.example/remote\n    version: 1.2.3\n    registry: "+registry.URL+"\n"+
		"  - name: pinned\n    registry: false\n    transport: stdio\n    command: node\n    version: 2.0.0\n")

	mustRunCLI(t, "install")

	assertMCPCoverLock(t, "install", "lockfile_version: '1'\napm_version: X\ndependencies: []\ndeployments:\n"+
		mcpCoverServerRow("io.example/remote", "claude")+mcpCoverServerRow("pinned", "claude")+
		"mcp_servers:\n- io.example/remote\n- pinned\nmcp_configs:\n"+
		"  io.example/remote:\n    name: io.example/remote\n    version: 1.2.3\n    registry: "+registry.URL+"\n"+
		"  pinned:\n    name: pinned\n    transport: stdio\n    version: 2.0.0\n    registry: false\n    command: node\n"+
		"mcp_target_servers:\n  claude:\n  - io.example/remote\n  - pinned\n")
}

// A target that takes no MCP config is skipped and is not recorded. The
// Oracle words the skip line differently, so that line is not compared.
func TestInstallMCP_TargetWithoutMCPSupport_NotRecorded(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")

	got := mustRunCLI(t, "install", "--mcp", "one", "--target", "claude,agent-skills", "--", "npx")

	wantBlock := "+- MCP Servers (1)\n" +
		" i Targeting specific runtime: claude\n" +
		"|   >  one (self-defined, stdio)\n"
	if !strings.Contains(got, "agent-skills") || !strings.Contains(got, wantBlock) {
		t.Errorf("install --mcp one:\n%s\nwant a line that names agent-skills and:\n%s", got, wantBlock)
	}
	assertMCPCoverLock(t, "install --mcp one", mcpCoverBare("one", "claude"))
}

// A server for Codex alone leaves no .claude directory.
func TestInstallMCP_CodexTarget_LockAndNoClaudeDir(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")

	got := mustRunCLI(t, "install", "--mcp", "one", "--target", "codex", "--", "npx")

	want := "+- MCP Servers (1)\n i Targeting specific runtime: codex\n|   >  one (self-defined, stdio)\n|     +- Configuring for Codex...\n" +
		" + Configured MCP server 'one' for Codex CLI\n  + one\n" +
		"|   +  one -> Codex (configured)\n + Configured 1 server\n + Added MCP server 'one'\n  transport: stdio\n"
	if !strings.Contains(got, want) {
		t.Errorf("install --mcp one:\n%s\nwant it to contain:\n%s", got, want)
	}
	assertMCPCoverLock(t, "install --mcp one", mcpCoverBare("one", "codex"))
	assertIssue50Exists(t, false, ".claude")
	assertIssue50Exists(t, true, ".codex")
}

// The same server installed again for another target is configured there,
// not reported as already configured.
func TestInstallMCP_SameServerOtherTarget_Configured(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
	mustRunCLI(t, "install", "--mcp", "one", "--target", "claude", "--", "npx")

	got := mustRunCLI(t, "install", "--mcp", "one", "--target", "codex", "--", "npx")

	for _, want := range []string{
		"|   >  one (self-defined, stdio)\n|     +- Configuring for Codex...\n",
		"|   +  one -> Codex (configured)\n + Configured 1 server\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("install --mcp one --target codex:\n%s\nwant it to contain:\n%s", got, want)
		}
	}
	if servers := readLockfile(t).MCPTargetServers["codex"]; len(servers) != 1 || servers[0] != "one" {
		t.Errorf("mcp_target_servers.codex = %v, want [one]", servers)
	}
}

// .claude that is a file of the user's cannot become the directory; the
// server is still written to .mcp.json and the file is left alone.
func TestInstallMCP_ClaudeDirIsAFile_WarnsAndKeepsFile(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
	writeCompileFile(t, dir, ".claude", "mine\n")

	stdout, _, _ := runCompileCLI(t, "install", "--mcp", "one", "--target", "claude", "--", "npx")

	if !strings.Contains(stdout, " ! could not create .claude: ") {
		t.Errorf("want a warning for .claude:\n%s", stdout)
	}
	if got, err := os.ReadFile(".claude"); err != nil || string(got) != "mine\n" {
		t.Errorf(".claude = %q (err %v), want it unchanged", got, err)
	}
}

func TestInstallMCP_UnreadableLock_Fails(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
	const brokenLock = "dependencies: [\n"
	writeCompileFile(t, dir, "apm.lock.yaml", brokenLock)

	stdout, _, exitCode := runCompileCLI(t, "install", "--mcp", "one", "--target", "claude", "--", "npx")

	if exitCode != 1 || !strings.Contains(stdout, " x parse apm.lock.yaml: ") {
		t.Errorf("exit = %d, want 1 and a parse error for the lock:\n%s", exitCode, stdout)
	}
	if got, err := os.ReadFile("apm.lock.yaml"); err != nil || string(got) != brokenLock {
		t.Errorf("apm.lock.yaml = %q (err %v), want it unchanged", got, err)
	}
}

func TestInstallMCP_LockNotWritable_Fails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
	// Codex's config is written inside .codex, which stays writable.
	if err := os.Mkdir(".codex", 0o755); err != nil {
		t.Fatal(err)
	}
	readOnlyWorkDir(t, dir)

	stdout, _, exitCode := runCompileCLI(t, "install", "--mcp", "one", "--target", "codex", "--", "npx")

	if exitCode != 1 || !strings.Contains(stdout, " x write apm.lock.yaml: ") {
		t.Errorf("exit = %d, want 1 and a write error for the lock:\n%s", exitCode, stdout)
	}
	assertIssue50Exists(t, false, "apm.lock.yaml")
}

// A package apm.yml lists but the lock does not know has no deployed file to
// clean; the lock keeps the server it records.
func TestUninstall_PackageNotInLock_CleansNothing(t *testing.T) {
	mcpCoverProject(t, mcpCoverManifest+"dependencies:\n  apm:\n  - ./dep\n", "name: dep\nversion: 1.0.0\n")
	mustRunCLI(t, "install", "--mcp", "x", "--target", "claude", "--", "npx")

	got := mustRunCLI(t, "uninstall", "./dep")

	if strings.Contains(got, "Cleaned") || !strings.HasSuffix(got, " + Uninstall complete: Removed 1 package(s) from apm.yml\n") {
		t.Errorf("uninstall ./dep:\n%s", got)
	}
	assertMCPCoverLock(t, "uninstall ./dep", mcpCoverBare("x", "claude"))
}

func TestUninstall_DryRun_ListsTransitiveDependencies(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", mcpCoverManifest+"dependencies:\n  apm:\n  - acme/foo\n")
	const lock = "lockfile_version: '1'\ndependencies:\n" +
		"- repo_url: acme/foo\n  resolved_commit: 1111111111111111111111111111111111111111\n  depth: 1\n" +
		"- repo_url: acme/kid\n  resolved_commit: 3333333333333333333333333333333333333333\n  depth: 2\n  resolved_by: acme/foo\n" +
		"- repo_url: acme/child\n  resolved_commit: 2222222222222222222222222222222222222222\n  depth: 2\n  resolved_by: acme/foo\n"
	writeCompileFile(t, dir, "apm.lock.yaml", lock)
	for _, name := range []string{"foo", "kid", "child"} {
		writeCompileFile(t, dir, "apm_modules/acme/"+name+"/apm.yml", "name: "+name+"\nversion: 1.0.0\n")
	}

	got := mustRunCLI(t, "uninstall", "acme/foo", "--dry-run")

	want := " > Uninstalling 1 package(s)...\n" +
		" + acme/foo - found in apm.yml\n" +
		" i Dry run: Would remove 1 package(s):\n" +
		" i   - acme/foo from apm.yml\n" +
		" i   - acme/foo from apm_modules/\n" +
		" i   Transitive dependencies that would be removed:\n" +
		" i     - acme/child\n" +
		" i     - acme/kid\n" +
		" + Dry run complete - no changes made\n"
	if got != want {
		t.Errorf("uninstall acme/foo --dry-run:\n%s\nwant:\n%s", got, want)
	}
	if after, err := os.ReadFile("apm.lock.yaml"); err != nil || string(after) != lock {
		t.Errorf("apm.lock.yaml changed (err %v):\n%s", err, after)
	}
	assertIssue50Exists(t, true, "apm_modules/acme/foo", "apm_modules/acme/kid", "apm_modules/acme/child")
}

// apm_modules that is a file cannot hold a package directory and cannot be
// kept as a directory: uninstall stops with the error.
func TestUninstall_ModulesPathIsAFile_Fails(t *testing.T) {
	tests := []struct {
		name    string
		arg     string
		wantErr string
	}{
		{name: "package", arg: "./dep", wantErr: " x remove apm_modules/_local/dep: "},
		{name: "MCP server", arg: "own", wantErr: " x keep apm_modules: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := mcpCoverProject(t,
				mcpCoverManifest+"dependencies:\n  apm:\n  - ./dep\n  mcp:\n  - name: own\n    registry: false\n    transport: stdio\n    command: node\n",
				"name: dep\nversion: 1.0.0\n")
			writeCompileFile(t, dir, "apm_modules", "mine\n")

			stdout, _, exitCode := runCompileCLI(t, "uninstall", tt.arg)

			got := filepath.ToSlash(stdout)
			windowsMissingParent := runtime.GOOS == "windows" && strings.Contains(got, " x keep apm_modules: ")
			if exitCode != 1 || (!strings.Contains(got, tt.wantErr) && !windowsMissingParent) {
				t.Errorf("exit = %d, want 1 and %q:\n%s", exitCode, tt.wantErr, stdout)
			}
			if got, err := os.ReadFile("apm_modules"); err != nil || string(got) != "mine\n" {
				t.Errorf("apm_modules = %q (err %v), want it unchanged", got, err)
			}
		})
	}
}
