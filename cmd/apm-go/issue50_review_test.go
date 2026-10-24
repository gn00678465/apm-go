package main

import (
	"os"
	"slices"
	"strings"
	"testing"

	"charm.land/huh/v2"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/ux"
	"github.com/apm-go/apm/internal/yamlcore"
)

func appendToLock(t *testing.T, text string) {
	t.Helper()
	f, err := os.OpenFile("apm.lock.yaml", os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func lockOnDisk(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile("apm.lock.yaml")
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// uninstall deletes the lock only when nothing is left in it, and a
// top-level key apm-go does not model is something left. The Oracle deletes
// the lock in the second case too; keeping it is PRODUCT.md's contract.
// The lock that stays has the Oracle's layout of an empty lock
// (LockFile.to_yaml: dependencies and deployments as empty lists).
func TestUninstall_LockWithUnmodeledKey_Stays(t *testing.T) {
	t.Run("last package, nothing else in the lock", func(t *testing.T) {
		installParityFixture(t, "install-local-path-dep")
		mustRunCLI(t, "install", "./dep", "--target", "claude")
		mustRunCLI(t, "uninstall", "./dep")
		if _, err := os.Stat("apm.lock.yaml"); !os.IsNotExist(err) {
			t.Errorf("apm.lock.yaml: stat err = %v, want it removed", err)
		}
	})
	t.Run("last package, unmodeled key", func(t *testing.T) {
		installParityFixture(t, "install-local-path-dep")
		mustRunCLI(t, "install", "./dep", "--target", "claude")
		appendToLock(t, "x-review-keep: yes\n")
		mustRunCLI(t, "uninstall", "./dep")
		const want = "lockfile_version: '1'\napm_version: dev\ndependencies: []\ndeployments: []\nx-review-keep: yes\n"
		if got := lockOnDisk(t); got != want {
			t.Errorf("apm.lock.yaml = %q, want %q", got, want)
		}
	})
	t.Run("last MCP server, unmodeled key", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude")
		appendToLock(t, "x-keep: yes\n")
		mustRunCLI(t, "uninstall", "demo")
		const want = "lockfile_version: '1'\napm_version: dev\ndependencies: []\ndeployments: []\nx-keep: yes\n"
		if got := lockOnDisk(t); got != want {
			t.Errorf("apm.lock.yaml = %q, want %q", got, want)
		}
	})
}

func hasAPMVersionLine(lock string) bool {
	return strings.Contains("\n"+lock, "\napm_version: ")
}

// Whether the lock has an apm_version line follows the command and
// --no-provenance, also when nothing else in the lock changes; the value of
// an existing line is not a reason to write the lock again.
func TestInstall_NoProvenance_AppliesToExistingLock(t *testing.T) {
	t.Run("line is added back", func(t *testing.T) {
		installParityFixture(t, "install-local-path-dep")
		mustRunCLI(t, "install", "./dep", "--target", "claude", "--no-provenance")
		if lock := lockOnDisk(t); hasAPMVersionLine(lock) {
			t.Fatalf("lock after --no-provenance has apm_version:\n%s", lock)
		}
		mustRunCLI(t, "install")
		if lock := lockOnDisk(t); !hasAPMVersionLine(lock) {
			t.Errorf("lock after a plain install has no apm_version:\n%s", lock)
		}
	})
	t.Run("another value is left alone", func(t *testing.T) {
		installParityFixture(t, "install-local-path-dep")
		mustRunCLI(t, "install", "./dep", "--target", "claude")
		edited := strings.Replace(lockOnDisk(t), "apm_version: dev\n", "apm_version: 0.0.1\n", 1)
		if err := os.WriteFile("apm.lock.yaml", []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		mustRunCLI(t, "install")
		if got := lockOnDisk(t); got != edited || !strings.Contains(got, "apm_version: 0.0.1\n") {
			t.Errorf("lock changed:\n%s\nwant:\n%s", got, edited)
		}
	})
	t.Run("line is removed", func(t *testing.T) {
		installParityFixture(t, "install-local-path-dep")
		mustRunCLI(t, "install", "./dep", "--target", "claude")
		if lock := lockOnDisk(t); !hasAPMVersionLine(lock) {
			t.Fatalf("lock after a plain install has no apm_version:\n%s", lock)
		}
		repeat := mustRunCLI(t, "install")
		if got := mustRunCLI(t, "install", "--no-provenance"); got != repeat {
			t.Errorf("stdout of the install that only drops the line:\n%s\nwant that of an install that changes nothing:\n%s", got, repeat)
		}
		if lock := lockOnDisk(t); hasAPMVersionLine(lock) {
			t.Errorf("lock after --no-provenance has apm_version:\n%s", lock)
		}
	})
	t.Run("install --mcp writes the line", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude", "--no-provenance")
		if lock := lockOnDisk(t); hasAPMVersionLine(lock) {
			t.Fatalf("lock after install --mcp --no-provenance has apm_version:\n%s", lock)
		}
		mustRunCLI(t, "install", "--mcp", "two", "--target", "claude", "--", "npx")
		if lock := lockOnDisk(t); !hasAPMVersionLine(lock) {
			t.Errorf("lock after install --mcp has no apm_version:\n%s", lock)
		}
	})
	t.Run("install --mcp --no-provenance removes the line", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude")
		if lock := lockOnDisk(t); !hasAPMVersionLine(lock) {
			t.Fatalf("lock after install --mcp has no apm_version:\n%s", lock)
		}
		mustRunCLI(t, "install", "--mcp", "two", "--target", "claude", "--no-provenance", "--", "npx")
		if lock := lockOnDisk(t); hasAPMVersionLine(lock) {
			t.Errorf("lock after install --mcp --no-provenance has apm_version:\n%s", lock)
		}
	})
}

// parsedLockOnDisk parses apm.lock.yaml and reports whether the document has
// a top-level apm_version key, whatever its value or quoting.
func parsedLockOnDisk(t *testing.T) (lock *lockfile.Lockfile, hasVersionKey bool) {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(lockOnDisk(t)))
	if err != nil {
		t.Fatalf("apm.lock.yaml is not valid YAML: %v\n%s", err, lockOnDisk(t))
	}
	lock, err = lockfile.ParseLockfile(node)
	if err != nil {
		t.Fatalf("apm.lock.yaml does not parse: %v\n%s", err, lockOnDisk(t))
	}
	root := node.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "apm_version" {
			hasVersionKey = true
		}
	}
	return lock, hasVersionKey
}

// install reads the same key: an apm_version line with an empty value or a
// quoted key is a line that is there, and --no-provenance removes it.
func TestInstall_NoProvenance_RemovesAnySpellingOfTheKey(t *testing.T) {
	for _, spelling := range []string{"apm_version: ''\n", "'apm_version': dev\n"} {
		t.Run(spelling, func(t *testing.T) {
			installParityFixture(t, "install-local-path-dep")
			mustRunCLI(t, "install", "./dep", "--target", "claude")
			edited := strings.Replace(lockOnDisk(t), "apm_version: dev\n", spelling, 1)
			if err := os.WriteFile("apm.lock.yaml", []byte(edited), 0o600); err != nil {
				t.Fatal(err)
			}
			mustRunCLI(t, "install", "--no-provenance")
			if lock, hasKey := parsedLockOnDisk(t); hasKey || len(lock.Dependencies) != 1 {
				t.Errorf("apm_version key = %v, dependencies = %d; want no key and one dependency\n%s", hasKey, len(lock.Dependencies), lockOnDisk(t))
			}
		})
	}
}

// A lock of the project's own content alone keeps the apm_version line it
// had (PRODUCT.md), and it has the line when the key is there, whatever the
// value. An empty value cannot be written, so the line that stays carries
// the current version.
func TestInstall_ProjectContentLock_KeepsLineWithEmptyValue(t *testing.T) {
	for _, tc := range []struct {
		name, line string
		args       []string
		wantKey    bool
	}{
		{"empty string", "apm_version: ''\n", []string{"install"}, true},
		{"null", "apm_version:\n", []string{"install"}, true},
		{"empty string with the flag", "apm_version: ''\n", []string{"install", "--no-provenance"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\n")
			writeCompileFile(t, dir, ".apm/instructions/a.instructions.md", "---\ndescription: a\napplyTo: \"**\"\n---\nA.\n")
			mustRunCLI(t, "install")
			const head = "lockfile_version: '1'\n"
			before := lockOnDisk(t)
			if !strings.HasPrefix(before, head+"dependencies: []\n") {
				t.Fatalf("lock before:\n%s", before)
			}
			if err := os.WriteFile("apm.lock.yaml", []byte(head+tc.line+strings.TrimPrefix(before, head)), 0o600); err != nil {
				t.Fatal(err)
			}
			mustRunCLI(t, tc.args...)
			lock, hasKey := parsedLockOnDisk(t)
			if hasKey != tc.wantKey {
				t.Errorf("apm_version key = %v, want %v\n%s", hasKey, tc.wantKey, lockOnDisk(t))
			}
			if got, want := lock.LocalDeployedFiles, []string{".claude/rules/a.md"}; !slices.Equal(got, want) {
				t.Errorf("local_deployed_files = %v, want %v", got, want)
			}
		})
	}
}

const registryEntryManifest = "name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  mcp:\n  - name: acme/server\n    transport: http\n    registry: http://127.0.0.1:1\n"

// install --mcp warns about a --registry on this machine before anything
// else, as Oracle 0.33.0 does (install/mcp/registry.py:197-217).
const loopbackRegistryWarning = " ! --registry host '127.0.0.1' is loopback/private/link-local; only registry-resolved installs will reach it. Confirm this is intentional (local dev / private mirror).\n"

// The registry at 127.0.0.1:1 cannot be reached: an entry that does not
// change ends the command before any request to it.
var unchangedRegistryArgs = []string{"install", "--mcp", "acme/server", "--transport", "http", "--registry", "http://127.0.0.1:1", "--target", "claude"}

// install --mcp has paths that end with exit 0 before the lock is written.
// On the two that are not a cancelled operation the apm_version line still
// follows --no-provenance, and nothing else in the lock changes.
func TestInstallMCP_EarlyReturn_ProvenanceLineFollowsFlag(t *testing.T) {
	const unchanged = loopbackRegistryWarning + " i MCP server \"acme/server\" unchanged\n"
	const withLine = "lockfile_version: '1'\napm_version: dev\n"
	const withoutLine = "lockfile_version: '1'\n"

	t.Run("registry entry unchanged, flag removes the line", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", registryEntryManifest)
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude")
		before := lockOnDisk(t)
		if !strings.HasPrefix(before, withLine) {
			t.Fatalf("lock before:\n%s", before)
		}
		if got := mustRunCLI(t, append(unchangedRegistryArgs, "--no-provenance")...); got != unchanged {
			t.Errorf("stdout = %q, want %q", got, unchanged)
		}
		if got, want := lockOnDisk(t), withoutLine+strings.TrimPrefix(before, withLine); got != want {
			t.Errorf("lock:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("registry entry unchanged, no flag adds the line", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", registryEntryManifest)
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude", "--no-provenance")
		before := lockOnDisk(t)
		if hasAPMVersionLine(before) {
			t.Fatalf("lock before:\n%s", before)
		}
		if got := mustRunCLI(t, unchangedRegistryArgs...); got != unchanged {
			t.Errorf("stdout = %q, want %q", got, unchanged)
		}
		if got, want := lockOnDisk(t), withLine+strings.TrimPrefix(before, withoutLine); got != want {
			t.Errorf("lock:\n%s\nwant:\n%s", got, want)
		}
	})
	t.Run("no target takes the server, flag removes the line", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\n")
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude")
		before := lockOnDisk(t)
		mcpJSON, err := os.ReadFile(".mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		got := mustRunCLI(t, "install", "--mcp", "two", "--target", "agent-skills", "--no-provenance", "--", "npx")
		if want := " ! MCP server \"two\" declared in apm.yml but not deployed to any target; see diagnostics above\n"; !strings.HasSuffix(got, want) {
			t.Errorf("stdout:\n%s\nwant it to end with %q", got, want)
		}
		if got, want := lockOnDisk(t), withoutLine+strings.TrimPrefix(before, withLine); got != want {
			t.Errorf("lock:\n%s\nwant:\n%s", got, want)
		}
		if after, err := os.ReadFile(".mcp.json"); err != nil || string(after) != string(mcpJSON) {
			t.Errorf(".mcp.json = %q (err %v), want it unchanged", after, err)
		}
	})
	t.Run("line already as the flag asks, lock file is not replaced", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", registryEntryManifest)
		mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude")
		edited := strings.Replace(lockOnDisk(t), "apm_version: dev\n", "apm_version: 0.0.1\n", 1)
		if err := os.WriteFile("apm.lock.yaml", []byte(edited), 0o600); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat("apm.lock.yaml")
		if err != nil {
			t.Fatal(err)
		}
		mustRunCLI(t, unchangedRegistryArgs...)
		after, err := os.Stat("apm.lock.yaml")
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(before, after) || lockOnDisk(t) != edited {
			t.Errorf("apm.lock.yaml was written again:\n%s\nwant the same file with:\n%s", lockOnDisk(t), edited)
		}
	})
	t.Run("empty value without the flag is a line that is there", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", registryEntryManifest)
		const lock = "lockfile_version: '1'\napm_version: ''\ndependencies: []\nmcp_servers:\n- demo\n"
		writeCompileFile(t, dir, "apm.lock.yaml", lock)
		mustRunCLI(t, unchangedRegistryArgs...)
		if got := lockOnDisk(t); got != lock {
			t.Errorf("lock:\n%s\nwant it unchanged:\n%s", got, lock)
		}
	})
	t.Run("no lock, none is created", func(t *testing.T) {
		dir := chdirTemp(t)
		writeCompileFile(t, dir, "apm.yml", registryEntryManifest)
		mustRunCLI(t, unchangedRegistryArgs...)
		if _, err := os.Stat("apm.lock.yaml"); !os.IsNotExist(err) {
			t.Errorf("apm.lock.yaml: stat err = %v, want no file", err)
		}
	})
}

// Each lock below is valid YAML that apm-go reads. The early return of
// install --mcp writes the lock whole when the apm_version key has to come
// or go, so the result is valid YAML whatever the layout of the file was.
func TestInstallMCP_EarlyReturn_WritesAnyValidLockWhole(t *testing.T) {
	const unchanged = loopbackRegistryWarning + " i MCP server \"acme/server\" unchanged\n"
	tests := []struct {
		name    string
		lock    string
		flag    bool
		wantKey bool
	}{
		{
			name:    "no newline at the end, line is added",
			lock:    "lockfile_version: '1'\ndependencies: []\nmcp_servers:\n- demo\ngenerated_at: 2026-10-12",
			wantKey: true,
		},
		{
			name: "block scalar value, line is removed",
			lock: "lockfile_version: '1'\napm_version: |-\n  dev\ndependencies: []\nmcp_servers:\n- demo\n",
			flag: true,
		},
		{
			name: "empty value, line is removed",
			lock: "lockfile_version: '1'\napm_version: ''\ndependencies: []\nmcp_servers:\n- demo\n",
			flag: true,
		},
		{
			name: "quoted key, line is removed",
			lock: "lockfile_version: '1'\n'apm_version': dev\ndependencies: []\nmcp_servers:\n- demo\n",
			flag: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := chdirTemp(t)
			writeCompileFile(t, dir, "apm.yml", registryEntryManifest)
			writeCompileFile(t, dir, "apm.lock.yaml", tt.lock)
			args := unchangedRegistryArgs
			if tt.flag {
				args = append(append([]string(nil), args...), "--no-provenance")
			}
			if got := mustRunCLI(t, args...); got != unchanged {
				t.Errorf("stdout = %q, want %q", got, unchanged)
			}
			lock, hasKey := parsedLockOnDisk(t)
			if hasKey != tt.wantKey || len(lock.MCPServers) != 1 || lock.MCPServers[0] != "demo" {
				t.Errorf("apm_version key = %v, mcp_servers = %v; want key = %v and [demo]\n%s", hasKey, lock.MCPServers, tt.wantKey, lockOnDisk(t))
			}
		})
	}
}

// A lock from before issue #50 is written in the current layout when the
// key has to go; its entries keep their content, so a frozen install still
// accepts it and the conversion stays the job of the next plain install.
func TestInstallMCP_EarlyReturn_LegacyLockKeepsItsEntries(t *testing.T) {
	key := legacyLockProject(t)
	if err := os.WriteFile("apm.yml", []byte("name: proj\nversion: 1.0.0\ntarget: claude\ndependencies:\n  apm:\n    - ./dep\n  mcp:\n  - name: acme/server\n    transport: http\n    registry: http://127.0.0.1:1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRunCLI(t, append(append([]string(nil), unchangedRegistryArgs...), "--no-provenance")...)

	lock, hasKey := parsedLockOnDisk(t)
	if hasKey || len(lock.Dependencies) != 1 || lock.Dependencies[0].RepoURL != key {
		t.Fatalf("apm_version key = %v, dependencies = %+v; want no key and one entry with repo_url %s\n%s", hasKey, lock.Dependencies, key, lockOnDisk(t))
	}
	if strings.Contains(lockOnDisk(t), "generated_at") {
		t.Errorf("lock is not in the current layout:\n%s", lockOnDisk(t))
	}
	if _, stderr, exitCode := runCompileCLI(t, "install", "--frozen"); exitCode != 0 || stderr != "" {
		t.Errorf("install --frozen: exit = %d, stderr = %q", exitCode, stderr)
	}
}

// A user who answers No to replacing an entry cancelled the operation: no
// file changes, the lock included, whatever the flag says.
func TestInstallMCP_ReplacementDeclined_WritesNothing(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntarget: claude\n")
	mustRunCLI(t, "install", "--mcp", "demo", "--url", "https://example.com/mcp", "--target", "claude")
	lock := lockOnDisk(t)
	manifestBefore, err := os.ReadFile("apm.yml")
	if err != nil {
		t.Fatal(err)
	}

	// The prompt is offered on a terminal only: stdout must be a character
	// device and no CI or E2E variable may be set.
	for _, name := range []string{"APM_E2E_TESTS", "CI", "GITHUB_ACTIONS", "TRAVIS", "JENKINS_URL", "BUILDKITE"} {
		t.Setenv(name, "")
	}
	t.Cleanup(ux.SetTTYSeamsForTest(true, true, true))
	asked := 0
	t.Cleanup(ux.SetPromptSeamsForTest(func(huh.Theme, string, bool) (bool, error) {
		asked++
		return false, nil
	}, nil, nil))
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	realStdout, realStderr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = devNull, devNull
	root := buildRootCmd()
	root.SetArgs([]string{"install", "--mcp", "demo", "--url", "https://example.com/other", "--target", "claude", "--no-provenance"})
	_, runErr := root.ExecuteC()
	os.Stdout, os.Stderr = realStdout, realStderr

	if runErr != nil || asked != 1 {
		t.Fatalf("err = %v, asked %d times; want no error and one question", runErr, asked)
	}
	if got := lockOnDisk(t); got != lock {
		t.Errorf("lock:\n%s\nwant it unchanged:\n%s", got, lock)
	}
	if got, err := os.ReadFile("apm.yml"); err != nil || string(got) != string(manifestBefore) {
		t.Errorf("apm.yml = %q (err %v), want it unchanged", got, err)
	}
}
