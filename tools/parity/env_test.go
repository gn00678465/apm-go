//go:build unix

package main

import (
	"os"
	"testing"
)

func TestBuildEnv_AllowListAndFixedVars(t *testing.T) {
	t.Setenv("PATH", "/fake/bin")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "C")
	// A var not on the allow-list must never reach the child, proving this
	// is an allow-list and not "inherit everything".
	t.Setenv("APM_PARITY_TEST_SECRET", "leak-me-not")

	env := buildEnv(map[string]string{"CASE_VAR": "case-value"}, "/tmp/sandbox-home", "/tmp/sandbox-config", "/tmp/sandbox-launchercache")

	want := map[string]string{
		"PATH":           "/fake/bin",
		"LANG":           "en_US.UTF-8",
		"LC_ALL":         "C",
		"NO_COLOR":       "1",
		"CI":             "1",
		"TERM":           "dumb",
		"CASE_VAR":       "case-value",
		"HOME":           "/tmp/sandbox-home",
		"APM_CONFIG_DIR": "/tmp/sandbox-config",
		// The Oracle is launched through `uv run`, and uv writes its own
		// cache under $HOME/.cache/uv by default. That is the launcher's
		// artefact, not the product's, and it must never land inside an
		// evidence root -- so it is pinned to a sandbox dir OUTSIDE
		// home/cwd/config (ticket 02 attempt 4, orchestrator fix).
		"UV_CACHE_DIR": "/tmp/sandbox-launchercache",
	}
	for k, v := range want {
		if env[k] != v {
			t.Errorf("env[%s] = %q, want %q", k, env[k], v)
		}
	}

	if _, leaked := env["APM_PARITY_TEST_SECRET"]; leaked {
		t.Error("non-allow-listed env var leaked into the sandbox env")
	}
	if len(env) != len(want) {
		t.Errorf("env has %d keys, want exactly %d: %v", len(env), len(want), env)
	}
}

func TestBuildEnv_CaseCannotOverrideIsolationVars(t *testing.T) {
	// HOME and APM_CONFIG_DIR must always win over a case.json override,
	// since a case pointing them at the real user's directories would break
	// the isolation guarantee.
	env := buildEnv(map[string]string{
		"HOME":           "/evil/home",
		"APM_CONFIG_DIR": "/evil/config",
		"UV_CACHE_DIR":   "/evil/cache",
	}, "/sandbox/home", "/sandbox/config", "/sandbox/launcher-cache")

	if env["HOME"] != "/sandbox/home" {
		t.Errorf("HOME = %q, want sandbox value to win", env["HOME"])
	}
	if env["UV_CACHE_DIR"] != "/sandbox/launcher-cache" {
		t.Errorf("UV_CACHE_DIR = %q, want sandbox value to win", env["UV_CACHE_DIR"])
	}
	if env["APM_CONFIG_DIR"] != "/sandbox/config" {
		t.Errorf("APM_CONFIG_DIR = %q, want sandbox value to win", env["APM_CONFIG_DIR"])
	}
}

func TestBuildEnv_MissingAllowListedVarsAreOmitted(t *testing.T) {
	t.Setenv("PATH", "/fake/bin")
	t.Setenv("LANG", "en_US.UTF-8")
	// os.LookupEnv treats an explicitly empty value as present; this test
	// needs the genuinely-absent case, so unset rather than set-to-empty.
	original, hadLCAll := os.LookupEnv("LC_ALL")
	os.Unsetenv("LC_ALL")
	t.Cleanup(func() {
		if hadLCAll {
			os.Setenv("LC_ALL", original)
		}
	})

	env := buildEnv(nil, "/sandbox/home", "/sandbox/config", "/sandbox/launcher-cache")

	if _, ok := env["LC_ALL"]; ok {
		t.Errorf("LC_ALL present in env despite being unset: %q", env["LC_ALL"])
	}
}

func TestEnvSlice_SortedKeyEqualsValue(t *testing.T) {
	got := envSlice(map[string]string{"B": "2", "A": "1"})
	want := []string{"A=1", "B=2"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("envSlice = %v, want %v", got, want)
	}
}
