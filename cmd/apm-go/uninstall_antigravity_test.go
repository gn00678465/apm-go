package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/lockfile"
)

// TestRunUninstall_AntigravityBundleRemovedSiblingBundleSurvives locks the
// antigravity plugin bundle lifecycle (task 07-11-antigravity-plugins-bundle,
// PRD AC3): uninstall is fully generic over deployed_files/deployed_file_hashes
// (deploy.RemoveDeployedFiles + cleanupEmptyParents), so no bundle-specific
// code is needed for a bundled dependency's entire .agents/plugins/<pkg>/
// directory to disappear on uninstall, while a sibling dependency's bundle --
// and the shared .agents/plugins/ root it still occupies -- survive untouched.
func TestRunUninstall_AntigravityBundleRemovedSiblingBundleSurvives(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/dep-a\n    - acme/dep-b\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"acme/dep-a", "acme/dep-b"} {
		if err := os.MkdirAll(filepath.Join(dir, "apm_modules", filepath.FromSlash(key)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	aManifestHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-a/plugin.json", "{\"name\": \"dep-a\"}\n")
	aHooksHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-a/hooks.json", `{"a-hook":{"Stop":[]}}`)
	aAgentHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-a/agents/depagent/agent.md", "dep-a agent")

	const bHooksContent = `{"b-hook":{"Stop":[]}}`
	bManifestHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-b/plugin.json", "{\"name\": \"dep-b\"}\n")
	bHooksHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-b/hooks.json", bHooksContent)

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{
				RepoURL: "acme/dep-a", Source: "git",
				DeployedFiles: []string{
					".agents/plugins/dep-a/plugin.json",
					".agents/plugins/dep-a/hooks.json",
					".agents/plugins/dep-a/agents/depagent/agent.md",
				},
				DeployedHashes: map[string]string{
					".agents/plugins/dep-a/plugin.json":              aManifestHash,
					".agents/plugins/dep-a/hooks.json":               aHooksHash,
					".agents/plugins/dep-a/agents/depagent/agent.md": aAgentHash,
				},
			},
			{
				RepoURL: "acme/dep-b", Source: "git",
				DeployedFiles: []string{".agents/plugins/dep-b/plugin.json", ".agents/plugins/dep-b/hooks.json"},
				DeployedHashes: map[string]string{
					".agents/plugins/dep-b/plugin.json": bManifestHash,
					".agents/plugins/dep-b/hooks.json":  bHooksHash,
				},
			},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/dep-a"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins", "dep-a")); !os.IsNotExist(err) {
		t.Errorf("expected dep-a's entire bundle directory to be pruned, stat err=%v", err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, ".agents", "plugins", "dep-b", "hooks.json")); err != nil || string(got) != bHooksContent {
		t.Errorf("expected sibling dep-b bundle to survive untouched, got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins")); err != nil {
		t.Errorf("expected shared .agents/plugins/ root to survive (dep-b still occupies it), stat err=%v", err)
	}
}

// TestRunUninstall_AntigravityBundleUserFileSurvives locks PRD AC3's "only
// deletes what it deployed" invariant for a bundle directory: a file the
// user hand-placed inside the bundle (never in DeployedFiles) survives
// byte-identical, and its presence keeps the bundle directory itself from
// being pruned even though every recorded file was removed.
func TestRunUninstall_AntigravityBundleUserFileSurvives(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/dep-b\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	manifestHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-b/plugin.json", "{\"name\": \"dep-b\"}\n")
	hooksHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-b/hooks.json", `{"b-hook":{"Stop":[]}}`)

	const userNotes = "keep me"
	userNotesPath := filepath.Join(dir, ".agents", "plugins", "dep-b", "USER-NOTES.md")
	if err := os.WriteFile(userNotesPath, []byte(userNotes), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{
				RepoURL: "acme/dep-b", Source: "git",
				DeployedFiles: []string{".agents/plugins/dep-b/plugin.json", ".agents/plugins/dep-b/hooks.json"},
				DeployedHashes: map[string]string{
					".agents/plugins/dep-b/plugin.json": manifestHash,
					".agents/plugins/dep-b/hooks.json":  hooksHash,
				},
			},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	if err := runUninstall([]string{"acme/dep-b"}, uninstallOptions{}); err != nil {
		t.Fatalf("runUninstall: %v", err)
	}

	if got, err := os.ReadFile(userNotesPath); err != nil || string(got) != userNotes {
		t.Errorf("expected user-authored bundle file to survive untouched, got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins", "dep-b")); err != nil {
		t.Errorf("expected non-empty bundle directory (user file remains) to survive, stat err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins", "dep-b", "plugin.json")); !os.IsNotExist(err) {
		t.Errorf("expected the recorded plugin.json to be removed, stat err=%v", err)
	}
}

// TestRunUninstall_AntigravityTamperedManifestKeptWithWarning locks un-053's
// non-negotiable safety line for plugin.json specifically: a hand-edited
// manifest whose bytes no longer match the recorded hash is kept (not force-
// deleted) with a stderr warning, leaving a non-empty "known limitation"
// bundle directory behind (design.md R7) -- while a sibling file that still
// matches its recorded hash is removed normally.
func TestRunUninstall_AntigravityTamperedManifestKeptWithWarning(t *testing.T) {
	dir := chdirTemp(t)

	manifestYAML := "name: test\nversion: \"1.0.0\"\ndependencies:\n  apm:\n    - acme/dep-b\n"
	if err := os.WriteFile("apm.yml", []byte(manifestYAML), 0644); err != nil {
		t.Fatal(err)
	}

	origHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-b/plugin.json", "{\"name\": \"dep-b\"}\n")
	hooksHash := writeUninstallDeployedFile(t, dir, ".agents/plugins/dep-b/hooks.json", `{"b-hook":{"Stop":[]}}`)

	manifestPath := filepath.Join(dir, ".agents", "plugins", "dep-b", "plugin.json")
	const tampered = `{"name":"user-edited","note":"keep"}`
	if err := os.WriteFile(manifestPath, []byte(tampered), 0o644); err != nil {
		t.Fatal(err)
	}

	lock := &lockfile.Lockfile{
		Dependencies: []lockfile.LockedDep{
			{
				RepoURL: "acme/dep-b", Source: "git",
				DeployedFiles: []string{".agents/plugins/dep-b/plugin.json", ".agents/plugins/dep-b/hooks.json"},
				DeployedHashes: map[string]string{
					".agents/plugins/dep-b/plugin.json": origHash,
					".agents/plugins/dep-b/hooks.json":  hooksHash,
				},
			},
		},
	}
	writeUninstallLockfileFixture(t, lock)

	stderr := captureUninstallStderr(t, func() {
		if err := runUninstall([]string{"acme/dep-b"}, uninstallOptions{}); err != nil {
			t.Fatalf("runUninstall: %v", err)
		}
	})
	if !strings.Contains(stderr, "modified since deploy (hash mismatch)") {
		t.Errorf(`expected a stderr warning containing "modified since deploy (hash mismatch)", got:\n%s`, stderr)
	}

	got, err := os.ReadFile(manifestPath)
	if err != nil || string(got) != tampered {
		t.Errorf("expected the hand-edited manifest to survive byte-identical, got=%q err=%v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".agents", "plugins", "dep-b", "hooks.json")); !os.IsNotExist(err) {
		t.Errorf("expected the correctly-hashed hooks.json to still be removed, stat err=%v", err)
	}
}
