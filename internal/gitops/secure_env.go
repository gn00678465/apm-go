package gitops

import (
	"os"
	"os/exec"
)

// SecureGitEnv returns a hardened environment slice (suitable for
// exec.Cmd.Env) layered on top of the current process environment:
//
//   - GIT_TERMINAL_PROMPT=0 disables git's interactive terminal prompt for
//     missing credentials.
//   - GIT_ASKPASS="" disables any configured GUI credential-prompt helper
//     that GIT_TERMINAL_PROMPT alone does not suppress.
//   - GCM_INTERACTIVE=never disables Git Credential Manager's own
//     interactive prompt/GUI, independent of both vars above.
//
// Without this, a git subprocess run against a private or unreachable
// remote can hang indefinitely waiting on interactive credential input
// instead of failing fast -- mirroring the Python original's
// GitAuthEnvBuilder.setup_environment (deps/git_auth_env.py).
func SecureGitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GCM_INTERACTIVE=never",
	)
}

// ApplySecureGitEnv sets cmd.Env to SecureGitEnv(), the convenience form
// every git subprocess call site should use to harden itself against
// interactive credential prompts.
func ApplySecureGitEnv(cmd *exec.Cmd) {
	cmd.Env = SecureGitEnv()
}
