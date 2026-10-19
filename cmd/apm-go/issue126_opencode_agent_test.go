package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Issue #126 item 81: an agent written to .opencode/agents/ whose frontmatter
// OpenCode refuses gets the Oracle's warning, only when the file is written.
// Every wanted transcript is what Oracle 0.33.0 printed for the same project
// (~/.cache/apm-go-verify/issue126/impl/F-t1, F-cmp2/s10), unwrapped,
// with apm-go's status symbols, without the two experimental-resolver lines
// (R10) and, in uninstall, without the Oracle's "Cleaned 1 stale file" line
// (issue #88 batch ruling, item c42-1).

const issue126ColorGreen = "OpenCode requires a hex value (e.g. '#aabbcc') or one of ['accent', 'error', 'info', 'primary', 'secondary', 'success', 'warning']. OpenCode will reject this agent at load time. Fix: replace the color with a '#rgb' or '#rrggbb' hex literal or one of the listed theme names.\n"

// impl/F-t1 (agent/s7 with opencode alone): a user file different from the source is left, one equal to it
// adopted, and neither is warned about.
func TestInstall_Issue126_OpencodeAgentWarningOnlyWhenWritten(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	// Windows takes the home directory from USERPROFILE.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "dep/apm.yml", "name: depname\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "dep/.apm/agents/a1.agent.md", "---\ndescription: a1\ntools: Read, Grep\ncolor: green\n---\nA1\n")
	writeCompileFile(t, dir, "dep/.apm/agents/a3.agent.md", "---\ndescription: a3\ncolor: blue\n---\nA3\n")
	writeCompileFile(t, dir, "dep/.apm/agents/a4.agent.md", "---\ndescription: a4\ncolor: red\n---\nA4\n")
	writeCompileFile(t, dir, ".opencode/agents/a1.md", "user file\n")
	writeCompileFile(t, dir, ".opencode/agents/a3.md", "---\ndescription: a3\ncolor: blue\n---\nA3\n")
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./dep\n")

	want := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./dep...\n" +
		" i Targets: opencode  (source: --target flag)\n" +
		"   + ./dep (local)\n" +
		"  |-- 1 agents integrated (1 adopted) -> .opencode/agents/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		"   ! 1 file skipped -- local files exist, not managed by APM\n" +
		"   ! [depname] OpenCode agent 'depname/a4.agent.md' has color='red'; " + issue126ColorGreen +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install", "--target", "opencode"); got != want {
		t.Errorf("install:\n%s\nwant:\n%s", got, want)
	}

	wantRepeat := " > Installing dependencies from apm.yml...\n" +
		" i Targets: opencode  (source: --target flag)\n" +
		"   + ./dep (local)\n" +
		"  |-- 2 agents adopted -> .opencode/agents/\n" +
		"  |-- (files unchanged)\n" +
		"   ! 1 file skipped -- local files exist, not managed by APM\n" +
		" + Installed 1 APM dependency in <DUR>.\n"
	if got := mustRunCLI(t, "install", "--target", "opencode"); got != wantRepeat {
		t.Errorf("repeat install:\n%s\nwant:\n%s", got, wantRepeat)
	}
}

// agent/s10, impl/F-cmp2/s10: b's x.agent.md lost to a's on install; uninstall ./a hands it
// over and writes it, so its warning comes after the block.
func TestUninstall_Issue126_OpencodeAgentWarningOnHandover(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	// Windows takes the home directory from USERPROFILE.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := chdirTemp(t)
	for _, n := range []string{"a", "b"} {
		writeCompileFile(t, dir, n+"/apm.yml", "name: "+n+"\nversion: 1.0.0\n")
	}
	writeCompileFile(t, dir, "a/.apm/agents/x.agent.md", "---\ndescription: xa\n---\nXA\n")
	writeCompileFile(t, dir, "b/.apm/agents/x.agent.md", "---\ndescription: xb\ncolor: green\n---\nXB\n")
	writeCompileFile(t, dir, "b/.apm/agents/y.agent.md", "---\ndescription: yb\ncolor: green\n---\nYB\n")
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [opencode]\ndependencies:\n  apm:\n    - ./a\n    - ./b\n")

	want := " > Installing dependencies from apm.yml...\n" +
		" > Resolving ./a...\n" +
		" > Resolving ./b...\n" +
		" i Targets: opencode  (source: apm.yml)\n" +
		"   + ./a (local)\n" +
		"   + ./b (local)\n" +
		"  |-- 1 agents integrated -> .opencode/agents/\n" +
		"  |-- 1 agents integrated -> .opencode/agents/\n" +
		" i Added apm_modules/ to .gitignore\n" +
		"   ! 1 file skipped -- local files exist, not managed by APM\n" +
		"   ! [b] OpenCode agent 'b/y.agent.md' has color='green'; " + issue126ColorGreen +
		" + Installed 2 APM dependencies in <DUR>.\n"
	if got := mustRunCLI(t, "install"); got != want {
		t.Errorf("install:\n%s\nwant:\n%s", got, want)
	}

	// A frozen install restores y.md with no warning of its own; the shadowed
	// line is main's.
	if err := os.Remove(filepath.Join(dir, ".opencode/agents/y.md")); err != nil {
		t.Fatal(err)
	}
	wantFrozen := " ! agents \"x\" from dependency:_local/b shadowed by dependency:_local/a (first-declared wins)\n" +
		" + Frozen install: all dependencies pinned and verified\n"
	if got := mustRunCLI(t, "install", "--frozen"); got != wantFrozen {
		t.Errorf("frozen install:\n%s", got)
	}

	manifestPath, err := filepath.Abs("apm.yml")
	if err != nil {
		t.Fatal(err)
	}
	wantUninstall := " > Uninstalling 1 package(s)...\n" +
		" + _local/a - found in apm.yml\n" +
		" i Removed _local/a from apm_modules/\n" +
		" i Removed _local/a from dependencies.apm in apm.yml\n" +
		" + Updated " + manifestPath + " (removed 1 package(s))\n" +
		"  |-- 1 agents integrated (1 adopted) -> .opencode/agents/\n" +
		"   ! [b] OpenCode agent 'b/x.agent.md' has color='green'; " + issue126ColorGreen +
		" + Uninstall complete: Removed 1 package(s) from apm.yml, Removed 1 package(s) from apm_modules/\n"
	if got := mustRunCLI(t, "uninstall", "./a"); got != wantUninstall {
		t.Errorf("uninstall:\n%s\nwant:\n%s", got, wantUninstall)
	}
}

// update deploys again and warns about the agent it writes; an agent already
// on disk with the source's bytes is not written, so it gets none. The
// Oracle's update of a local package deploys nothing, so the text alone is
// the Oracle's (impl/F-cmp2/u1).
func TestUpdate_Issue126_OpencodeAgentWarning(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	home := t.TempDir()
	// Windows takes the home directory from USERPROFILE.
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "dep/apm.yml", "name: depname\nversion: 1.0.0\n")
	writeCompileFile(t, dir, "dep/.apm/agents/a1.agent.md", "---\ndescription: a1\ncolor: green\n---\nA1\n")
	writeCompileFile(t, dir, "dep/.apm/agents/a2.agent.md", "---\ndescription: a2\ntools: Read\n---\nA2\n")
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [opencode]\ndependencies:\n  apm:\n    - ./dep\n")
	mustRunCLI(t, "install")
	writeCompileFile(t, dir, "dep/.apm/agents/a1.agent.md", "---\ndescription: a1 v2\ncolor: green\n---\nA1\n")

	got := mustRunCLI(t, "update")
	if want := " ! [depname] OpenCode agent 'depname/a1.agent.md' has color='green'; " + issue126ColorGreen; !strings.Contains(got, want) {
		t.Errorf("update:\n%s\nwant the line:\n%s", got, want)
	}
	if strings.Contains(got, "a2.agent.md") {
		t.Errorf("update warned about a2, which it did not write:\n%s", got)
	}
}
