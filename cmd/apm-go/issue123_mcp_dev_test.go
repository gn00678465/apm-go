package main

import (
	"os"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// TestInstallMCPDev_WritesDevDependencies follows Oracle 0.33.0 (issue #123
// runs c57_1 and c57_1b; install/mcp/writer.py:44-140): --dev declares the
// server under devDependencies.mcp, looks for a same-named entry in that
// section alone, and a same name under dependencies.mcp is no conflict.
func TestInstallMCPDev_WritesDevDependencies(t *testing.T) {
	t.Chdir(t.TempDir())
	setTestHome(t, t.TempDir())
	const base = "name: p\nversion: 1.0.0\ntargets: [claude]\ndependencies:\n  mcp:\n  - name: devsrv\n    registry: false\n    transport: stdio\n    command: npx\n"
	if err := os.WriteFile("apm.yml", []byte(base), 0o644); err != nil {
		t.Fatal(err)
	}

	mustRun(t, "install", "--mcp", "devsrv", "--dev", "--", "npx", "-y", "foo")

	data := readText(t, "apm.yml")
	if !strings.HasPrefix(data, base) {
		t.Fatalf("dependencies.mcp changed:\n%s", data)
	}
	node, err := yamlcore.SafeLoad([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := manifest.ParseManifest(node)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.MCPServers) != 1 || m.MCPServers[0].Args != nil {
		t.Fatalf("dependencies.mcp = %+v, want the one entry with no args", m.MCPServers)
	}
	if len(m.MCPDevServers) != 1 || m.MCPDevServers[0].Name != "devsrv" ||
		m.MCPDevServers[0].Args == nil || strings.Join(*m.MCPDevServers[0].Args, " ") != "-y foo" {
		t.Fatalf("devDependencies.mcp = %+v, want devsrv with args -y foo", m.MCPDevServers)
	}

	// Oracle c57_1 step 3: another value for the dev entry without --force
	// is refused and changes nothing.
	if _, _, code := runCompileCLI(t, "install", "--mcp", "devsrv", "--dev", "--", "npx", "-y", "bar"); code == 0 {
		t.Fatal("conflicting --dev entry without --force exited 0")
	}
	if got := readText(t, "apm.yml"); got != data {
		t.Fatalf("apm.yml changed on a refused --dev install:\n%s", got)
	}
}
