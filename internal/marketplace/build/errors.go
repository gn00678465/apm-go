package build

import "fmt"

// HeadNotAllowedError is mkt-055's gate: a package's ref resolves to a
// branch name or HEAD. Branch refs are mutable, so a marketplace.json built
// against one would not be reproducible, and `apm pack` (unlike the Python
// original) exposes no "--allow-head" escape hatch -- so this is always a
// hard failure.
//
// The message deliberately does NOT mention "--allow-head": the Python
// original's error string (errors.py:106) tells the user to pass that flag
// to override, but `apm pack` never defines it (design.md), so copying that
// text verbatim would point users at a flag that does not exist.
type HeadNotAllowedError struct {
	Package string
	Ref     string
}

func (e *HeadNotAllowedError) Error() string {
	return fmt.Sprintf(
		"package %q resolves to branch/HEAD ref %q; branch refs are mutable and not reproducible -- pin to a tag or commit SHA instead",
		e.Package, e.Ref,
	)
}

// NoMatchingVersionError is mkt-051's failure when no remote tag (after
// tagPattern filtering) satisfies a package's declared semver range.
type NoMatchingVersionError struct {
	Package string
	Range   string
	Detail  string
}

func (e *NoMatchingVersionError) Error() string {
	detail := ""
	if e.Detail != "" {
		detail = fmt.Sprintf(" (%s)", e.Detail)
	}
	return fmt.Sprintf("no tag matching version %q found for package %q%s", e.Range, e.Package, detail)
}

// RefNotFoundError is mkt-051's failure when an explicit ref: pin (not a
// 40-hex lowercase SHA, not a tag, not a branch, not HEAD) matches nothing
// on the remote at all.
type RefNotFoundError struct {
	Package string
	Ref     string
	Remote  string
}

func (e *RefNotFoundError) Error() string {
	return fmt.Sprintf("ref %q not found on remote %q for package %q", e.Ref, e.Remote, e.Package)
}
