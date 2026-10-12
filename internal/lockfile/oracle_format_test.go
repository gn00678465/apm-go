package lockfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

// The files under testdata/oracle are apm.lock.yaml as the Oracle v0.33.0
// wrote them for the parity cases install-local-repeat, mcp-install-stdio
// and mcp-install-offline, and (local-skill) for a local package with one
// instruction and one skill of two files, whose directory the lock lists with
// no hash. Parsing one and writing it back must give the
// same bytes: that is the whole lock format, the deployments section
// included, which apm-go derives and does not read.
func TestWriteLockfile_ReproducesOracleBytes(t *testing.T) {
	for _, name := range []string{"local-and-project", "mcp-stdio", "mcp-http", "local-skill"} {
		t.Run(name, func(t *testing.T) {
			want, err := os.ReadFile(filepath.Join("testdata", "oracle", name+".lock.yaml"))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := yamlcore.SafeLoad(want)
			if err != nil {
				t.Fatal(err)
			}
			lf, err := ParseLockfile(doc)
			if err != nil {
				t.Fatal(err)
			}
			// Without the original node: every byte comes from the model.
			got, err := WriteLockfile(lf, nil)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != string(want) {
				t.Errorf("lock differs from the Oracle's\n--- got\n%s\n--- want\n%s", got, want)
			}
		})
	}
}
