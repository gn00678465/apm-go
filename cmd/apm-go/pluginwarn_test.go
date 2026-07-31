// Tests for pluginwarn.go's detectPluginNativeRoot: R4's plugin-native
// root-directory warning, shared by both `apm-go init` and
// `apm-go plugin init`.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// createDirSymlinkOrJunction creates a directory symlink at link pointing to
// target, falling back to a Windows directory junction (`mklink /J`) when a
// real symlink cannot be created (no Developer Mode/
// SeCreateSymbolicLinkPrivilege) -- mirroring
// internal/marketplace/authoring/refcheck_test.go's own helper of the same
// name and rationale (a plain Windows account can create a junction with no
// special privilege, exercising the same os.Lstat/os.Stat codepaths a real
// symlink would). Returns ok=false only when both mechanisms fail.
func createDirSymlinkOrJunction(t *testing.T, target, link string) (ok bool, lastErr error) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return true, nil
	} else {
		lastErr = err
	}
	if runtime.GOOS != "windows" {
		return false, lastErr
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/J", link, target)
	if _, err := cmd.CombinedOutput(); err != nil {
		return false, err
	}
	return true, nil
}

// createFileSymlinkOrHardlink creates a file symlink at link pointing to
// target, falling back to a Windows hard link (`mklink /H`) when a real
// symlink cannot be created -- a hard link needs no special Windows
// privilege (unlike a symlink), and os.Stat/os.Lstat cannot tell a hard link
// apart from an ordinary file (there is no separate "is a hard link" mode
// bit), which is exactly the property this file's tests need: a hard link
// is a completely ordinary regular file from Stat/Lstat's point of view,
// standing in for "this environment's fallback when it cannot create a
// symlink, but the test's own assertion (IsRegular(), no ModeSymlink) is
// identical either way." Returns ok=false only when both mechanisms fail.
func createFileSymlinkOrHardlink(t *testing.T, target, link string) (ok bool, lastErr error) {
	t.Helper()
	if err := os.Symlink(target, link); err == nil {
		return true, nil
	} else {
		lastErr = err
	}
	if runtime.GOOS != "windows" {
		return false, lastErr
	}
	cmd := exec.Command("cmd", "/c", "mklink", "/H", link, target)
	if _, err := cmd.CombinedOutput(); err != nil {
		return false, err
	}
	return true, nil
}

func TestDetectPluginNativeRoot_NoSources_ReturnsNil(t *testing.T) {
	root := t.TempDir()
	if got := detectPluginNativeRoot(root); got != nil {
		t.Errorf("detectPluginNativeRoot(empty root) = %v, want nil", got)
	}
}

func TestDetectPluginNativeRoot_RealSkillsDir_Detected(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := detectPluginNativeRoot(root)
	if len(got) != 1 || got[0] != "skills/" {
		t.Errorf("detectPluginNativeRoot = %v, want [skills/]", got)
	}
}

func TestDetectPluginNativeRoot_RealApmDir_SuppressesWarning(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, ".apm"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := detectPluginNativeRoot(root); got != nil {
		t.Errorf("detectPluginNativeRoot = %v, want nil (a real .apm/ short-circuits the whole check)", got)
	}
}

func TestDetectPluginNativeRoot_SymlinkedSkillsDir_NotDetected(t *testing.T) {
	// AC16: a symlinked skills/ must NOT trigger the warning.
	root := t.TempDir()
	real := filepath.Join(t.TempDir(), "real-skills")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "skills")
	if ok, err := createDirSymlinkOrJunction(t, real, link); !ok {
		t.Skipf("SKIPPED: cannot create a directory symlink or junction in this environment (%v); AC16's symlinked-skills guard is untested by this run", err)
	}
	if got := detectPluginNativeRoot(root); got != nil {
		t.Errorf("detectPluginNativeRoot = %v, want nil (a symlinked skills/ must not trigger the warning, AC16)", got)
	}
}

// TestDetectPluginNativeRoot_SymlinkedApmDir_StillSuppressesWarning is
// A-BLOCKING-1's first named regression (external audit round 6, 2026-07-31):
// a project whose ".apm" is a symlink to a real directory elsewhere must
// still short-circuit the whole check, exactly as a real ".apm/" directory
// does -- os.Lstat().IsDir() (the pre-fix check) is false for ANY symlink
// regardless of what it resolves to, so this used to print the warning even
// though ".apm/" conceptually exists.
func TestDetectPluginNativeRoot_SymlinkedApmDir_StillSuppressesWarning(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	realApm := filepath.Join(t.TempDir(), "real-apm")
	if err := os.Mkdir(realApm, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, ".apm")
	if ok, err := createDirSymlinkOrJunction(t, realApm, link); !ok {
		t.Skipf("SKIPPED: cannot create a directory symlink or junction in this environment (%v); A-BLOCKING-1's symlinked-.apm guard is untested by this run", err)
	}
	if got := detectPluginNativeRoot(root); got != nil {
		t.Errorf("detectPluginNativeRoot = %v, want nil (a symlinked .apm/ must still suppress the warning, A-BLOCKING-1)", got)
	}
}

func TestDetectPluginNativeRoot_RealHooksJSON_Detected(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hooks.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := detectPluginNativeRoot(root)
	if len(got) != 1 || got[0] != "hooks.json" {
		t.Errorf("detectPluginNativeRoot = %v, want [hooks.json]", got)
	}
}

// TestDetectPluginNativeRoot_SymlinkedHooksJSON_Detected is A-BLOCKING-1's
// second named regression: a project whose "hooks.json" is a symlink to a
// real file must ALSO trigger the warning -- unlike the pluginNativeDirs
// directory list (which the PRD's own R4.2 wording and AC16 both require to
// exclude a symlink), hooks.json has no such carve-out; upstream's own
// is_file() check follows a symlink. The pre-fix code added an unrequested
// ModeSymlink exclusion here too, under-reporting this case.
func TestDetectPluginNativeRoot_SymlinkedHooksJSON_Detected(t *testing.T) {
	root := t.TempDir()
	realFile := filepath.Join(t.TempDir(), "real-hooks.json")
	if err := os.WriteFile(realFile, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "hooks.json")
	if ok, err := createFileSymlinkOrHardlink(t, realFile, link); !ok {
		t.Skipf("SKIPPED: cannot create a file symlink or hard link in this environment (%v); A-BLOCKING-1's symlinked-hooks.json guard is untested by this run", err)
	}
	got := detectPluginNativeRoot(root)
	if len(got) != 1 || got[0] != "hooks.json" {
		t.Errorf("detectPluginNativeRoot = %v, want [hooks.json] (a symlinked hooks.json must still trigger the warning, A-BLOCKING-1)", got)
	}
}

// TestPluginInitCmd_HooksJSONOnly_StillWarns is A-BLOCKING-1's e2e backstop
// (external audit, plugin-init, 2026-07-31): every test above calls
// detectPluginNativeRoot directly, so none of them would notice a regression
// introduced one layer up, at its sole call site (runInitCore, init.go:171),
// e.g.
//
//	sources := detectPluginNativeRoot(originalCwd)
//	if mode.plugin && len(sources) == 1 && sources[0] == "hooks.json" { sources = nil }
//
// Read init.go:171 -- the real call site has no such mode.plugin branch
// today (confirmed by inspection: `if sources := detectPluginNativeRoot(originalCwd); len(sources) > 0`
// runs unconditionally for both modes), but nothing short of an end-to-end
// run through `apm-go plugin init` itself would catch that branch being
// reintroduced, because the mutation lives outside detectPluginNativeRoot's
// own body. This test drives pluginInitCmd() end-to-end against a directory
// whose ONLY plugin-native source is hooks.json (no pluginNativeDirs
// present, matching the mutation's `len(sources) == 1 && sources[0] ==
// "hooks.json"` guard exactly) and asserts the warning still reaches stderr.
func TestPluginInitCmd_HooksJSONOnly_StillWarns(t *testing.T) {
	dir := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { os.Chdir(origDir) })

	if err := os.WriteFile("hooks.json", []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := captureStderr(t, func() {
		cmd := pluginInitCmd()
		cmd.SetArgs([]string{"my-plugin", "--yes", "--target", "claude"})
		if err := cmd.Execute(); err != nil {
			t.Fatalf("plugin init returned an error: %v", err)
		}
	})

	if !strings.Contains(out, "plugin-native") {
		t.Errorf("`apm-go plugin init` with only hooks.json present (no other pluginNativeDirs, no .apm/) printed no plugin-native warning; output:\n%s", out)
	}
}

func TestDetectPluginNativeRoot_AllSixDirsAndHooksJSON_EachDetected(t *testing.T) {
	// AC39: every one of pluginNativeDirs, plus hooks.json, triggers the
	// warning individually -- not just skills/.
	for _, d := range pluginNativeDirs {
		t.Run(d, func(t *testing.T) {
			root := t.TempDir()
			if err := os.Mkdir(filepath.Join(root, d), 0o755); err != nil {
				t.Fatal(err)
			}
			got := detectPluginNativeRoot(root)
			if len(got) != 1 || got[0] != d+"/" {
				t.Errorf("detectPluginNativeRoot = %v, want [%s/]", got, d)
			}
		})
	}
}
