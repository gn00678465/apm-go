package main

import (
	"strings"
	"testing"
)

// Issue #87: the MCP block prints each target's adapter line and "  + <name>"
// as Oracle v0.33.0 prints them, with the targets in the order --target or
// targets: names them. The blocks below are what the Oracle printed for the
// same projects, with apm-go's status symbols.

const issue87Server = "dependencies:\n  mcp:\n  - name: two\n    registry: false\n    transport: stdio\n    command: npx\n    args: [\"-y\", \"foo\"]\n"

func issue87Project(t *testing.T, targets string) string {
	t.Helper()
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: ["+targets+"]\n"+issue87Server)
	return dir
}

func assertContainsBlock(t *testing.T, what, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s:\n%s\nwant it to contain:\n%s", what, got, want)
	}
}

func TestInstallMCP_AdapterLinesPerTarget(t *testing.T) {
	for _, tc := range []struct {
		target, title, lines string
	}{
		{"codex", "Codex", " + Configured MCP server 'two' for Codex CLI\n  + two\n"},
		{"copilot", "Copilot", "  + two\n"},
		{"opencode", "Opencode", "Successfully configured MCP server 'two' for OpenCode\n  + two\n"},
		{"antigravity", "Antigravity", " + Configured MCP server 'two' for antigravity\n  + two\n"},
	} {
		t.Run(tc.target, func(t *testing.T) {
			issue87Project(t, tc.target)
			got := mustRunCLI(t, "install")
			assertContainsBlock(t, "install", got,
				" i Targeting declared target from apm.yml: "+tc.target+"\n"+
					"|   >  two (self-defined, stdio)\n"+
					"|     +- Configuring for "+tc.title+"...\n"+
					tc.lines+
					"|   +  two -> "+tc.title+" (configured)\n")
		})
	}
}

func TestInstallMCP_TargetsInDeclaredOrder(t *testing.T) {
	issue87Project(t, "opencode, codex, claude, copilot")

	got := mustRunCLI(t, "install")

	assertContainsBlock(t, "install", got,
		" i Targeting declared targets from apm.yml: opencode, codex, claude, copilot\n"+
			"|   >  two (self-defined, stdio)\n"+
			"|     +- Configuring for Opencode, Codex, Claude, Copilot...\n"+
			"Successfully configured MCP server 'two' for OpenCode\n  + two\n"+
			" + Configured MCP server 'two' for Codex CLI\n  + two\n"+
			"Successfully configured MCP server 'two' for Claude Code\n  + two\n"+
			"  + two\n"+
			"|   +  two -> Opencode, Codex, Claude, Copilot (configured)\n")
}

// A target given twice keeps its first place, and agy is antigravity.
func TestInstallMCPFlag_TargetOrderDedupAndAlias(t *testing.T) {
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")

	got := mustRunCLI(t, "install", "--mcp", "two", "--target", "codex,agy,codex,claude", "--", "npx", "-y", "foo")

	assertContainsBlock(t, "install --mcp two", got,
		" i Targeting specific runtimes: codex, antigravity, claude\n"+
			"|   >  two (self-defined, stdio)\n"+
			"|     +- Configuring for Codex, Antigravity, Claude...\n"+
			" + Configured MCP server 'two' for Codex CLI\n  + two\n"+
			" + Configured MCP server 'two' for antigravity\n  + two\n"+
			"Successfully configured MCP server 'two' for Claude Code\n  + two\n"+
			"|   +  two -> Codex, Antigravity, Claude (configured)\n")
}

// An unchanged server prints no adapter line; a changed one prints the same
// lines under "Updating for".
func TestInstallMCP_AdapterLinesOnUpdateNotWhenUnchanged(t *testing.T) {
	dir := issue87Project(t, "codex, copilot")
	mustRunCLI(t, "install")

	got := mustRunCLI(t, "install")
	assertContainsBlock(t, "second install", got,
		" i Targeting declared targets from apm.yml: codex, copilot\n"+
			"|   + two (already configured)\n"+
			" + All servers up to date\n")
	if strings.Contains(got, "  + two\n") || strings.Contains(got, "Configured MCP server") {
		t.Errorf("second install printed an adapter line:\n%s", got)
	}

	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\ntargets: [codex, copilot]\n"+
		strings.Replace(issue87Server, `"foo"`, `"bar"`, 1))
	got = mustRunCLI(t, "install")
	assertContainsBlock(t, "install after a change", got,
		"|   >  two (self-defined, stdio)\n"+
			"|     +- Updating for Codex, Copilot...\n"+
			" + Configured MCP server 'two' for Codex CLI\n  + two\n"+
			"  + two\n"+
			"|   +  two -> Codex, Copilot (updated)\n"+
			" + Updated 1 server\n")
}
