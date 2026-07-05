package gitops

import (
	"os/exec"
	"slices"
	"testing"
)

func TestSecureGitEnv_ContainsHardenedVars(t *testing.T) {
	// Arrange / Act
	env := SecureGitEnv()

	// Assert
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never"} {
		if !slices.Contains(env, want) {
			t.Errorf("SecureGitEnv() missing %q; got %v", want, env)
		}
	}
}

func TestSecureGitEnv_PreservesAmbientEnv(t *testing.T) {
	// Arrange
	t.Setenv("APM_SECURE_GIT_ENV_TEST_MARKER", "present")

	// Act
	env := SecureGitEnv()

	// Assert
	if !slices.Contains(env, "APM_SECURE_GIT_ENV_TEST_MARKER=present") {
		t.Errorf("SecureGitEnv() dropped an ambient env var; got %v", env)
	}
}

func TestApplySecureGitEnv_SetsCmdEnv(t *testing.T) {
	// Arrange
	cmd := exec.Command("git", "--version")

	// Act
	ApplySecureGitEnv(cmd)

	// Assert
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GCM_INTERACTIVE=never"} {
		if !slices.Contains(cmd.Env, want) {
			t.Errorf("ApplySecureGitEnv() did not set cmd.Env with %q; got %v", want, cmd.Env)
		}
	}
}
