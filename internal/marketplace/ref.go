package marketplace

import (
	"fmt"
	"regexp"
	"strings"
)

// marketplaceRefHeadRe matches the "plugin@marketplace" head of a CLI
// PLUGIN@MARKETPLACE[#REF] reference (mkt-020). Both segments are
// restricted to [a-zA-Z0-9._-]+ (the same alias-format charset as mkt-004),
// which also implicitly rejects any head containing "/" or ":" -- covering
// design.md rule 2 (local paths, SCP remotes, and "owner/repo" shorthands
// all fall through this way) without a separate pre-check.
var marketplaceRefHeadRe = regexp.MustCompile(`^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+$`)

// semverRangeCharRe matches any character that only appears in a semver
// range constraint (~^<>=!), never in a raw git tag/branch/SHA -- mirrors
// the Python original's _SEMVER_RANGE_CHARS (mkt-021).
var semverRangeCharRe = regexp.MustCompile(`[~^<>=!]`)

// ParseRef recognizes a CLI dependency string shaped like
// "PLUGIN@MARKETPLACE" or "PLUGIN@MARKETPLACE#REF" (mkt-020/021).
//
// ok=false (err=nil) means s is not a marketplace reference: the caller
// must fall through to the general dependency-string parser
// (manifest.ParseDepString). This is the ONLY place that decision is
// allowed to happen (design.md's interception-layer decision) -- callers
// (install, and future uninstall/view) must not add their own
// strings.Contains(s, "/")-style pre-checks; that duplication is exactly
// what caused the Python original's install/uninstall behavior to diverge.
//
// Rules (design.md "ParseRef(mkt-020/021)" -- resolver semantics: head is
// checked BEFORE any "#" split is validated, NOT the Python install
// layer's whole-string quirk):
//
//  0. s is trimmed of leading/trailing whitespace first.
//  1. s is split on the FIRST "#" into head/ref (a second "#" inside ref,
//     e.g. "pkg@mkt#a#b", stays part of ref -- ref="a#b").
//  2. head must match ^[a-zA-Z0-9._-]+@[a-zA-Z0-9._-]+$; anything else
//     (including any head containing "/" or ":") falls through.
//  3. If "#" was present but ref is empty (e.g. "pkg@mkt#" or, after
//     trimming, "pkg@mkt# "), falls through -- mirrors the Python
//     original's whole-string regex simply not matching (this is not an
//     error, and not an accepted empty ref).
//  4. ref containing any of [~^<>=!] is a hard error naming "semver
//     range" (mkt-021): #REF only accepts a raw git tag/branch/SHA, never
//     a semver range constraint (those belong in the apm.yml dict form's
//     version: field, mkt-033).
//
// Deviation (recorded for the A/B exception list, design.md): unlike the
// Python original's install layer, "pkg@mkt#feature/branch" is accepted
// here even though ref contains "/". The Python install command has a
// coarser "/" not in package pre-check that rejects this shape, while its
// own resolver/uninstall path accepts it -- a known upstream
// inconsistency. This package implements only the resolver semantics, so
// install/uninstall/view stay consistent with each other in the Go port.
func ParseRef(s string) (plugin, mkt, ref string, ok bool, err error) {
	s = strings.TrimSpace(s)

	head := s
	hasFragment := false
	if idx := strings.Index(s, "#"); idx >= 0 {
		head = s[:idx]
		ref = s[idx+1:]
		hasFragment = true
	}

	if !marketplaceRefHeadRe.MatchString(head) {
		return "", "", "", false, nil
	}
	if hasFragment && ref == "" {
		return "", "", "", false, nil
	}

	if semverRangeCharRe.MatchString(ref) {
		return "", "", "", false, fmt.Errorf(
			"marketplace ref %q looks like a semver range constraint; #REF only accepts a raw git tag/branch/SHA, not a semver range (use the apm.yml dict form's version: field for ranges)",
			ref,
		)
	}

	plugin, mkt, _ = strings.Cut(head, "@")
	return plugin, mkt, ref, true, nil
}
