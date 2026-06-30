package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"go.yaml.in/yaml/v4"
)

// oracleExpected mirrors the relevant fields of
// conformance/conformance-kit/oracle/targets/expected/<target>.yaml.
type oracleExpected struct {
	Target        string   `yaml:"target"`
	DeployedFiles []string `yaml:"deployed_files"`
	NotDeployed   []string `yaml:"not_deployed"`
}

// oracleRoot is the repo-relative path to the generated oracle.
// The oracle is gitignored (produced by tools/gen_oracle.py), so tests
// that consume it must skip gracefully when it is absent.
const oracleRoot = "../../conformance/conformance-kit/oracle/targets/expected"

// loadOracle reads the expected golden for a target, or skips the test
// if the oracle has not been generated on this machine.
func loadOracle(t *testing.T, target string) oracleExpected {
	t.Helper()
	path := filepath.Join(oracleRoot, target+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("oracle not generated (%s); run tools/gen_oracle.py", path)
	}
	var exp oracleExpected
	if err := yaml.Unmarshal(data, &exp); err != nil {
		t.Fatalf("parse oracle %s: %v", path, err)
	}
	return exp
}

// oracleFileSet returns the deployed_files as a set for membership checks.
func oracleFileSet(exp oracleExpected) map[string]bool {
	set := make(map[string]bool, len(exp.DeployedFiles))
	for _, f := range exp.DeployedFiles {
		set[f] = true
	}
	return set
}
