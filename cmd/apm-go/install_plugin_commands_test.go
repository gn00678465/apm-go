package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The expected bytes below are what Oracle 0.33.0 wrote and printed for the
// same plugin and `install --target claude,copilot,opencode`
// (~/.cache/apm-go-verify/issue107/r1.txt, case nokey), the warnings with the
// status symbol in apm-go's shape.

const pluginCommandOracleCommand = "---\nargument-hint: <x>\ndescription: hello cmd\n---\n\nSay hi $ARGUMENTS"

const pluginCommandSource = "---\ndescription: hello cmd\nargument-hint: <x>\nname: hello\n---\nSay hi $ARGUMENTS\n"

const pluginCommandOracleLock = `  deployed_files:
  - .claude/commands/hello.md
  - .github/prompts/hello.prompt.md
  - .opencode/commands/hello.md
  deployed_file_hashes:
    .claude/commands/hello.md: sha256:cba0147abc7b7cfee55b63892904b3284c0b8f0db3db5594747f7f8b2dbeb529
    .github/prompts/hello.prompt.md: sha256:59af53e914c19d321d39bb2cc8a70db48ec5b2717332671c2bb3034fbc740035
    .opencode/commands/hello.md: sha256:cba0147abc7b7cfee55b63892904b3284c0b8f0db3db5594747f7f8b2dbeb529
`

// A Claude plugin whose plugin.json lists no commands deploys its commands/
// as prompts, as Oracle 0.33.0 does (issue #107).
func TestInstall_PluginDefaultCommandsArePrompts(t *testing.T) {
	root := bootstrapProject(t, "proj")
	proj := filepath.Join(root, "proj")
	writeCompileFile(t, proj, "apm.yml", "name: proj\nversion: 1.0.0\ndependencies:\n  apm:\n    - ./plug\n")
	writeCompileFile(t, proj, "plug/.claude-plugin/plugin.json", `{"name":"plug","version":"1.0.0"}`+"\n")
	writeCompileFile(t, proj, "plug/commands/hello.md", pluginCommandSource)

	stdout, stderr, code := runCompileCLI(t, "install", "--target", "claude,copilot,opencode")
	if code != 0 {
		t.Fatalf("exit %d, stdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}

	for rel, want := range map[string]string{
		".claude/commands/hello.md":       pluginCommandOracleCommand,
		".opencode/commands/hello.md":     pluginCommandOracleCommand,
		".github/prompts/hello.prompt.md": pluginCommandSource,
	} {
		got, err := os.ReadFile(filepath.Join(proj, rel))
		if err != nil || string(got) != want {
			t.Errorf("%s = %q (err %v), want %q", rel, got, err, want)
		}
	}
	lock, err := os.ReadFile(filepath.Join(proj, "apm.lock.yaml"))
	if err != nil || !strings.Contains(string(lock), pluginCommandOracleLock) {
		t.Errorf("apm.lock.yaml (err %v):\n%s\nwant it to hold:\n%s", err, lock, pluginCommandOracleLock)
	}
	for _, target := range []string{"Claude", "Opencode"} {
		want := "   ! [plug] " + target + " command hello: frontmatter keys not supported for " + strings.ToLower(target) +
			" commands and were dropped: name. Supported keys: allowed-tools, argument-hint, description, input, model.\n"
		if !strings.Contains(stdout, want) {
			t.Errorf("stdout:\n%s\nwant a line:\n%s", stdout, want)
		}
	}
}
