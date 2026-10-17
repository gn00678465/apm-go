package gitops

import (
	"path/filepath"
	"strings"
)

// autocrlfOff is the configuration every git command that writes a
// package's working tree runs with, so the tree has the bytes the repository
// holds whatever the user's core.autocrlf, as in the Oracle's git cache
// (cache/git_cache.py:63-92). It is given before the subcommand, which
// outranks a value a parent process passes in GIT_CONFIG_COUNT or
// GIT_CONFIG_PARAMETERS; a clone also gets it after `clone`, which writes it
// into the checkout's .git/config for pinsAutocrlfFalse. core.eol and
// .gitattributes still apply, as in the Oracle.
const autocrlfOff = "core.autocrlf=false"

// pinsAutocrlfFalse reports whether the checkout at installDir was cloned
// with autocrlfOff. One an earlier apm-go cloned under core.autocrlf=true
// has CRLF in its working tree, and git status still calls it clean, so it
// is cloned again, as the Oracle rematerializes such a cache checkout
// (cache/git_cache.py:95-127).
func pinsAutocrlfFalse(installDir string) bool {
	cmd := gitCommand("config", "--file", filepath.Join(installDir, ".git", "config"), "--type=bool", "--get", "core.autocrlf")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "false"
}
