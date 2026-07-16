package gitops

import (
	"os"
	"os/exec"
)

// SecureGitEnv returns a hardened environment slice (suitable for
// exec.Cmd.Env) layered on top of the current process environment. It is the
// single git-subprocess hardening point every call site in the project uses
// (directly, or via ApplySecureGitEnv / gitCommand):
//
//   - GIT_TERMINAL_PROMPT=0 disables git's interactive terminal prompt for
//     missing credentials.
//   - GIT_ASKPASS="" disables any configured GUI credential-prompt helper
//     that GIT_TERMINAL_PROMPT alone does not suppress.
//   - GCM_INTERACTIVE=never disables Git Credential Manager's own
//     interactive prompt/GUI, independent of both vars above.
//   - GIT_ALLOW_PROTOCOL restricts clone/fetch to an ALLOW-list of
//     transports, so a remote-helper transport such as "ext::<command>"
//     (which git would otherwise run as an arbitrary subprocess) in an
//     attacker-controlled dependency clone URL is refused by git before it
//     executes -- the primary fix for the git-ext supply-chain RCE.
//   - GIT_PROTOCOL_FROM_USER=0 marks these clones as non-user-initiated so
//     the protocol policy is enforced even for transports whose default
//     "user" policy would otherwise permit them.
//
// Without the prompt vars, a git subprocess run against a private or
// unreachable remote can hang indefinitely waiting on interactive
// credential input instead of failing fast -- mirroring the Python
// original's GitAuthEnvBuilder.setup_environment (deps/git_auth_env.py).
func SecureGitEnv() []string {
	return secureGitEnvWithProtocols(allowedGitProtocols)
}

// secureGitEnvWithProtocols is SecureGitEnv with an explicit transport
// allow-list, so a local-path clone can opt into the "file" transport
// (allowedGitProtocolsLocal) without widening the allow-list for every other
// git subprocess.
func secureGitEnvWithProtocols(protocols string) []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"GCM_INTERACTIVE=never",
		"GIT_ALLOW_PROTOCOL="+protocols,
		"GIT_PROTOCOL_FROM_USER=0",
	)
}

// ApplySecureGitEnv sets cmd.Env to SecureGitEnv(), the convenience form
// every git subprocess call site should use to harden itself against
// interactive credential prompts.
func ApplySecureGitEnv(cmd *exec.Cmd) {
	cmd.Env = SecureGitEnv()
}
