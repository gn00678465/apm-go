package marketplace

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// defaultSourceHost is the host assumed for an OWNER/REPO shorthand SOURCE
// string (design.md rule 5) when neither an embedded HOST segment nor a
// --host override is given.
const defaultSourceHost = "github.com"

// defaultSourceRef is the branch/ref assumed for any git-backed source
// (github/gitlab/git kinds) whose SOURCE string does not carry its own ref.
// The `add` command's "#ref" fragment and --ref flag (mkt-018) layer on top
// of this in a later step; that CLI-level parsing is out of scope here.
const defaultSourceRef = "main"

// defaultManifestPath is the manifest filename assumed when SOURCE does not
// point directly at a hosted marketplace.json (design.md rule 4's KindURL
// shortcut). mkt-003's fallback probing order (marketplace.json ->
// .github/plugin/marketplace.json -> .claude-plugin/marketplace.json) is
// the fetch clients' job, not this parser's.
const defaultManifestPath = "marketplace.json"

// ParseMarketplaceSource classifies a raw `marketplace add SOURCE` argument
// (plus a --host override, "" when not given) into a MarketplaceSource,
// following the discrimination order design.md prescribes for mkt-010: the
// first matching rule wins, not "the closest match".
//
//  1. Local path shape (looksLikeLocalPath) -> KindLocal, URL canonicalized
//     to an absolute path.
//  2. Bare "http://" -> hard error (https:// or a schemeless form only).
//  3. SCP-style SSH remote ("user@host:path") -> KindGit/GitHub/GitLab by
//     host, URL kept verbatim (the SSH transport must survive for client_
//     git.go's clone step).
//  4. Full "https://" URL -> KindURL if the path (ignoring a trailing
//     slash) ends in "/marketplace.json"; otherwise KindGitHub/GitLab/Git
//     by host.
//  5. Otherwise, OWNER/REPO, HOST/OWNER/REPO, or a deeper nested-group
//     shorthand (a HOST segment is only recognized when it has FQDN shape)
//     -> canonicalized to an https:// URL so a later Kind() re-derivation
//     (e.g. after a registry round-trip) agrees with the Kind chosen here.
//
// --host interacts with the resolved SOURCE per mkt-011 (revised):
//   - Conflicting with an embedded host -- a full URL's (rule 4) or a
//     shorthand's (rule 5) -- is a hard error.
//   - Matching a full URL's host, targeting a local path (rule 1), or
//     mismatching an SCP remote's host (rule 3) is downgraded to a warning
//     printed to os.Stderr; the override itself is never applied in any of
//     these cases.
//   - Matching a shorthand's embedded host (rule 5) is a silent no-op (no
//     warning).
//   - A shorthand with no embedded host (bare OWNER/REPO) is the only case
//     where --host actually applies.
func ParseMarketplaceSource(raw, host string) (*MarketplaceSource, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("marketplace source must not be empty")
	}
	if err := rejectControlCharacters(raw); err != nil {
		return nil, err
	}

	if looksLikeLocalPath(raw) {
		return parseLocalSource(raw, host)
	}
	if strings.HasPrefix(strings.ToLower(raw), "http://") {
		// Never echo raw here: a bare http:// SOURCE could carry
		// user:pass@ credentials in its userinfo (credsec convention, see
		// internal/mcpregistry.NewClient).
		return nil, fmt.Errorf("marketplace source does not support http://; use https:// or a schemeless OWNER/REPO form")
	}
	if m := scpLikeSourceRe.FindStringSubmatch(raw); m != nil {
		return parseSCPSource(raw, m[1], host)
	}
	if strings.HasPrefix(strings.ToLower(raw), "https://") {
		return parseHTTPSSource(raw, host)
	}
	return parseShorthandSource(raw, host)
}

// rejectControlCharacters mirrors the Python original's control-character
// guard (__init__.py:280-281): any rune below 0x20 (space) anywhere in the
// raw SOURCE string is rejected outright, before any shape-specific
// parsing runs.
func rejectControlCharacters(raw string) error {
	for _, r := range raw {
		if r < 0x20 {
			return fmt.Errorf("marketplace source contains invalid control characters")
		}
	}
	return nil
}

// parseLocalSource implements design.md rule 1. --host is meaningless for a
// local source, so any given value is warned about and ignored.
//
// mkt B5: when the resolved path is itself an existing file (e.g.
// "./dir/marketplace.json"), Path is left "" so fetchLocal (client_local.go)
// reads that file directly instead of probing mkt-003's fallback candidates
// underneath it as if it were a directory. Mirrors the Python original's
// _local_source_points_to_file, which makes the same file-vs-directory
// check at add-time. A nonexistent or directory path keeps the default
// probing behavior.
func parseLocalSource(raw, host string) (*MarketplaceSource, error) {
	if host != "" {
		warnHostIgnored(host, "the marketplace source is a local path; --host has no effect")
	}
	resolved, err := resolveLocalPath(raw)
	if err != nil {
		return nil, fmt.Errorf("resolving local marketplace source: %w", err)
	}
	path := defaultManifestPath
	if info, statErr := os.Stat(resolved); statErr == nil && !info.IsDir() {
		path = ""
	}
	return &MarketplaceSource{
		URL:  resolved,
		Path: path,
	}, nil
}

// resolveLocalPath canonicalizes a local SOURCE string to an absolute path
// (design.md: "URL 存絕對路徑"). Backslashes are normalized to slashes
// first so a Windows-shaped input ("C:\..", ".\rel", "~\rel") is
// interpreted consistently regardless of which separator style the raw
// SOURCE string used, then handed to filepath so the OS resolves the
// platform-specific absolute form.
func resolveLocalPath(raw string) (string, error) {
	normalized := strings.ReplaceAll(raw, `\`, "/")

	switch {
	case strings.HasPrefix(normalized, "file://"):
		normalized = strings.TrimPrefix(normalized, "file://")
	case normalized == "~" || strings.HasPrefix(normalized, "~/"):
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving home directory: %w", err)
		}
		normalized = filepath.ToSlash(home) + strings.TrimPrefix(normalized, "~")
	}

	abs, err := filepath.Abs(filepath.FromSlash(normalized))
	if err != nil {
		return "", err
	}
	return abs, nil
}

// parseSCPSource implements design.md rule 3. sshHost is the host segment
// scpLikeSourceRe already extracted from raw. mkt B3 parity: the path
// segment(s) after "user@host:" are validated for traversal markers the
// same way an https:// URL's path segments are (__init__.py:303-304).
func parseSCPSource(raw, sshHost, hostOverride string) (*MarketplaceSource, error) {
	if err := validateSCPPathSegments(raw, "marketplace SSH path"); err != nil {
		return nil, err
	}
	src := &MarketplaceSource{
		URL:  raw,
		Ref:  defaultSourceRef,
		Path: defaultManifestPath,
		Host: sshHost,
	}
	if owner, repo, ok := splitOwnerRepoFromSCPPath(raw); ok {
		src.Owner = owner
		src.Repo = repo
	}
	if hostOverride != "" && !strings.EqualFold(hostOverride, sshHost) {
		warnHostIgnored(hostOverride, fmt.Sprintf("does not match the SSH remote host %q", sshHost))
	}
	return src, nil
}

// validateSCPPathSegments validates every "/"-separated segment of the path
// portion of an SCP-style remote (the part after the first ":") for
// traversal markers. Unlike splitOwnerRepoFromSCPPath, this validates the
// raw segments verbatim (no ".git" suffix stripping), matching Python's
// validation-before-parsing order.
func validateSCPPathSegments(raw, context string) error {
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return nil
	}
	for _, seg := range nonEmptySegments(raw[idx+1:]) {
		if err := validateSourcePathSegment(seg, context); err != nil {
			return err
		}
	}
	return nil
}

// splitOwnerRepoFromSCPPath extracts owner/repo from the path segment of an
// SCP-style remote (the part after the first ":"), tolerating a trailing
// ".git" suffix and (for hosts with nested groups, e.g. self-hosted
// GitLab) taking the last two path segments as owner/repo.
func splitOwnerRepoFromSCPPath(raw string) (owner, repo string, ok bool) {
	idx := strings.Index(raw, ":")
	if idx < 0 {
		return "", "", false
	}
	path := strings.Trim(strings.TrimSuffix(raw[idx+1:], ".git"), "/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[len(parts)-2], parts[len(parts)-1], true
}

// parseHTTPSSource implements design.md rule 4.
//
// mkt B3: every path segment (u.Path is already percent-decoded by
// net/url, so "%2E%2E" arrives as "..") is checked for a traversal marker
// before anything else. mkt B4: a completely path-less URL is always a hard
// error (aligned with the Python original's "HTTPS URL is missing a repo
// path" check, __init__.py:322-330); for a github/gitlab-family host
// specifically, fewer than two path segments is also a hard error (no
// owner to resolve), while a generic git host may legitimately have a
// single segment (e.g. a self-hosted "https://gitea.example.com/repo").
func parseHTTPSSource(raw, hostOverride string) (*MarketplaceSource, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parsing marketplace source URL: %w", err)
	}
	if u.User != nil {
		// credsec: never echo raw here, it may carry the credential this
		// check just found.
		return nil, fmt.Errorf("marketplace source URL must not contain embedded credentials")
	}
	urlHost := u.Hostname()
	if urlHost == "" {
		return nil, fmt.Errorf("marketplace source URL is missing a host")
	}

	pathSegments := nonEmptySegments(u.Path)
	for _, seg := range pathSegments {
		if err := validateSourcePathSegment(seg, "marketplace source URL path"); err != nil {
			return nil, err
		}
	}

	if urlNamesRemoteManifest(raw) {
		if err := reconcileFullURLHost(hostOverride, urlHost); err != nil {
			return nil, err
		}
		return &MarketplaceSource{
			URL:  raw,
			Host: urlHost,
		}, nil
	}

	if len(pathSegments) == 0 {
		return nil, fmt.Errorf("marketplace source URL %q is missing a repo path", raw)
	}
	if kind := classifySourceHost(urlHost); (kind == KindGitHub || kind == KindGitLab) && len(pathSegments) < 2 {
		return nil, fmt.Errorf("marketplace source URL %q is invalid: Expected 'OWNER/REPO' in the URL path", raw)
	}

	if err := reconcileFullURLHost(hostOverride, urlHost); err != nil {
		return nil, err
	}
	owner, repo := splitOwnerRepoFromURLPath(u.Path)
	return &MarketplaceSource{
		URL:   raw,
		Ref:   defaultSourceRef,
		Path:  defaultManifestPath,
		Host:  urlHost,
		Owner: owner,
		Repo:  repo,
	}, nil
}

// reconcileFullURLHost applies mkt-011 to a source whose host is fully
// determined by an embedded https:// URL (rule 4, including the direct
// marketplace.json case): --host can never actually change anything here,
// so it either agrees (warned and ignored) or conflicts (hard error).
func reconcileFullURLHost(hostOverride, urlHost string) error {
	if hostOverride == "" {
		return nil
	}
	if !strings.EqualFold(hostOverride, urlHost) {
		return fmt.Errorf("--host %q conflicts with the marketplace source URL's host %q", hostOverride, urlHost)
	}
	warnHostIgnored(hostOverride, "the marketplace source is a full URL; --host has no effect")
	return nil
}

// splitOwnerRepoFromURLPath extracts owner/repo from an https URL's path
// (e.g. "/owner/repo" or "/owner/repo.git"), tolerating a trailing ".git"
// suffix and additional path segments beyond owner/repo.
func splitOwnerRepoFromURLPath(path string) (owner, repo string) {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return "", ""
	}
	parts := strings.SplitN(trimmed, "/", 3)
	if len(parts) < 2 {
		return parts[0], ""
	}
	return parts[0], strings.TrimSuffix(parts[1], ".git")
}

// parseShorthandSource implements design.md rule 5: OWNER/REPO,
// HOST/OWNER/REPO, or a deeper nested-group path (OWNER/GROUP/.../REPO or
// HOST/OWNER/GROUP/.../REPO).
//
// mkt B2: the first "/"-segment is only treated as an embedded HOST when it
// has FQDN shape (looksLikeFQDN, aligned with Python's is_valid_fqdn gate in
// commands/marketplace/__init__.py) -- otherwise there is no embedded host
// and the *entire* string is OWNER path (which may itself contain "/", e.g.
// a nested GitLab group) + REPO (the final segment). This also naturally
// supports 4+ segment shorthands (HOST/OWNER/GROUP/REPO and
// OWNER/GROUP/REPO) since the owner path is not limited to one segment.
//
// mkt B1 (mkt-011 revised): an embedded host that disagrees with --host
// (case-insensitively) is a hard error; agreeing is a silent no-op (no
// warning -- unlike the full-URL case, a match here is not itself
// noteworthy). --host only ever "wins" over defaultSourceHost when there is
// no embedded host to conflict with.
func parseShorthandSource(raw, hostOverride string) (*MarketplaceSource, error) {
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		decoded = raw
	}
	segments := nonEmptySegments(decoded)
	if len(segments) < 2 {
		return nil, invalidSourceShapeError(raw)
	}

	var embeddedHost string
	if looksLikeFQDN(segments[0]) {
		if len(segments) < 3 {
			return nil, invalidSourceShapeError(raw)
		}
		embeddedHost = strings.ToLower(segments[0])
		segments = segments[1:]
	}

	repo := segments[len(segments)-1]
	ownerSegments := segments[:len(segments)-1]
	if len(ownerSegments) == 0 || repo == "" {
		return nil, invalidSourceShapeError(raw)
	}
	for _, seg := range ownerSegments {
		if err := validateSourcePathSegment(seg, "marketplace source owner path"); err != nil {
			return nil, err
		}
	}
	if err := validateSourcePathSegment(repo, "marketplace source repo name"); err != nil {
		return nil, err
	}

	if embeddedHost != "" && hostOverride != "" && !strings.EqualFold(hostOverride, embeddedHost) {
		return nil, fmt.Errorf("--host %q conflicts with embedded host %q in marketplace source %q", hostOverride, embeddedHost, raw)
	}

	effectiveHost := defaultSourceHost
	if embeddedHost != "" {
		effectiveHost = embeddedHost
	}
	if hostOverride != "" {
		effectiveHost = hostOverride
	}

	owner := strings.Join(ownerSegments, "/")
	return &MarketplaceSource{
		URL:   fmt.Sprintf("https://%s/%s/%s", effectiveHost, owner, repo),
		Ref:   defaultSourceRef,
		Path:  defaultManifestPath,
		Host:  effectiveHost,
		Owner: owner,
		Repo:  repo,
	}, nil
}

func invalidSourceShapeError(raw string) error {
	return fmt.Errorf("marketplace source %q is not a recognized SOURCE shape (expected a local path, https:// URL, SCP-style SSH remote, OWNER/REPO, or HOST/OWNER/REPO)", raw)
}

// warnHostIgnored writes a non-fatal mkt-011 diagnostic to os.Stderr: a
// --host value was given but ultimately had no effect.
func warnHostIgnored(hostOverride, reason string) {
	fmt.Fprintf(os.Stderr, "[warn] --host %q ignored: %s\n", hostOverride, reason)
}

// fqdnShapeRe matches a bare hostname shape, mirroring Python's
// is_valid_fqdn (utils/github_host.py): labels of alphanumerics/hyphens
// (never starting or ending with a hyphen), joined by at least one ".".
// Used by parseShorthandSource (mkt B2) to decide whether the first
// "/"-segment of a shorthand SOURCE is an embedded HOST or the start of an
// OWNER path.
var fqdnShapeRe = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

func looksLikeFQDN(s string) bool {
	return fqdnShapeRe.MatchString(s)
}

// nonEmptySegments splits raw on "/" and drops empty segments (from a
// leading/trailing or doubled "/"), mirroring the Python original's
// "[seg for seg in raw.split('/') if seg]" idiom used for both SOURCE
// shorthand and https URL path parsing.
func nonEmptySegments(raw string) []string {
	parts := strings.Split(raw, "/")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// isTraversalSegment reports whether a (percent-decoded) path segment is a
// traversal marker.
func isTraversalSegment(s string) bool {
	return s == "." || s == ".." || s == "~"
}

// validateSourcePathSegment rejects a raw SOURCE path segment shaped like a
// traversal marker or empty. segment is iteratively percent-decoded (up to
// 8 rounds, matching Python's validate_path_segments in
// utils/path_security.py) so a multi-encoded marker (e.g. "%252e%252e")
// cannot slip through disguised as an opaque segment (mkt B3).
func validateSourcePathSegment(segment, context string) error {
	if segment == "" {
		return fmt.Errorf("invalid %s: path segments must not be empty", context)
	}
	decoded := segment
	for i := 0; i < 8; i++ {
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			break
		}
		decoded = next
	}
	if isTraversalSegment(segment) || isTraversalSegment(decoded) {
		return fmt.Errorf("invalid %s: segment %q is a traversal sequence", context, segment)
	}
	return nil
}
