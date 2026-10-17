package gitops

import (
	"path/filepath"
	"strings"
)

// autocrlfOff is the configuration every clone of a package is made with, so
// the working tree has the bytes the repository holds whatever the user's
// core.autocrlf, as in the Oracle's git cache (cache/git_cache.py:63-92).
// git clone writes it into the checkout's .git/config, where the checkout of
// cloneRepoAtCommit and every later git command in the checkout read it.
// core.eol and .gitattributes still apply, as in the Oracle.
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
