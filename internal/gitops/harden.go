package gitops

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// allowedGitProtocols is the DEFAULT allow-list SecureGitEnv hands to every
// git subprocess via GIT_ALLOW_PROTOCOL. It is an ALLOW-list, so any
// transport not named here -- crucially the remote-helper transports "ext::"
// (runs an arbitrary command) and "fd::", plus anything else git might grow
// -- is refused by git itself before it connects. This is the primary
// defense against the git-ext transport RCE (a dependency whose clone URL is
// "ext::sh -c '...'" reaching git clone): git errors out rather than run the
// helper command. https/ssh/git cover real remotes. "file" is DELIBERATELY
// EXCLUDED from the default so a malicious remote dependency can never coerce
// a local-filesystem read (e.g. via a future recursive-submodule fetch whose
// .gitmodules points file:// at a known local repo) -- only the explicit,
// top-level local `git: ./path` clone opts into it (allowedGitProtocolsLocal).
const allowedGitProtocols = "https:ssh:git"

// allowedGitProtocolsLocal additionally permits git's local "file" transport,
// used ONLY for an explicit top-level local `git: ./path` dependency clone
// (isLocalCloneURL) -- never for a remote clone, so an untrusted remote can
// never reach the file protocol.
const allowedGitProtocolsLocal = allowedGitProtocols + ":file"

// gitCommand builds an *exec.Cmd for `git args...` with the project's full
// git hardening applied (SecureGitEnv: no interactive prompts + the default
// remote GIT_ALLOW_PROTOCOL allow-list). EVERY git invocation in this
// package MUST go through it (or gitLocalCloneCommand for a local-path
// clone), never exec.Command("git", ...) directly, so the hardening can
// never be forgotten at a call site.
func gitCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	ApplySecureGitEnv(cmd)
	return cmd
}

// gitLocalCloneCommand is gitCommand for the one case that needs the local
// "file" transport: cloning an explicit local `git: ./path` dependency. It
// is never used for a remote clone.
func gitLocalCloneCommand(args ...string) *exec.Cmd {
	cmd := exec.Command("git", args...)
	cmd.Env = secureGitEnvWithProtocols(allowedGitProtocolsLocal)
	return cmd
}

// cloneCommandFor returns the hardened git command for cloning url: the
// file-permitting variant only when url is a local filesystem path, the
// remote-only default otherwise.
func cloneCommandFor(url string, args ...string) *exec.Cmd {
	if isLocalCloneURL(url) {
		return gitLocalCloneCommand(args...)
	}
	return gitCommand(args...)
}

// ApplyCloneEnv is ApplySecureGitEnv for a git subprocess that operates on a
// specific clone/ls-remote URL: it permits the local "file" transport ONLY
// when url is a local filesystem path, keeping a remote URL file-blocked.
// Callers outside this package (e.g. internal/marketplace's own git clone /
// ls-remote sites) use this instead of ApplySecureGitEnv so a local
// fixture/dependency URL works while an untrusted remote can never reach the
// file transport.
func ApplyCloneEnv(cmd *exec.Cmd, url string) {
	if isLocalCloneURL(url) {
		cmd.Env = secureGitEnvWithProtocols(allowedGitProtocolsLocal)
		return
	}
	ApplySecureGitEnv(cmd)
}

// isLocalCloneURL reports whether git will treat url as a LOCAL filesystem
// path (and thus whether the file-permitting env may be used). It is
// deliberately CONSERVATIVE: anything that could reach the network is
// classified NOT-local so it can never obtain the file transport.
//
//   - "scheme://..."      -> remote (has an explicit transport scheme).
//   - "\\host\share" or "//host/share" (UNC / network path) -> NOT local:
//     git would do an SMB/network fetch on Windows, which can leak NTLM
//     credentials -- a remote reach that must never get the file env.
//   - "host:path" / "user@host:path" (SCP form: a ":" with no "/" or "\"
//     before it) -> remote. A Windows drive path "C:\..." / "C:/..." is
//     explicitly excluded from SCP (single drive letter, colon, separator).
//   - everything else (./x, ../x, ~/x, /abs, C:\abs, bare "remote", sub/dir)
//     -> local.
func isLocalCloneURL(url string) bool {
	if strings.Contains(url, "://") {
		return false
	}
	// Any two leading slashes -- "\\", "//", or a mixed "\/" / "/\" -- is a
	// UNC / network path (git-for-Windows normalizes the mixed forms too), so
	// it must never be treated as local.
	if len(url) >= 2 && isSlash(url[0]) && isSlash(url[1]) {
		return false
	}
	if isSCPLikeURL(url) {
		return false
	}
	return true
}

func isSlash(b byte) bool { return b == '/' || b == '\\' }

// isSCPLikeURL reports whether url is git's SCP-style remote form
// "[user@]host:path" -- a ":" that appears before any "/" (a "\" does NOT
// terminate SCP detection, so "DOMAIN\user@host:repo" is still SCP remote),
// excluding a Windows drive-letter prefix ("C:\" / "C:/") ON WINDOWS ONLY.
// On non-Windows
// a "C:/..." string is NOT a drive path (there are no drive letters), so it is
// left classified as SCP remote -- the conservative choice, since that only
// ever DENIES the file transport.
func isSCPLikeURL(url string) bool {
	// Only ":" and "/" delimit SCP detection -- NOT "\": git treats a ":"
	// reached before any "/" as SCP even when a "\" precedes it (e.g.
	// "DOMAIN\user@host:repo"), and a UNC "\\..." path is already caught by
	// isLocalCloneURL's two-leading-slash check before this runs.
	i := strings.IndexAny(url, ":/")
	if i < 0 || url[i] != ':' {
		return false // no colon, or a "/" comes first -> a path
	}
	if runtime.GOOS == "windows" && i == 1 && isASCIILetter(url[0]) {
		rest := url[2:]
		if rest == "" || rest[0] == '/' || rest[0] == '\\' {
			return false // "C:\" / "C:/" drive path -> local, not SCP
		}
	}
	return true
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}

// validateCloneURL rejects a clone URL that could be re-interpreted by git as
// a remote-helper transport or an option, BEFORE it is ever passed to git.
// It is defense-in-depth in front of GIT_ALLOW_PROTOCOL (harden.go's env
// guard): the two URL shapes resolveCloneURL returns VERBATIM without
// building them from validated owner/repo parts -- a local `git: ./path`
// dependency and a bare `{name: ...}` dependency -- are the only ones that
// can carry an attacker-controlled string here, so both are checked.
//
//   - A "<helper>::..." remote-helper spec (ext::, fd::, etc.) is rejected
//     outright. The check is "contains :: but is not a real URL scheme": a
//     legitimate "scheme://" always has a single ":" followed by "//", never
//     "::". SCP form (git@host:path) has a single ":" too. Only helper specs
//     use "::".
//   - A leading "-" would let git parse the URL as an option (argument
//     injection); rejected. resolveCloneURL's constructed URLs never start
//     with "-", and local paths must start with ./ ../ / or ~, so this only
//     ever fires on a crafted value.
func validateCloneURL(url string) error {
	if strings.HasPrefix(url, "-") {
		return fmt.Errorf("refusing clone URL %q: begins with '-' (would be parsed as a git option)", url)
	}
	if strings.Contains(url, "::") {
		return fmt.Errorf("refusing clone URL %q: '::' remote-helper transport (e.g. ext::) is not permitted", url)
	}
	return nil
}
