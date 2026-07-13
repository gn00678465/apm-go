// Tests for audit.go's P0 #3 fix (register §3.1/§5): apm-go audit (bare)
// and Python's `apm audit` (bare) share a name but check different things
// (SHA-256 deployed-file re-verification vs. a hidden-Unicode scan) --
// --help must say so explicitly, and the wording is locked here so it
// cannot silently drift back into an unqualified same-name claim.
package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestAuditCmd_HelpDocumentsSemanticDifference(t *testing.T) {
	cmd := auditCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("audit --help returned error: %v", err)
	}
	out := buf.String()

	for _, token := range []string{"SHA", "Python", "Unicode"} {
		if !strings.Contains(out, token) {
			t.Errorf("audit --help output missing %q:\n%s", token, out)
		}
	}
	if !strings.Contains(out, "differs") && !strings.Contains(out, "does not") {
		t.Errorf("audit --help output lacks explicit semantic contrast:\n%s", out)
	}
	const contrastLine = "audit' (bare), which instead runs a hidden-Unicode scan and never"
	if !strings.Contains(out, contrastLine) {
		t.Errorf("audit --help output missing contrast line %q:\n%s", contrastLine, out)
	}
}
