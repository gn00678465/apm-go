package experimental

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableDisableRoundtrip(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())

	if IsEnabled("registries") {
		t.Fatal("registries should default to disabled")
	}
	if err := RequireEnabled("registries"); err == nil ||
		!strings.Contains(err.Error(), "apm-go experimental enable registries") {
		t.Fatalf("RequireEnabled off: want hint, got %v", err)
	}

	if err := Enable("registries"); err != nil {
		t.Fatal(err)
	}
	if !IsEnabled("registries") {
		t.Error("registries should be enabled after Enable")
	}
	if err := RequireEnabled("registries"); err != nil {
		t.Errorf("RequireEnabled on: %v", err)
	}

	if err := Disable("registries"); err != nil {
		t.Fatal(err)
	}
	if IsEnabled("registries") {
		t.Error("registries should be disabled after Disable")
	}
}

func TestUnknownFlag(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	if err := Enable("nope"); err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Errorf("Enable unknown: %v", err)
	}
	if err := Disable("nope"); err == nil {
		t.Errorf("Disable unknown should error")
	}
}

func TestAllListsRegistries(t *testing.T) {
	found := false
	for _, f := range All() {
		if f.Name == "registries" && f.Hint != "" {
			found = true
		}
	}
	if !found {
		t.Error("All() must include the registries flag with a hint")
	}
}

func TestKnown(t *testing.T) {
	if f, ok := Known("registries"); !ok || f.Description == "" || f.Hint == "" {
		t.Errorf("registries must be known with description+hint, got %+v ok=%v", f, ok)
	}
	if _, ok := Known("nope"); ok {
		t.Error("unknown flag must not be reported as known")
	}
}

func TestEnablePersistsAcrossReload(t *testing.T) {
	t.Setenv("APM_CONFIG_DIR", t.TempDir())
	if err := Enable("registries"); err != nil {
		t.Fatal(err)
	}
	// fresh load() (no in-process cache) still sees it enabled
	if !load().Experimental["registries"] {
		t.Error("enabled flag must persist to config and re-load")
	}
}

func TestCorruptConfigTreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("APM_CONFIG_DIR", dir)
	os.WriteFile(filepath.Join(dir, "config.json"), []byte("{not json"), 0o644)
	if IsEnabled("registries") {
		t.Error("corrupt config must read as disabled, not panic/enable")
	}
}
