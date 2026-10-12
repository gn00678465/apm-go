package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
	"go.yaml.in/yaml/v4"
)

// oracleDevMCPLock is apm.lock.yaml as the Oracle v0.33.0 wrote it for the
// install-local-repeat fixture with ./dep under devDependencies and one
// self-defined MCP server declared by dep/apm.yml, after the line
// "x-entry: kept" was added to the entry and the lock went through the
// Oracle's LockFile.from_yaml(...).to_yaml(). is_dev, x-entry and
// mcp_config_provenance are values apm-go does not model.
const oracleDevMCPLock = `lockfile_version: '1'
apm_version: 0.33.0
dependencies:
- repo_url: _local/dep
  name: local-dep
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .claude/rules/example.md
  deployed_file_hashes:
    .claude/rules/example.md: sha256:2bce21782221d26c7efac5eed312f71e70fb6d0460082c29e9a5c2f8c8afd0ac
  source: local
  local_path: ./dep
  is_dev: true
  x-entry: kept
deployments:
- kind: project-relative
  target: claude
  value: .claude/rules/example.md
  runtime: null
  scope: project
  owners:
  - ./dep
  active_owner: ./dep
  content_hash: sha256:2bce21782221d26c7efac5eed312f71e70fb6d0460082c29e9a5c2f8c8afd0ac
- kind: project-relative
  target: claude
  value: .claude/rules/mine.md
  runtime: null
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: sha256:afbf75091351177625dae27f1d1b1d075f45dbc7ca80572a043105ccbeceab4c
- kind: uri
  target: mcp
  value: two
  runtime: claude
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
mcp_servers:
- two
mcp_configs:
  two:
    name: two
    transport: stdio
    args:
    - -y
    - foo
    registry: false
    command: npx
mcp_target_servers:
  claude:
  - two
mcp_config_provenance:
  two: local-dep
local_deployed_files:
- .claude/rules/mine.md
local_deployed_file_hashes:
  .claude/rules/mine.md: sha256:afbf75091351177625dae27f1d1b1d075f45dbc7ca80572a043105ccbeceab4c
`

// legacyUserAndURILock has no deployments section; oracleUserAndURILock is
// what the Oracle v0.33.0 writes for it (LockFile.from_yaml(...).to_yaml()).
const legacyUserAndURILock = `lockfile_version: '1'
apm_version: 0.33.0
dependencies:
- repo_url: _local/dep
  name: local-dep
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .copilot/skills/review/SKILL.md
  - cowork://skills/review/SKILL.md
  deployed_file_hashes:
    .copilot/skills/review/SKILL.md: sha256:2bce21782221d26c7efac5eed312f71e70fb6d0460082c29e9a5c2f8c8afd0ac
  source: local
  local_path: ./dep
local_deployed_files:
- copilot-app-db://workflows/mine
`

const oracleUserAndURILock = `lockfile_version: '1'
apm_version: 0.33.0
dependencies:
- repo_url: _local/dep
  name: local-dep
  version: 1.0.0
  package_type: apm_package
  deployed_files:
  - .copilot/skills/review/SKILL.md
  - cowork://skills/review/SKILL.md
  deployed_file_hashes:
    .copilot/skills/review/SKILL.md: sha256:2bce21782221d26c7efac5eed312f71e70fb6d0460082c29e9a5c2f8c8afd0ac
  source: local
  local_path: ./dep
deployments:
- kind: uri
  target: copilot-app
  value: copilot-app-db://workflows/mine
  runtime: null
  scope: project
  owners:
  - .
  active_owner: .
  content_hash: null
- kind: project-relative
  target: copilot
  value: .copilot/skills/review/SKILL.md
  runtime: null
  scope: user
  owners:
  - ./dep
  active_owner: ./dep
  content_hash: sha256:2bce21782221d26c7efac5eed312f71e70fb6d0460082c29e9a5c2f8c8afd0ac
- kind: uri
  target: cowork
  value: cowork://skills/review/SKILL.md
  runtime: null
  scope: project
  owners:
  - ./dep
  active_owner: ./dep
  content_hash: null
local_deployed_files:
- copilot-app-db://workflows/mine
`

func parseLockText(t *testing.T, text string) (*Lockfile, *yaml.Node) {
	t.Helper()
	doc, err := yamlcore.SafeLoad([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	lf, err := ParseLockfile(doc)
	if err != nil {
		t.Fatal(err)
	}
	return lf, doc
}

func rewriteLockText(t *testing.T, text string) string {
	t.Helper()
	lf, doc := parseLockText(t, text)
	got, err := WriteLockfile(lf, doc)
	if err != nil {
		t.Fatal(err)
	}
	return string(got)
}

func oracleFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "oracle", name+".lock.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteLockfile_KeepsOracleValuesItDoesNotModel(t *testing.T) {
	if got := rewriteLockText(t, oracleDevMCPLock); got != oracleDevMCPLock {
		t.Errorf("lock differs from the Oracle's\n--- got\n%s\n--- want\n%s", got, oracleDevMCPLock)
	}
}

// The Oracle drops a top-level key it does not know; apm-go keeps it, after
// every key of the lock format (PRODUCT.md, Lockfile).
func TestWriteLockfile_KeepsUnknownTopLevelKeyLast(t *testing.T) {
	want := oracleDevMCPLock + "x-note: kept\n"
	if got := rewriteLockText(t, want); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestWriteLockfile_DerivesUserScopeAndURIRowsAsTheOracle(t *testing.T) {
	if got := rewriteLockText(t, legacyUserAndURILock); got != oracleUserAndURILock {
		t.Errorf("lock differs from the Oracle's\n--- got\n%s\n--- want\n%s", got, oracleUserAndURILock)
	}
}

func TestParseLockfile_IgnoresDeploymentRowThatIsNotAMapping(t *testing.T) {
	text := legacyUserAndURILock + "deployments:\n- stray\n"
	if got := rewriteLockText(t, text); got != oracleUserAndURILock {
		t.Errorf("got\n%s\nwant\n%s", got, oracleUserAndURILock)
	}
}

func TestWriteLockfile_OriginalThatIsNotAMappingAddsNothing(t *testing.T) {
	want := oracleFixture(t, "mcp-stdio")
	lf, _ := parseLockText(t, want)
	original, err := yamlcore.SafeLoad([]byte("- x-note\n- kept\n"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := WriteLockfile(lf, original)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestIsSemanticEqual_MCPState(t *testing.T) {
	base := oracleFixture(t, "mcp-stdio")
	tests := []struct {
		name     string
		old, new string
		want     bool
	}{
		{"same lock", "", "", true},
		{"server added to a second target", "  claude:\n  - two\n", "  claude:\n  - two\n  codex:\n  - two\n", false},
		{"server moved to another target", "  claude:\n  - two\n", "  codex:\n  - two\n", false},
		{"another server on the same target", "  claude:\n  - two\n", "  claude:\n  - three\n", false},
		{"config added", "mcp_target_servers:", "  three:\n    name: three\nmcp_target_servers:", false},
		{"config value changed", "    command: npx\n", "    command: uvx\n", false},
		{"argument added", "    - foo\n", "    - foo\n    - bar\n", false},
		{"arguments became a string", "    args:\n    - -y\n    - foo\n", "    args: -y foo\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			changed := strings.Replace(base, tt.old, tt.new, 1)
			if tt.old != "" && changed == base {
				t.Fatalf("fixture has no %q", tt.old)
			}
			a, _ := parseLockText(t, base)
			b, _ := parseLockText(t, changed)
			if got := IsSemanticEqual(a, b); got != tt.want {
				t.Errorf("IsSemanticEqual = %v, want %v", got, tt.want)
			}
			if got := IsSemanticEqual(b, a); got != tt.want {
				t.Errorf("IsSemanticEqual reversed = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNodesEqual_AbsentValue(t *testing.T) {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: "npx", Tag: "!!str"}
	if NodesEqual(nil, node) || NodesEqual(node, nil) {
		t.Error("an absent value equals a present one")
	}
	if !NodesEqual(nil, nil) {
		t.Error("two absent values differ")
	}
}
