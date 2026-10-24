package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/apm-go/apm/internal/manifest"
)

// userScopeRelativeReason is the Oracle's reason for a relative local path at
// user scope (install/package_resolution.py:113-137): HOME is not a project,
// so a relative path has no root there.
const userScopeRelativeReason = "relative local paths are not supported at user scope (--global). " +
	"Use an absolute path or a remote reference (owner/repo) instead"

// relativeAtUserScope reports whether ref is a local package whose path is
// relative once a leading ~ is expanded, the Oracle's
// `not Path(local_path).expanduser().is_absolute()`. It reads the path as
// normalizeLocalDep does, on a copy, so ref may be normalized already.
func relativeAtUserScope(ref *manifest.DependencyReference) bool {
	c := *ref
	normalizeLocalDep(&c)
	p := c.LocalDeclaredPath
	if p == "" || p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		return false
	}
	return !filepath.IsAbs(p)
}

// checkUserScopeLocalPaths refuses a local package of ~/.apm/apm.yml whose
// path is relative, before anything is fetched or written, as the command
// line refuses one. The Oracle prints its Resolving line, skips the package
// without a word and ends with "No changes" (install/phases/resolve.py:551-565,
// measured); apm-go refuses it instead, because a package skipped that way is
// one no longer declared, whose files a later run removes, as the Oracle's
// update -g plans to (measured, issue #128).
func checkUserScopeLocalPaths(m *manifest.Manifest) error {
	for _, dep := range allDirectDeps(m) {
		if relativeAtUserScope(dep) {
			return fmt.Errorf("validate apm.yml: local dependency %q: %s", dep.LocalDeclaredPath, userScopeRelativeReason)
		}
	}
	return nil
}
