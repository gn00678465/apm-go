//go:build unix

package main

import (
	"os"
	"path/filepath"
	"testing"
)

// writeStubScript writes a POSIX shell script at dir/name (0o755) and
// returns its path. Tests use these as stand-ins for both `apm` and
// `apm-go` so go test never invokes the real Oracle.
func writeStubScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing stub script: %v", err)
	}
	return path
}
