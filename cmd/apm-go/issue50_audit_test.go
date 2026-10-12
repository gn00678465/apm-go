package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// localSkillProject deploys three files (one rule, SKILL.md, extra.txt) and
// the lock also lists the skill's directory; both audits count the files.
func TestAuditContent_SkillDirectoryIsNotCountedAsFile(t *testing.T) {
	localSkillProject(t)
	mustRunCLI(t, "install")

	if out := mustRunCLI(t, "audit"); !strings.Contains(out, "3 deployed files verified") {
		t.Errorf("audit stdout = %q, want it to report 3 deployed files", out)
	}
	const want = " + audit --content: 3 file(s) scanned, no hidden characters\n"
	if out := mustRunCLI(t, "audit", "--content"); out != want {
		t.Errorf("audit --content stdout = %q, want %q", out, want)
	}
}

// apm-go refuses an existing .mcp.json it cannot parse and keeps its bytes;
// the line ends with encoding/json's own error for the file, as it did
// before issue #50. (The Oracle rewrites the file; ticket 32 item 26.)
func TestInstallMCP_MalformedMCPJSON_RefusedAndKept(t *testing.T) {
	const content = "{not json"
	dir := chdirTemp(t)
	writeCompileFile(t, dir, "apm.yml", "name: proj\nversion: 1.0.0\n")
	writeCompileFile(t, dir, ".mcp.json", content)

	stdout, stderr, exitCode := runCompileCLI(t, "install", "--mcp", "one", "--target", "claude", "--", "npx")

	var v any
	want := " x deploy to claude: existing .mcp.json is not valid, refusing to overwrite: " +
		json.Unmarshal([]byte(content), &v).Error() + "\n"
	if stdout != want || stderr != "" || exitCode != 1 {
		t.Errorf("stdout = %q, stderr = %q, exit = %d; want stdout %q, empty stderr, exit 1", stdout, stderr, exitCode, want)
	}
	got, err := os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil || string(got) != content {
		t.Errorf(".mcp.json = %q, %v; want it unchanged", got, err)
	}
}
