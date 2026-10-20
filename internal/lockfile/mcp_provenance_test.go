package lockfile

import (
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// TestMCPConfigProvenance_RoundTrip: a value is one owner or a list of
// them (deps/lockfile.py:138-149), written sorted by server with a list's
// owners sorted (deps/lockfile.py:160-166,881-883).
func TestMCPConfigProvenance_RoundTrip(t *testing.T) {
	in := "lockfile_version: '1'\ndependencies: []\nmcp_config_provenance:\n  zz: pkg\n  aa:\n  - b\n  - a\n"
	node, err := yamlcore.SafeLoad([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	if got := lf.MCPConfigProvenance["zz"]; got.List || len(got.Names) != 1 || got.Names[0] != "pkg" {
		t.Fatalf("zz = %+v", got)
	}
	out, err := WriteLockfile(lf, node)
	if err != nil {
		t.Fatal(err)
	}
	const want = "lockfile_version: '1'\ndependencies: []\ndeployments: []\nmcp_config_provenance:\n  aa:\n  - a\n  - b\n  zz: pkg\n"
	if string(out) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", out, want)
	}
	other := *lf
	other.MCPConfigProvenance = map[string]MCPOwners{"zz": {Names: []string{"pkg"}}}
	if IsSemanticEqual(lf, &other) {
		t.Error("a lock without aa compares equal")
	}
}
