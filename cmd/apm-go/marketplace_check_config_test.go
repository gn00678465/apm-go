package main

import (
	"os"
	"testing"
)

// SPEC marketplace-check-outdated SC-A5: a config validation error exits 2
// with the Oracle's "marketplace config error: <msg>" line
// (commands/marketplace/__init__.py:148-172 _load_config_or_exit).

func writeInvalidMarketplaceConfig(t *testing.T) {
	t.Helper()
	chdirTemp(t)
	apmYML := "name: demo\nversion: 1.0.0\nmarketplace:\n" +
		"  owner:\n    name: acme\n" +
		"  packages:\n" +
		"    - name: bare\n      source: owner/repo\n"
	if err := os.WriteFile("apm.yml", []byte(apmYML), 0o644); err != nil {
		t.Fatal(err)
	}
}

const wantConfigErrorLine = "marketplace config error: packages[0] ('bare'): remote packages require at least one of 'version' or 'ref'"

func TestMarketplaceCheck_ConfigError_ExitsTwo(t *testing.T) {
	writeInvalidMarketplaceConfig(t)

	_, err := runMarketplaceCmd(t, "check")

	if err == nil {
		t.Fatal("marketplace check accepted a config the Oracle rejects at load time")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if got := err.Error(); got != wantConfigErrorLine {
		t.Errorf("err = %q, want %q", got, wantConfigErrorLine)
	}
}

func TestMarketplaceOutdated_ConfigError_ExitsTwo(t *testing.T) {
	writeInvalidMarketplaceConfig(t)

	_, err := runMarketplaceCmd(t, "outdated")

	if err == nil {
		t.Fatal("marketplace outdated accepted a config the Oracle rejects at load time")
	}
	if got := exitCodeOf(err); got != 2 {
		t.Errorf("exitCodeOf(err) = %d, want 2", got)
	}
	if got := err.Error(); got != wantConfigErrorLine {
		t.Errorf("err = %q, want %q", got, wantConfigErrorLine)
	}
}
