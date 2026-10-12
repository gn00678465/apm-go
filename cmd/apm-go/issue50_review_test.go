package main

import (
	"os"
	"strings"
	"testing"
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
