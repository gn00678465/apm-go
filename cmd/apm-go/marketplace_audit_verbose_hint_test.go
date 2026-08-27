package main

import (
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/marketplace"
)

// Ticket 18. The Oracle wraps audit's whole body in `except Exception`
// (commands/marketplace/audit.py:140-143) and always prints
// "Run with --verbose for details." after the error before exiting 1.
// apm-go printed nothing after the error line.
//
// The --strict gates are the load-bearing exception: the Oracle reaches
// those through sys.exit(1), and SystemExit does not inherit from Exception,
// so audit.py:141 never runs for them. Testing only the happy hint would
// pass just as well with a blanket "always print the hint" implementation,
// which would be wrong -- so both halves are asserted here.

const auditVerboseHint = "[i] Run with --verbose for details."

func TestMarketplaceAudit_UnregisteredName_PrintsVerboseHintAfterError(t *testing.T) {
	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	out, err := runMarketplaceCmd(t, "audit", "nonexistent")

	// Assert
	if err == nil {
		t.Fatalf("audit of an unregistered marketplace returned no error (output: %s)", out)
	}
	if !strings.Contains(out, auditVerboseHint) {
		t.Errorf("output missing the Oracle's trailing hint %q\ngot:\n%s", auditVerboseHint, out)
	}
	// The hint follows the error; it never precedes or replaces it.
	errIdx := strings.Index(out, "[x] Failed to audit marketplace:")
	hintIdx := strings.Index(out, auditVerboseHint)
	if errIdx < 0 {
		t.Fatalf("output missing the error line itself\ngot:\n%s", out)
	}
	if hintIdx < errIdx {
		t.Errorf("hint printed before the error line\ngot:\n%s", out)
	}
	if got := exitCodeOf(err); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestMarketplaceAudit_ErrorIsPrintedOnceNotTwice(t *testing.T) {
	// The command now prints the error itself and returns a SILENT exit so
	// main()'s renderRootError does not print it a second time.

	// Arrange
	isolatedMarketplaceRegistry(t)

	// Act
	out, err := runMarketplaceCmd(t, "audit", "nonexistent")

	// Assert
	if err == nil {
		t.Fatal("expected an error")
	}
	if n := strings.Count(out, "[x] Failed to audit marketplace:"); n != 1 {
		t.Errorf("error line printed %d times, want exactly 1\ngot:\n%s", n, out)
	}
	if !isSilentExit(err) {
		t.Error("returned error is not a silent exit; renderRootError would print the message twice")
	}
}

func TestMarketplaceAudit_StrictGateFailures_OmitVerboseHint(t *testing.T) {
	// Arrange: one skipped plugin and one 404 -- zero plugins audited, so
	// --strict takes its sys.exit(1) path (audit.py:117-125), which the
	// Oracle's `except Exception` never sees.
	isolatedMarketplaceRegistry(t)
	dir := writeLocalManifestDir(t, `{"name": "acme", "plugins": [`+
		`{"name": "unsupported", "source": "./relative"},`+
		`{"name": "no-manifest", "source": {"type": "github", "repo": "acme/gone", "ref": "v1.0.0"}}`+
		`]}`)
	if err := marketplace.AddSource(marketplace.MarketplaceSource{Name: "acme", URL: dir, Path: "marketplace.json", Host: "github.com"}); err != nil {
		t.Fatal(err)
	}
	withApmYMLFetcher(t, &fakeCmdApmYMLFetcher{})

	// Act
	out, err := runMarketplaceCmd(t, "audit", "acme", "--strict")

	// Assert
	if err == nil {
		t.Fatalf("--strict with zero audited plugins returned no error (output: %s)", out)
	}
	if strings.Contains(out, auditVerboseHint) {
		t.Errorf("--strict gate failure must NOT print the catch-all hint (the Oracle exits via SystemExit, which its `except Exception` never catches)\ngot:\n%s", out)
	}
	if !strings.Contains(err.Error(), "no plugins were audited") {
		t.Errorf("error = %q, want the --strict explanation", err)
	}
}
