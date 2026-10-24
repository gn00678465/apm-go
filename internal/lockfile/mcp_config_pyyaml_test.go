package lockfile

import (
	"fmt"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

// The mcp_configs of these locks are what Oracle 0.33.0 wrote for an apm.yml
// whose enabled values are the strings "1e3", "1:30", "no", "0x1F", "1_000",
// "2024-01-01", "~" and ".5" (C/e24), and the plain 1e3, 1:30, 0o17, 017,
// 1.5e3, 0b11 and 1.50 (C/e24p, C/e24/f): PyYAML quotes a string its YAML 1.1
// resolver reads as another type, and leaves 1e3 plain. apm-go reads them
// back as the values apm.yml declares and writes them byte for byte. The
// floats are what PyYAML 6.0.3 of Oracle 0.33.0 dumps for them after
// safe_load (C/sci/pyyaml_ref.txt): an exponent with no decimal point gets
// ".0" before the "e", or its resolver would read 1e+20 back as a string.
func TestMCPConfigs_PyYAMLQuoting(t *testing.T) {
	type server struct{ declared, locked string }
	servers := []server{
		{`"1e3"`, "1e3"}, {`"1:30"`, "'1:30'"}, {`"no"`, "'no'"}, {`"0x1F"`, "'0x1F'"},
		{`"1_000"`, "'1_000'"}, {`"2024-01-01"`, "'2024-01-01'"}, {`"~"`, "'~'"}, {`".5"`, "'.5'"},
		{"1e3", "1e3"}, {"1:30", "90"}, {"0o17", "0o17"}, {"017", "15"}, {"1.5e3", "1.5e3"},
		{"0b11", "3"}, {"1.50", "1.5"}, {"off", "false"},
	}
	floatFrom := len(servers)
	servers = append(servers, []server{
		{"1.0e+20", "1.0e+20"}, {"1.0e-10", "1.0e-10"}, {"1.0e+16", "1.0e+16"}, {"1.2e+20", "1.2e+20"},
		{"-1.0e+20", "-1.0e+20"}, {"1.0E+20", "1.0e+20"}, {"1.0e+15", "1000000000000000.0"},
		{"+.Inf", ".inf"}, {"1.0e+400", ".inf"}, {"-.inf", "-.inf"}, {".NaN", ".nan"},
	}...)
	name := func(i int) string { return fmt.Sprintf("s%02d", i) }
	var lock strings.Builder
	lock.WriteString("lockfile_version: '1'\ndependencies: []\nmcp_servers:\n")
	for i := range servers {
		lock.WriteString("- " + name(i) + "\n")
	}
	lock.WriteString("mcp_configs:\n")
	for i, s := range servers {
		lock.WriteString("  " + name(i) + ":\n    name: " + name(i) + "\n    enabled: " + s.locked + "\n")
	}
	node, err := yamlcore.SafeLoad([]byte(lock.String()))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(node)
	if err != nil {
		t.Fatal(err)
	}
	for i, s := range servers {
		var decl yaml.Node
		if err := yaml.Unmarshal([]byte("name: "+name(i)+"\nenabled: "+s.declared+"\n"), &decl); err != nil {
			t.Fatal(err)
		}
		want := manifest.PyYAML11Value(decl.Content[0])
		if !MCPConfigEqual(lf.MCPConfigs[name(i)], want) {
			t.Errorf("%s: the locked enabled %s is not the declared %s", name(i), s.locked, s.declared)
		}
		if i >= floatFrom {
			if tag := lf.MCPConfigs[name(i)].Content[3].ShortTag(); tag != "!!float" {
				t.Errorf("%s: the locked enabled %s reads back as %s, want !!float", name(i), s.locked, tag)
			}
		}
	}
	out, err := WriteLockfile(lf, node)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if i := strings.Index(got, "mcp_configs:\n"); i < 0 || !strings.HasPrefix(lock.String()[strings.Index(lock.String(), "mcp_configs:\n"):], got[i:]) {
		t.Errorf("written lock:\n%s\nwant the mcp_configs of:\n%s", got, lock.String())
	}
}
