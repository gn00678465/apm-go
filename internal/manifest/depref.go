package manifest

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v4"
)

type DependencyReference struct {
	RepoURL string
	// ArtifactoryPrefix is the VCS route prefix (for example,
	// "artifactory/github") stripped from RepoURL. It is populated only for
	// Artifactory coordinates and is used when rebuilding the clone URL.
	ArtifactoryPrefix string
	Host              string
	Owner             string
	Repo              string
	Reference         string
	VirtualPath       string
	VirtualType       string // "file" or "subdirectory"
	Alias             string
	IsLocal           bool
	LocalPath         string
	// LocalSourcePath is a RUNTIME-ONLY materialization detail: for a
	// dependency that must be vendored into apm_modules by COPYING a local
	// directory (rather than git-cloning), it holds the real absolute
	// filesystem source path. RepoURL then carries the sanitized, contained
	// "_local/<name>" apm_modules KEY (used by the resolver, deploy, and
	// lockfile), keeping the invalid-path-as-key problem out of every
	// filepath.Join(apm_modules, key). Empty for git/registry/marketplace
	// deps. Never serialized to apm.yml or the lockfile.
	LocalSourcePath string
	IsParent        bool
	Port            int
	Scheme          string // "https", "http", "ssh", "git" (SCP)
	Source          string // "git", "registry", "local", "marketplace", "" (inferred)
	RegistryName    string // registry name for source=="registry" (empty = use default)

	// SSHUser is the parsed userinfo for an ssh:// or SCP-form dependency
	// whose git remote needs a non-default user (e.g. an EMU/self-hosted
	// GHE SSH account) -- ticket 11 attempt 5, matching the Oracle's
	// DependencyReference.ssh_user (reference.py's _parse_ssh_protocol_url/
	// _parse_ssh_url both return one, validated by validate_ssh_user).
	// "" means the implicit default "git", kept empty (not "git") so every
	// existing caller/serialization this field's introduction doesn't
	// change stays byte-identical. NOT wired into the actual git-clone URL
	// builder (internal/gitops/clone.go hardcodes "git@") -- that is a
	// materialize-time concern, out of this ticket's Structure-validation
	// scope; the field exists so ParseDepString's accept/reject boundary
	// (what this ticket's fixtures exercise) matches the Oracle instead of
	// silently discarding information the Oracle's own parse() carries.
	SSHUser string

	// Marketplace* fields (mkt-033) are only ever set for Source=="marketplace"
	// -- an apm.yml dependencies.apm dict entry of the form {name, marketplace,
	// version} straight out of ParseDepDict, still unresolved. RepoURL for
	// such an entry is the "_marketplace/<marketplace>/<name>" placeholder
	// (mirrors the Python original's DependencyReference.to_apm_yml_entry
	// dedup key), not a real repository coordinate.
	MarketplaceName        string // registered marketplace name (dict "marketplace:" key), case preserved
	MarketplacePluginName  string // plugin name within that marketplace (dict "name:" key), case preserved
	MarketplaceVersionSpec string // dict "version:" key verbatim; "" if absent. Parse time performs no semver/format validation (mkt-033)

	// SkillSubset is the persisted per-dep skill whitelist from an apm.yml
	// dict entry's `skills:` field (BUG-2, prd.md B2-1): nil means "no
	// subset declared" (install/deploy all skills in the bundle); a
	// non-nil, non-empty, trimmed/deduplicated/sorted slice restricts
	// deployment to just those skill names. Only ever populated for the
	// git dict form (ParseDepDict); every other dict form rejects the key
	// outright rather than silently discarding it. Not yet consumed by any
	// caller as of this field's introduction -- ParseDepDict is the only
	// writer so far.
	SkillSubset []string
}

var virtualFileExtensions = []string{
	".prompt.md", ".instructions.md", ".agent.md",
}

var (
	ownerCharRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	repoCharRe  = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	// identity.py:30 and reference.py:1599-1614/1703-1709 at b75a02b1:
	// non-ADO URL identities preserve safe percent-encoded octets.
	percentEncodedRepoCharRe = regexp.MustCompile(`^(?:[A-Za-z0-9._~-]|%[0-9A-Fa-f]{2})+$`)
	hostCharRe               = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
	segmentRe                = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	adoSegmentRe             = regexp.MustCompile(`^[A-Za-z0-9._\- ]+$`)
	portRangeMax             = 65535

	// fqdnRe is the Oracle's is_valid_fqdn regex, ported verbatim
	// (github_host.py:1099-1101): labels of alphanumerics/hyphens that
	// neither start nor end with a hyphen, joined by dots, with AT LEAST
	// ONE dot (a bare single-label host like "host" or "localhost" is
	// therefore never a valid FQDN). See isValidFQDN's doc comment for
	// exactly which ParseDepString branches gate on this.
	fqdnRe = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

	// scpLikeRe ports cache/url_normalize.py's SCP_LIKE_RE verbatim: an
	// SCP-shorthand SSH remote is ANY valid SSH user, not just "git" --
	// ticket 11 eval attempt 4's reproducer 1 (also applies to
	// _parse_ssh_protocol_url's ssh:// form, gated separately below).
	scpLikeRe = regexp.MustCompile(`^(?P<user>[a-zA-Z0-9_][a-zA-Z0-9_.+-]*)@(?P<host>[^:/]+):(?P<path>.+)$`)

	// sshUserRe ports github_host.py's _SSH_USER_RE verbatim.
	sshUserRe = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.+-]*$`)

	// shorthandPortRe matches _split_shorthand_host_port's own
	// `re.fullmatch(r"[0-9]{1,5}", raw_port)` port-shape check.
	shorthandPortRe = regexp.MustCompile(`^[0-9]{1,5}$`)

	// refVersionSuffixRe ports reference.py:52's _REF_VERSION_SUFFIX_RE.
	// It is used only to preserve the one version-shaped @ suffix exception
	// in the bare-shorthand alias guard (reference.py:487-518).
	refVersionSuffixRe = regexp.MustCompile(`^v?\d+(?:\.\d+)*(?:[-+][A-Za-z0-9][A-Za-z0-9._-]*)?$`)
)

// sshUserMaxLen ports github_host.py's _SSH_USER_MAX_LEN.
const sshUserMaxLen = 64

// validateSSHUser ports validate_ssh_user (github_host.py:411-439): first
// character alphanumeric or underscore (blocks SSH option-injection
// vectors like "-oProxyCommand=..."), remaining characters letters/digits/
// "."/"+"/"-"/"_", max 64 bytes. Deliberately does not echo the raw value
// in its error (matching the Oracle's own "do NOT echo" comment -- a
// hostile apm.yml could embed control/ANSI characters that survive log
// emission).
func validateSSHUser(user string) error {
	if user == "" {
		return fmt.Errorf("SSH user must be a non-empty string")
	}
	if len(user) > sshUserMaxLen {
		return fmt.Errorf("SSH user is too long (%d > %d chars)", len(user), sshUserMaxLen)
	}
	if !sshUserRe.MatchString(user) {
		return fmt.Errorf("invalid SSH user (length %d)", len(user))
	}
	return nil
}

// stripQuery discards a URL's "?query" component and everything after it,
// mirroring what urllib.parse.urlparse's structural split gives every URL
// form for free (reference.py's _parse_standard_url/_parse_ssh_protocol_url
// both parse via urlparse, so both get the query stripped from the path
// before it is ever split into owner/repo segments) -- ticket 11 eval
// attempt 4's reproducer 2: "https://x.io/owner/repo?x" must accept,
// treating "repo" (not "repo?x") as the repository segment. Deliberately
// NOT applied to parseShorthand (a bare "owner/repo?x" with no URL scheme
// stays rejected -- the eval's own attempt-3 regression case, confirming
// shorthand's rejection was never evidence of URL-form equivalence) or to
// parseSCPURL (_parse_ssh_url has no urlparse call and no query handling
// of its own).
func stripQuery(s string) string {
	if idx := strings.IndexByte(s, '?'); idx >= 0 {
		return s[:idx]
	}
	return s
}

// splitShorthandHostPort ports _split_shorthand_host_port
// (identity.py:35-46) exactly: splits an optional ":port" suffix off a
// shorthand dependency's leading path segment BEFORE that segment is
// checked for host-shape at all -- ticket 11 eval attempt 4's reproducer 2:
// "x.io:443/owner/repo" must split to host "x.io" (port 443, which the
// Oracle then normalizes away as the HTTPS default) before isValidFQDN ever
// sees it, not FQDN-validate the unsplit "x.io:443" and reject it. Runs
// UNCONDITIONALLY on parts[0] in parseShorthand, exactly like the Oracle's
// own `parts[0], port = _split_shorthand_host_port(parts[0])` at the top of
// _resolve_shorthand_to_parsed_url -- even a segment that will end up being
// treated as a plain "owner" (no dot, not host-qualified) still gets this
// port-shape check first, so a malformed "owner:notaport/repo" fails here,
// not silently later.
func splitShorthandHostPort(hostSegment string) (host string, port int, err error) {
	idx := strings.LastIndex(hostSegment, ":")
	if idx < 0 {
		return hostSegment, 0, nil
	}
	host = hostSegment[:idx]
	rawPort := hostSegment[idx+1:]
	if host == "" || !shorthandPortRe.MatchString(rawPort) {
		return "", 0, fmt.Errorf("invalid shorthand port %q; expected an integer from 1 to 65535", rawPort)
	}
	p, convErr := strconv.Atoi(rawPort)
	if convErr != nil || p < 1 || p > 65535 {
		return "", 0, fmt.Errorf("invalid shorthand port %q; expected an integer from 1 to 65535", rawPort)
	}
	if p == 443 { // identity.py:46: only the HTTPS default is stripped, regardless of the eventual scheme
		return host, 0, nil
	}
	return host, p, nil
}

// isValidFQDN ports the Oracle's is_valid_fqdn (github_host.py:1074-1102).
//
// Ticket 11 attempt 4: probed directly against the pinned Oracle to find
// exactly where this gate applies, since is_supported_git_host (which
// wraps is_valid_fqdn) is NOT applied uniformly to every host-bearing
// dependency-string form:
//   - HTTPS/HTTP URL form (_validate_url_repo_path, reference.py:1492-1493):
//     gated -- `https://x/owner/repo` raises "Invalid Git host: 'x'.",
//     `https://x.io/owner/repo` succeeds with host "x.io". Ported into
//     parseHTTPURL.
//   - Shorthand's host-qualified form, e.g. "host.tld/owner/repo"
//     (_detect_virtual_package, reference.py:1125-1141): also gated --
//     `-x.io/owner/repo` and `x-.io/owner/repo` (hyphen-boundary labels)
//     and `x..io/owner/repo` (empty label) all raise "Invalid Git host".
//     Ported into parseShorthand.
//   - ssh:// protocol URLs and SCP shorthand (git@host:owner/repo)
//     (_parse_ssh_protocol_url, _parse_ssh_url): NOT gated at all -- probed
//     directly, `ssh://git@host:7999/owner/repo.git` and
//     `git@host:owner/repo.git` both succeed with the bare, non-FQDN host
//     "host" kept verbatim. Neither function calls is_supported_git_host,
//     and parse()'s own post-parse validation (_validate_final_repo_fields)
//     only checks repo_url segment characters, never host FQDN-ness.
//     parseSSHURL/parseSCPURL are therefore deliberately left ungated here
//     (hostCharRe's existing looser character-class check is all the
//     Oracle itself enforces for these two forms).
func isValidFQDN(host string) bool {
	if host == "" {
		return false
	}
	return fqdnRe.MatchString(host)
}

// ParseDepString parses an apm.yml dependency string (or a marketplace
// plugin's github/url/git-subdir/gitlab source coordinate, via
// marketplace.isValidRemoteCoordinate) into a DependencyReference: local
// paths, "owner/repo" shorthand (with an optional "#ref", host-qualified
// prefix, or virtual-path suffix), and https/http/ssh:// URLs and SCP
// (user@host:path) shorthand.
//
// The parser follows the pinned Oracle's DependencyReference.parse
// (reference.py), including its host-specific GitLab, Azure DevOps, and
// Artifactory path boundaries. The checked-in conformance table is generated
// directly from that Oracle (tools/depref_conformance_gen.py) and asserted by
// TestParseDepString_OracleConformance; any intentional security hardening
// remains explicitly documented as a known gap there.
func ParseDepString(s string) (*DependencyReference, error) {
	if s == "" {
		return nil, fmt.Errorf("empty dependency string")
	}

	// The Oracle no longer performs a whole-string urllib.parse.unquote here
	// (reference.py:1774-1779 at b75a02b1). Percent-bearing repository paths
	// are decoded only by the strict URL-path helper below; shorthand keeps
	// its encoded presentation and validates its segments before character
	// matching. This distinction is the security fix from upstream commit
	// 645a5a53.

	// reference.py:1777-1778: control characters are rejected before the
	// shape-specific parser sees them.
	for _, r := range s {
		if r < 32 {
			return nil, fmt.Errorf("dependency string contains invalid control characters")
		}
	}

	// An OS-absolute filesystem path (POSIX "/...", Windows "C:\..."/"C:/...",
	// or a "\\host\share" UNC path) is user-intended, not a path-traversal
	// attempt -- accept it as a local dependency outright, WITHOUT running
	// containsEscape below (that guard only makes sense for a path meant to
	// stay relative to -- and inside -- the project root). This also lets an
	// absolute path resolved by mkt-025's local-marketplace fast path
	// round-trip back through apm.yml when install.go can't relativize it
	// into the project tree.
	if IsAbsoluteLocalPath(s) {
		return &DependencyReference{IsLocal: true, LocalPath: s, Source: "local"}, nil
	}

	if isLocalPath(s) {
		if containsEscape(s) {
			return nil, fmt.Errorf("dependency path %q escapes project root", s)
		}
		return &DependencyReference{IsLocal: true, LocalPath: s, Source: "local"}, nil
	}

	// reference.py:1775 calls _reject_shorthand_alias after local-path
	// detection and before URL parsing. Bare shorthand retired @alias syntax
	// must produce the Oracle's migration text, while explicit URL/SCP
	// parsers retain their own userinfo/path-alias handling.
	if err := rejectBareShorthandAlias(s); err != nil {
		return nil, err
	}

	// reference.py:1626,1635 (_parse_standard_url): `repo_url_lower =
	// repo_url.lower()` then `repo_url_lower.startswith(("https://",
	// "http://"))` -- the scheme match is CASE-INSENSITIVE. Ticket 11 eval
	// attempt 6's reproducer 1: `HTTPS://x.io/owner/repo` is accepted by
	// the Oracle (scheme normalizes to lowercase "https" in the result);
	// apm-go previously matched only the literal lowercase prefix.
	if hasFoldPrefix(s, "https://") || hasFoldPrefix(s, "http://") {
		return parseHTTPURL(s)
	}
	// reference.py:541: `if not url.startswith("ssh://"): return None` -- no
	// userinfo requirement in the prefix check itself; the user (if any) is
	// extracted from parsed.username afterward, defaulting to "git". Ticket
	// 11 eval attempt 4's reproducer 1: apm-go previously required the
	// literal "ssh://git@" prefix, rejecting an arbitrary SSH user
	// ("ssh://alice@host/owner/repo").
	//
	// Deliberately CASE-SENSITIVE, unlike the https/http check above --
	// probed directly for ticket 11 eval attempt 6's scheme-case fix:
	// "SSH://git@host.io/owner/repo" is REJECTED by the Oracle (it falls
	// through to a shorthand-port parse error, since `url.startswith`
	// above has no `.lower()`, unlike `_parse_standard_url`'s
	// `repo_url_lower`). Do not "fix" this to be case-insensitive too.
	if strings.HasPrefix(s, "ssh://") {
		return parseSSHURL(s)
	}
	// SCP_LIKE_RE (cache/url_normalize.py), matched the same way
	// reference.py:1246 does -- ANY valid SSH user, not just "git@" (the
	// SCP half of the same reproducer 1 gap).
	if scpLikeRe.MatchString(s) {
		return parseSCPURL(s)
	}

	return parseShorthand(s)
}

// rejectBareShorthandAlias ports reference.py:487-518 exactly. The version
// suffix exception intentionally uses the Oracle's regex verbatim: notably,
// v1.0.1-rc.1+build contains two separators and is rejected by that regex,
// despite the broader boundary wording in the verifier brief.
func rejectBareShorthandAlias(s string) error {
	stripped := strings.TrimSpace(s)
	if !strings.Contains(stripped, "@") && !strings.Contains(strings.ToLower(stripped), "%40") {
		return nil
	}
	lower := strings.ToLower(stripped)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "ssh://") {
		return nil
	}
	if scpLikeRe.MatchString(stripped) {
		return nil
	}
	shorthandPart, refPart := stripped, ""
	if idx := strings.IndexByte(stripped, '#'); idx >= 0 {
		shorthandPart, refPart = stripped[:idx], stripped[idx+1:]
	}
	hasEncodedAlias := false
	if strings.Contains(strings.ToLower(shorthandPart), "%40") {
		_, decoded, err := parseURLPathSegments(shorthandPart, "repository path")
		if err != nil {
			return err
		}
		for _, segment := range decoded {
			if strings.Contains(segment, "@") {
				hasEncodedAlias = true
				break
			}
		}
	}
	if !strings.Contains(stripped, "@") && !hasEncodedAlias {
		return nil
	}
	if !strings.Contains(shorthandPart, "@") && !hasEncodedAlias {
		if idx := strings.LastIndexByte(refPart, '@'); idx >= 0 && refVersionSuffixRe.MatchString(refPart[idx+1:]) {
			return nil
		}
	}

	runes := []rune(stripped)
	for i, r := range runes {
		if r < 32 || r > 126 {
			runes[i] = '?'
		}
	}
	if len(runes) > 160 {
		runes = append(runes[:157], '.', '.', '.')
	}
	preview := string(runes)
	return fmt.Errorf(
		"Shorthand '@alias' is not supported in '%s'. Use object form with 'git:', optional 'path:', and 'alias:' fields to install a dependency under a custom directory name. See: https://microsoft.github.io/apm/consumer/manage-dependencies/#reference-formats",
		preview,
	)
}

// parseURLPathSegments ports path_security.py:32-120, introduced by Oracle
// commit 645a5a53 and used by reference.py:1505-1510 at b75a02b1. It keeps
// raw URL segments for non-ADO identity/presentation, while returning strict
// decoded segments for ADO coordinates and virtual-path decisions. The
// helper intentionally rejects empty segments, malformed escapes, invalid
// UTF-8, residual multi-encoding, decoded separators, and traversal names.
func parseURLPathSegments(rawPath, context string) ([]string, []string, error) {
	path := strings.TrimPrefix(rawPath, "/")
	if path == "" {
		return nil, nil, fmt.Errorf("Invalid %s: path segments must not be empty", context)
	}
	rawSegments := strings.Split(path, "/")
	decodedSegments := make([]string, 0, len(rawSegments))
	for _, rawSegment := range rawSegments {
		if rawSegment == "" {
			return nil, nil, fmt.Errorf("Invalid %s: path segments must not be empty", context)
		}
		if strings.ContainsRune(rawSegment, '\\') {
			return nil, nil, fmt.Errorf("Invalid %s: path segments must not contain path separators", context)
		}
		for i := 0; i < len(rawSegment); {
			c := rawSegment[i]
			if c < 0x21 || c > 0x7e {
				return nil, nil, fmt.Errorf("Invalid %s: path segments must use percent-encoded UTF-8 bytes", context)
			}
			if c == '%' {
				if i+2 >= len(rawSegment) {
					return nil, nil, fmt.Errorf("Invalid %s: malformed percent-encoding", context)
				}
				if _, ok := hexNibble(rawSegment[i+1]); !ok {
					return nil, nil, fmt.Errorf("Invalid %s: malformed percent-encoding", context)
				}
				if _, ok := hexNibble(rawSegment[i+2]); !ok {
					return nil, nil, fmt.Errorf("Invalid %s: malformed percent-encoding", context)
				}
				i += 3
				continue
			}
			i++
		}
		decodedBytes := lenientUnquoteBytes(rawSegment)
		if !utf8.Valid(decodedBytes) {
			return nil, nil, fmt.Errorf("Invalid %s: percent-encoding must be valid UTF-8", context)
		}
		decoded := string(decodedBytes)
		for _, r := range decoded {
			if r < 0x20 || r == 0x7f {
				return nil, nil, fmt.Errorf("Invalid %s: percent-encoding must not decode to control characters", context)
			}
		}
		if strings.ContainsRune(decoded, '%') {
			return nil, nil, fmt.Errorf("Invalid %s: residual percent-encoding is not allowed", context)
		}
		if strings.ContainsAny(decoded, `/\\`) {
			return nil, nil, fmt.Errorf("Invalid %s: percent-encoding must not decode to a path separator", context)
		}
		if isDotSegment(decoded) {
			return nil, nil, fmt.Errorf("Invalid %s: segment '%s' is a traversal sequence", context, rawSegment)
		}
		decodedSegments = append(decodedSegments, decoded)
	}
	return rawSegments, decodedSegments, nil
}

// validateEncodedPathSegments ports path_security.py:123-173 for shorthand
// repository and virtual paths. Unlike parseURLPathSegments it permits empty
// segments (the Oracle's default reject_empty=False) and only rejects literal
// or up-to-eight-round percent-decoded traversal markers.
func validateEncodedPathSegments(path, context string) error {
	for _, segment := range strings.Split(strings.ReplaceAll(path, `\`, "/"), "/") {
		decoded := segment
		for i := 0; i < 8; i++ {
			next := lenientUnquote(decoded)
			if next == decoded {
				break
			}
			decoded = next
		}
		if isDotSegment(segment) || isDotSegment(decoded) {
			return fmt.Errorf("Invalid %s '%s': segment '%s' is a traversal sequence", context, path, segment)
		}
	}
	return nil
}

// lenientUnquote ports Python's urllib.parse.unquote for the bounded
// traversal check in path_security.py:157-168. Percent-decode is lenient,
// not strict: an invalid escape (a "%" not followed by two hex digits) is
// left completely unconsumed -- the literal "%" and whatever follows it
// pass through unchanged. The resulting bytes are UTF-8-decoded with
// errors='replace' semantics for parity with the Oracle's path guard.
func lenientUnquote(s string) string {
	return utf8ReplaceInvalid(lenientUnquoteBytes(s))
}

// lenientUnquoteBytes is unquote_to_bytes's core loop: a percent sign
// followed by two valid hex digits decodes to that byte; anything else
// (including a "%" at the very end of the string, or followed by
// non-hex characters) is copied through verbatim, exactly as CPython's
// bits = string.split(b'%') / _hextobyte lookup does.
func lenientUnquoteBytes(s string) []byte {
	src := []byte(s)
	out := make([]byte, 0, len(src))
	for i := 0; i < len(src); i++ {
		if src[i] == '%' && i+2 < len(src) {
			hi, ok1 := hexNibble(src[i+1])
			lo, ok2 := hexNibble(src[i+2])
			if ok1 && ok2 {
				out = append(out, hi<<4|lo)
				i += 2
				continue
			}
		}
		out = append(out, src[i])
	}
	return out
}

func hexNibble(b byte) (byte, bool) {
	switch {
	case b >= '0' && b <= '9':
		return b - '0', true
	case b >= 'a' && b <= 'f':
		return b - 'a' + 10, true
	case b >= 'A' && b <= 'F':
		return b - 'A' + 10, true
	}
	return 0, false
}

// utf8ReplaceInvalid walks b as UTF-8, replacing each invalid byte (or
// invalid lead-plus-partial-continuation run) with U+FFFD one rune at a
// time via utf8.DecodeRune -- the same "maximal subpart" algorithm
// CPython's UTF-8 decoder uses for errors='replace'.
// utf8ReplaceInvalid decodes b the way Python's bytes.decode("utf-8",
// errors="replace") does: ONE U+FFFD per MAXIMAL ill-formed subsequence
// (Unicode "maximal subpart" / WHATWG decode), not one per byte. A
// truncated but otherwise well-formed prefix like E0 A0 consumes both
// bytes for a single replacement, while each stray continuation byte gets
// its own -- eval-ticket-11 Attempt 6 reproducer 4: Go's utf8.DecodeRune
// replaces per byte, splitting "%e0%a0" into two U+FFFDs where the Oracle's
// unquote(..., errors='replace') yields exactly one.
func utf8ReplaceInvalid(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r != utf8.RuneError || size != 1 {
			sb.WriteRune(r)
			i += size
			continue
		}
		sb.WriteRune(utf8.RuneError)
		i += maximalSubpartLen(b[i:])
	}
	return sb.String()
}

// maximalSubpartLen returns the byte length (>= 1) of the maximal subpart
// of the ill-formed subsequence at the start of b: the longest prefix that
// is also a prefix of SOME well-formed UTF-8 sequence (Unicode 15 §3.9
// U+FFFD substitution of maximal subparts; CPython's UTF-8 'replace'
// handler implements the same rule). Only the FIRST continuation byte has
// a lead-byte-dependent constrained range; later ones are plain 80-BF.
func maximalSubpartLen(b []byte) int {
	var need int
	var lo, hi byte
	switch c := b[0]; {
	case c >= 0xc2 && c <= 0xdf:
		need, lo, hi = 1, 0x80, 0xbf
	case c == 0xe0:
		need, lo, hi = 2, 0xa0, 0xbf
	case c >= 0xe1 && c <= 0xec:
		need, lo, hi = 2, 0x80, 0xbf
	case c == 0xed:
		need, lo, hi = 2, 0x80, 0x9f
	case c >= 0xee && c <= 0xef:
		need, lo, hi = 2, 0x80, 0xbf
	case c == 0xf0:
		need, lo, hi = 3, 0x90, 0xbf
	case c >= 0xf1 && c <= 0xf3:
		need, lo, hi = 3, 0x80, 0xbf
	case c == 0xf4:
		need, lo, hi = 3, 0x80, 0x8f
	default:
		// Stray continuation byte, overlong lead C0/C1, or F5-FF: the
		// maximal subpart is the single byte itself.
		return 1
	}
	n := 1
	for ; n <= need && n < len(b); n++ {
		if b[n] < lo || b[n] > hi {
			break
		}
		lo, hi = 0x80, 0xbf
	}
	return n
}

// hasFoldPrefix reports whether s starts with prefix, ignoring ASCII case
// -- ParseDepString's https/http dispatch (reference.py:1626's
// `repo_url_lower.startswith(...)`).
func hasFoldPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix)
}

func parseHTTPURL(s string) (*DependencyReference, error) {
	scheme := "https"
	prefixLen := len("https://")
	if hasFoldPrefix(s, "http://") {
		scheme = "http"
		prefixLen = len("http://")
	}
	rest := s[prefixLen:]

	ref, rest := splitRef(rest)
	// urlparse structurally separates the query from the path (reference.py's
	// _parse_standard_url parses via urllib.parse.urlparse) -- ticket 11
	// eval attempt 4's reproducer 2: "https://x.io/owner/repo?x" must treat
	// "repo" (not "repo?x") as the repository segment.
	rest = stripQuery(rest)

	// urlsplit semantics (eval-ticket-11 Attempt 6): the netloc ends at the
	// FIRST "/", and everything before it -- userinfo, host, port -- is
	// parsed by netlocHostPort the way parsed_url.hostname/.port would be.
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return nil, fmt.Errorf("dependency %q: url-form requires host/owner/repo", s)
	}
	netloc, rawPath := rest[:slash], rest[slash+1:]
	host, port, err := netlocHostPort(netloc)
	if err != nil {
		return nil, fmt.Errorf("dependency %q: %w", s, err)
	}
	// reference.py:1492-1493 (_validate_url_repo_path): the HTTPS/HTTP URL
	// form gates its host on is_supported_git_host, which falls through to
	// is_valid_fqdn for any host outside the GitHub/Azure-DevOps allowlists
	// -- see isValidFQDN's doc comment for the direct probe evidence.
	if !isValidFQDN(host) {
		return nil, fmt.Errorf("dependency %q: invalid Git host %q: not a valid FQDN", s, host)
	}

	// _validate_url_repo_path (reference.py:1504-1514) now routes the raw
	// parsed URL path through path_security.parse_url_path_segments. It strips
	// only the leading URL slash, rejects every empty segment (including a
	// leading double slash and a trailing slash), decodes exactly once, and
	// retains the raw presentation for non-ADO repository identities. The
	// helper's error text is intentionally surfaced without a dependency
	// prefix, matching the Oracle's ValueError(str(PathTraversalError)) wrap.
	presentationParts, parts, err := parseURLPathSegments("/"+rawPath, "repository URL path")
	if err != nil {
		return nil, err
	}
	if strings.HasSuffix(parts[len(parts)-1], ".git") {
		parts[len(parts)-1] = strings.TrimSuffix(parts[len(parts)-1], ".git")
		if strings.HasSuffix(presentationParts[len(presentationParts)-1], ".git") {
			presentationParts[len(presentationParts)-1] = strings.TrimSuffix(presentationParts[len(presentationParts)-1], ".git")
		}
	}
	path := strings.Join(parts, "/")

	d := &DependencyReference{
		Host:   host,
		Port:   port,
		Scheme: scheme,
		Source: "git",
	}
	if ref != "" {
		d.Reference = ref
	}

	// reference.py:1503-1559 removes an ADO `_git` marker, uses the
	// host-specific repository width, injects the legacy Visual Studio org,
	// and treats any remaining segments as a virtual package path.
	if isAzureDevOpsHost(host) {
		parts = removeGitMarker(parts)
		presentationParts = removeGitMarker(presentationParts)
		baseLen := 3
		if isVisualStudioLegacyHost(host) {
			baseLen = 2
		}
		if len(parts) < baseLen {
			return nil, fmt.Errorf("Invalid Azure DevOps repository path: expected 'org/project/repo', got '%s'", path)
		}
		repoParts := append([]string(nil), parts[:baseLen]...)
		if isVisualStudioLegacyHost(host) {
			repoParts = append([]string{strings.Split(host, ".")[0]}, repoParts...)
		}
		d.RepoURL = strings.Join(repoParts, "/")
		if len(parts) > baseLen {
			vp := strings.Join(parts[baseLen:], "/")
			if err := validateVirtualPath(vp); err != nil {
				return nil, err
			}
			d.VirtualPath = vp
			d.VirtualType = classifyVirtualPath(vp)
		}
	} else if prefix, ok := artifactoryPathPrefix(parts); ok {
		// reference.py:1565-1572 strips the Artifactory route from the
		// repository identity. Unlike ADO, the HTTPS parser keeps every
		// remaining segment in repo_url (reference.py:1573-1590); the prefix
		// is recovered separately at reference.py:1824-1842.
		d.ArtifactoryPrefix = prefix
		presentationParts = presentationParts[2:]
		d.RepoURL = strings.Join(presentationParts, "/")
	} else {
		// reference.py:1560-1589 treats non-ADO URL paths as repository
		// coordinates, including nested GitLab groups; URL virtual packages
		// are handled only by the ADO branch above.
		d.RepoURL = strings.Join(presentationParts, "/")
	}
	if err := setRepositoryFields(d, d.RepoURL, isAzureDevOpsHost(host), true); err != nil {
		return nil, err
	}
	return d, nil
}

func parseSSHURL(s string) (*DependencyReference, error) {
	rest := strings.TrimPrefix(s, "ssh://")
	// The Oracle checks the raw SSH userinfo for percent escapes before
	// interpreting it (reference.py:544-555 at b75a02b1). Keep the same
	// rejection; the commit removed the old whole-string decode, so a host
	// escape such as %20 remains encoded in the parsed hostname.
	if at := strings.IndexByte(rest, '@'); at >= 0 && strings.Contains(rest[:at], "%") {
		return nil, fmt.Errorf("Percent-encoded characters are not allowed in SSH userinfo. Use the literal username (e.g. 'ssh://myuser@host/...').")
	}

	ref, rest := splitRef(rest)
	rest = stripQuery(rest) // urlparse separates query from path -- reproducer 2
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return nil, fmt.Errorf("dependency %q: ssh url-form requires host/owner/repo", s)
	}
	netloc, rawPath := rest[:slash], rest[slash+1:]

	// _parse_ssh_protocol_url (reference.py:558-575) reads parsed.username
	// from ONE urlsplit of the netloc -- eval-ticket-11 Attempt 7: userinfo
	// is everything before the LAST "@" (so "one@two@host" has username
	// "one@two", which validate_ssh_user then rejects), the username is the
	// userinfo up to the FIRST ":" (a ":password" is split off and ignored,
	// so "alice:pw@host" validates just "alice"), and an EMPTY username
	// falls back to the default "git" with no validation at all
	// (`validate_ssh_user(raw_user) if raw_user else "git"` --
	// "ssh://@host/..." parses). The old code took the FIRST "@" as the
	// user boundary and validated the raw userinfo whole, diverging on all
	// three shapes.
	user := "git"
	if at := strings.LastIndexByte(netloc, '@'); at >= 0 {
		userinfo := netloc[:at]
		if colon := strings.IndexByte(userinfo, ':'); colon >= 0 {
			userinfo = userinfo[:colon]
		}
		if userinfo != "" {
			if err := validateSSHUser(userinfo); err != nil {
				return nil, fmt.Errorf("dependency %q: %w", s, err)
			}
			user = userinfo
		}
	}

	host, port, err := netlocHostPortSSH(netloc)
	if err != nil {
		return nil, fmt.Errorf("dependency %q: %w", s, err)
	}

	// _parse_ssh_protocol_url (reference.py:562-599): the path is
	// lstrip("/")-ed -- LEFT only, so a leading "//" collapses (conformance
	// row ssh-leading-double-slash) -- then a terminal ".git" comes off,
	// then validate_path_segments(..., reject_empty=True) rejects ANY empty
	// segment, so an internal "//" or a trailing "/" both fail
	// (ssh-internal-double-slash / ssh-trailing-slash). Deliberately
	// asymmetric with the https form's strip("/") + no reject_empty. No
	// per-part unquote here either -- only _validate_url_repo_path (the
	// https path) double-decodes.
	path, alias := splitPathAlias(rawPath)
	path = strings.TrimLeft(path, "/")
	path = strings.TrimSuffix(path, ".git")
	segs := strings.Split(path, "/")
	if len(segs) < 2 {
		return nil, fmt.Errorf("dependency %q: ssh url-form requires host/owner/repo", s)
	}
	for _, sg := range segs {
		if sg == "" {
			return nil, fmt.Errorf("dependency %q: empty segment in ssh repository path", s)
		}
	}
	owner := segs[0]
	repo := segs[1]

	if !ownerCharRe.MatchString(owner) || isDotSegment(owner) {
		return nil, fmt.Errorf("dependency %q: invalid owner %q", s, owner)
	}
	if !repoCharRe.MatchString(repo) || isDotSegment(repo) {
		return nil, fmt.Errorf("dependency %q: invalid repo %q", s, repo)
	}

	d := &DependencyReference{
		Host:    host,
		Port:    port,
		Owner:   owner,
		Repo:    repo,
		RepoURL: owner + "/" + repo,
		Scheme:  "ssh",
		Source:  "git",
	}
	if user != "git" {
		d.SSHUser = user
	}
	if ref != "" {
		d.Reference = ref
	}
	if alias != "" {
		d.Alias = alias
	}
	if len(segs) > 2 {
		// ".git" already stripped from the raw path end above, pre-split.
		vp := strings.Join(segs[2:], "/")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func parseSCPURL(s string) (*DependencyReference, error) {
	// scpLikeRe (SCP_LIKE_RE) captures ANY valid SSH user, not just "git" --
	// ticket 11 eval attempt 4's reproducer 1. The caller (ParseDepString)
	// already confirmed a match before dispatching here.
	m := scpLikeRe.FindStringSubmatch(s)
	if m == nil {
		return nil, fmt.Errorf("dependency %q: SCP form requires user@host:path", s)
	}
	user, host, path := m[1], m[2], m[3]
	if err := validateSSHUser(user); err != nil {
		return nil, fmt.Errorf("dependency %q: %w", s, err)
	}
	if !hostCharRe.MatchString(host) {
		return nil, fmt.Errorf("dependency %q: invalid host %q", s, host)
	}
	ref, path := splitRef(path)
	path, alias := splitPathAlias(path)
	repoURL := strings.TrimSpace(strings.TrimSuffix(path, ".git"))
	parts := strings.Split(repoURL, "/")
	if first := parts[0]; allDigits(first) {
		portCandidate, convErr := strconv.Atoi(first)
		if convErr == nil && portCandidate >= 1 && portCandidate <= 65535 {
			remainingPath := strings.Join(parts[1:], "/")
			if remainingPath != "" {
				gitSuffix := ""
				if strings.HasSuffix(path, ".git") {
					gitSuffix = ".git"
				}
				refSuffix := ""
				if ref != "" {
					refSuffix = "#" + ref
				}
				aliasSuffix := ""
				if alias != "" {
					aliasSuffix = "@" + alias
				}
				suggested := fmt.Sprintf("ssh://%s@%s:%d/%s%s%s%s", user, host, portCandidate, remainingPath, gitSuffix, refSuffix, aliasSuffix)
				return nil, fmt.Errorf("It looks like '%s' in '%s@%s:%s' is a port number, but SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form instead:\n  %s", first, user, host, repoURL, suggested)
			}
			return nil, fmt.Errorf("It looks like '%s' in '%s@%s:%s' is a port number, but no repository path follows it. SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form: ssh://%s@%s:%d/<owner>/<repo>.git", first, user, host, first, user, host, portCandidate)
		}
	}
	if len(parts) < 2 {
		return nil, fmt.Errorf("dependency %q: SCP form requires user@host:owner/repo", s)
	}
	for _, part := range parts {
		if !repoCharRe.MatchString(part) || isDotSegment(part) {
			return nil, fmt.Errorf("dependency %q: invalid repository path component %q", s, part)
		}
	}
	owner := parts[0]
	repo := parts[1]

	if !ownerCharRe.MatchString(owner) || isDotSegment(owner) {
		return nil, fmt.Errorf("dependency %q: invalid owner %q", s, owner)
	}
	if !repoCharRe.MatchString(repo) || isDotSegment(repo) {
		return nil, fmt.Errorf("dependency %q: invalid repo %q", s, repo)
	}

	d := &DependencyReference{
		Host:    host,
		Owner:   owner,
		Repo:    repo,
		RepoURL: repoURL,
		Scheme:  "git",
		Source:  "git",
	}
	if user != "git" {
		d.SSHUser = user
	}
	if ref != "" {
		d.Reference = ref
	}
	if alias != "" {
		d.Alias = alias
	}
	return d, nil
}

func parseShorthand(s string) (*DependencyReference, error) {
	ref, rest := splitRef(s)
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return nil, fmt.Errorf("dependency %q does not match any valid form (url, shorthand, or local-path)", s)
	}

	// identity.py:35-46 (_split_shorthand_host_port): runs UNCONDITIONALLY
	// on the leading segment, before it is even checked for host-shape --
	// ticket 11 eval attempt 4's reproducer 2, "x.io:443/owner/repo" must
	// split to host "x.io" (port 443, normalized away as the HTTPS default)
	// before isValidFQDN ever sees it. See splitShorthandHostPort's doc
	// comment.
	first, port, err := splitShorthandHostPort(parts[0])
	if err != nil {
		return nil, fmt.Errorf("dependency %q: %w", s, err)
	}
	normalizedParts := append([]string(nil), parts...)
	normalizedParts[0] = first

	var host string
	var repoParts, vpParts []string

	if len(parts) >= 3 && strings.Contains(first, ".") {
		host = first
		// reference.py:1125-1141 (_detect_virtual_package): the SAME
		// `"." in first_segment` heuristic used above to decide "does this
		// look like a host at all" gates the candidate on
		// is_supported_git_host once triggered -- a dotted-but-invalid host
		// (a leading/trailing hyphen label, an empty label from "..") is a
		// hard "Invalid Git host" error here, not a fallthrough to treating
		// it as a plain owner. Probed directly: "-x.io/owner/repo",
		// "x-.io/owner/repo", and "x..io/owner/repo" all raise. See
		// isValidFQDN's doc comment.
		if !isValidFQDN(host) {
			return nil, fmt.Errorf("dependency %q: invalid Git host %q: not a valid FQDN", s, host)
		}
		pathParts := append([]string(nil), parts[1:]...)
		if isAzureDevOpsHost(host) {
			pathParts = removeGitMarker(pathParts)
			baseLen := 3
			if isVisualStudioLegacyHost(host) {
				baseLen = 2
			}
			if len(pathParts) < baseLen {
				return nil, fmt.Errorf("Invalid Azure DevOps repository format: %s. Expected 'org/project/repo'", rest)
			}
			repoParts = append([]string(nil), pathParts[:baseLen]...)
			if isVisualStudioLegacyHost(host) {
				repoParts = append([]string{strings.Split(host, ".")[0]}, repoParts...)
			}
			if len(pathParts) > baseLen {
				vpParts = pathParts[baseLen:]
			}
		} else if _, ok := artifactoryPathPrefix(pathParts); ok {
			// github_host.py:911-917,973-990 recognizes
			// artifactory/{repo-key}/{owner}/{repo} when the first path
			// segment is case-insensitively "artifactory" and at least four
			// path segments follow the host.
			repoParts = pathParts[2:4]
			vpParts = pathParts[4:]
		} else if isGitLabHost(host) {
			// reference.py:1047-1059 keeps extensionless GitLab paths as
			// the repository, but recognizes virtual-file tails using known
			// layout roots or the 3/4/5-segment fallback boundary.
			baseLen := gitLabRepoSegmentCount(pathParts)
			repoParts = pathParts[:baseLen]
			if baseLen < len(pathParts) {
				vpParts = pathParts[baseLen:]
			}
		} else if isGitHubHost(host) {
			repoParts = pathParts[:2]
			vpParts = pathParts[2:]
		} else if virtualFileTail(pathParts) {
			repoParts = pathParts[:2]
			vpParts = pathParts[2:]
		} else {
			// reference.py:1182-1185 and 1403-1421 keep all
			// extensionless generic-host segments in repo_url.
			repoParts = pathParts
		}
	} else {
		host = "github.com"
		repoParts = normalizedParts[:2]
		if len(normalizedParts) > 2 {
			vpParts = normalizedParts[2:]
		}
	}

	if len(repoParts) == 0 {
		return nil, fmt.Errorf("dependency %q does not match any valid form (url, shorthand, or local-path)", s)
	}
	repoParts = append([]string(nil), repoParts...)
	repoParts[len(repoParts)-1] = strings.TrimSuffix(repoParts[len(repoParts)-1], ".git")
	repoURL := strings.Join(repoParts, "/")
	if err := validateRepositoryPath(repoURL, isAzureDevOpsHost(host), false); err != nil {
		// reference.py:1468-1472 lets the repository-path validation error
		// escape directly; preserve the Oracle's exact diagnostic without a
		// dependency-string wrapper.
		return nil, err
	}
	owner, repo := repositoryFields(repoURL)

	d := &DependencyReference{
		Host:    host,
		Owner:   owner,
		Repo:    repo,
		RepoURL: repoURL,
		Source:  "git",
	}
	if prefix, ok := artifactoryPathPrefix(parts[1:]); ok {
		d.ArtifactoryPrefix = prefix
	}
	d.Port = port
	if ref != "" {
		d.Reference = ref
	}
	if len(vpParts) > 0 {
		vp := strings.Join(vpParts, "/")
		if err := validateVirtualPath(vp); err != nil {
			return nil, err
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func ParseDepDict(entry *yaml.Node, idx int) (*DependencyReference, error) {
	kv := make(map[string]string)
	keys := make(map[string]bool)
	var skillsNode *yaml.Node
	for i := 0; i < len(entry.Content)-1; i += 2 {
		k := entry.Content[i].Value
		keys[k] = true
		if k == "skills" {
			skillsNode = entry.Content[i+1]
		}
		if entry.Content[i+1].Kind == yaml.ScalarNode {
			kv[k] = entry.Content[i+1].Value
		}
	}

	// mkt-033: the marketplace branch MUST be checked before every other
	// branch below, including "name" -- a marketplace dict entry
	// ({name, marketplace, version}) always carries a "name" key, and the
	// existing `keys["name"]` branch a few lines down would otherwise
	// silently swallow it as a plain git-literal RepoURL (see depref.go's
	// git-literal "name" branch); that shadowing is exactly what the
	// mkt-033 branch-order regression test below locks down.
	if keys["marketplace"] {
		if keys["git"] || keys["path"] || keys["registry"] || keys["id"] {
			return nil, fmt.Errorf("dependency entry %d: Ambiguous dependency - 'marketplace' cannot be combined with 'git', 'path', 'registry', or 'id'", idx)
		}
		for k := range keys {
			switch k {
			case "name", "marketplace", "version":
				// allowed
			default:
				return nil, fmt.Errorf("dependency entry %d: unknown key %q for a marketplace dependency (allowed: name, marketplace, version)", idx, k)
			}
		}

		// name is required, checked before the regex validation below, with
		// its own dedicated error message (mirrors reference.py:763-766).
		name := strings.TrimSpace(kv["name"])
		if name == "" {
			return nil, fmt.Errorf("dependency entry %d: Marketplace dependency must have a non-empty 'name' field", idx)
		}
		mkt := strings.TrimSpace(kv["marketplace"])

		// name/marketplace are only stripped, never lowercased -- case
		// insensitivity happens later at plugin-lookup time, not at parse
		// time (mkt-033: "大小寫保留").
		if !segmentRe.MatchString(name) {
			return nil, fmt.Errorf("dependency entry %d: invalid marketplace plugin name %q", idx, name)
		}
		if !segmentRe.MatchString(mkt) {
			return nil, fmt.Errorf("dependency entry %d: invalid marketplace name %q", idx, mkt)
		}

		// version is optional; when present it must be non-empty, but parse
		// time performs no format/semver validation at all (range legality
		// is deferred to resolve time, mirrors reference.py:781-785).
		version := kv["version"]
		if keys["version"] && strings.TrimSpace(version) == "" {
			return nil, fmt.Errorf("dependency entry %d: marketplace dependency 'version' must be a non-empty string when present", idx)
		}

		return &DependencyReference{
			RepoURL:                "_marketplace/" + mkt + "/" + name,
			Source:                 "marketplace",
			MarketplaceName:        mkt,
			MarketplacePluginName:  name,
			MarketplaceVersionSpec: version,
		}, nil
	}

	if keys["id"] && keys["git"] {
		return nil, fmt.Errorf("dependency entry %d has both 'id' and 'git' keys", idx)
	}

	if keys["path"] && !keys["git"] && !keys["id"] && !keys["name"] {
		if keys["skills"] {
			return nil, fmt.Errorf("dependency entry %d: 'skills' is only supported for git dependencies", idx)
		}
		p := kv["path"]
		if containsEscape(p) {
			return nil, fmt.Errorf("dependency path %q escapes project root", p)
		}
		return &DependencyReference{
			IsLocal:   true,
			LocalPath: p,
			Alias:     kv["alias"],
			Source:    "local",
		}, nil
	}

	if keys["git"] {
		gitVal := kv["git"]
		if gitVal == "parent" {
			if !keys["path"] {
				return nil, fmt.Errorf("dependency entry %d: git: parent requires a 'path' field", idx)
			}
			if keys["type"] {
				return nil, fmt.Errorf("dependency entry %d: 'type' is not allowed with git: parent", idx)
			}
			if keys["skills"] {
				return nil, fmt.Errorf("dependency entry %d: 'skills' is not allowed with git: parent", idx)
			}
			return &DependencyReference{
				IsParent:    true,
				VirtualPath: kv["path"],
				VirtualType: classifyVirtualPath(kv["path"]),
				Alias:       kv["alias"],
			}, nil
		}
		d, err := ParseDepString(gitVal)
		if err != nil {
			return nil, fmt.Errorf("dependency entry %d: %w", idx, err)
		}
		// git: key forces source=git even for local filesystem paths
		if d.IsLocal {
			d.IsLocal = false
			d.RepoURL = d.LocalPath
			d.LocalPath = ""
			d.Source = "git"
		}
		if kv["ref"] != "" {
			d.Reference = kv["ref"]
		}
		if kv["alias"] != "" {
			d.Alias = kv["alias"]
		}
		if kv["path"] != "" {
			d.VirtualPath = kv["path"]
			d.VirtualType = classifyVirtualPath(kv["path"])
		}
		if keys["skills"] {
			subset, err := parseSkillsField(skillsNode, idx)
			if err != nil {
				return nil, err
			}
			d.SkillSubset = subset
		}
		return d, nil
	}

	if keys["id"] {
		if keys["skills"] {
			return nil, fmt.Errorf("dependency entry %d: 'skills' is only supported for git dependencies", idx)
		}
		// Registry object form uses `version:` (docs); accept `ref:` as an alias.
		reference := kv["version"]
		if reference == "" {
			reference = kv["ref"]
		}
		return &DependencyReference{
			RepoURL:      kv["id"],
			Reference:    reference,
			RegistryName: kv["registry"],
			Alias:        kv["alias"],
			Source:       "registry",
		}, nil
	}

	if keys["name"] {
		// Merge resolution (#4 → #5): keep #5's skills guard AND #4's security
		// validation. The skills subset is only wired for the explicit `git:`
		// dict form, so a bare {name: ...} with skills is rejected first.
		if keys["skills"] {
			return nil, fmt.Errorf("dependency entry %d: 'skills' is only supported for git dependencies", idx)
		}
		// A bare {name: ...} entry is a git-literal shorthand ("owner/repo").
		// Validate it as one: this branch previously stored the value
		// VERBATIM with empty Owner/Repo, so a value like
		// "ext::sh -c '<cmd>'" flowed through resolveCloneURL unchanged and
		// reached `git clone` as a remote-helper transport (RCE). Parsing it
		// as a shorthand rejects any non-owner/repo string outright and, for
		// legitimate values, populates Owner/Repo/Host so resolveCloneURL
		// builds a proper https URL instead of cloning the raw string.
		name := kv["name"]
		ref, err := ParseDepString(name)
		if err != nil {
			return nil, fmt.Errorf("dependency entry %d: invalid name %q: %w", idx, name, err)
		}
		if ref.IsLocal || ref.Source != "git" {
			return nil, fmt.Errorf("dependency entry %d: name %q must be a git repository shorthand (owner/repo)", idx, name)
		}
		if kv["alias"] != "" {
			ref.Alias = kv["alias"]
		}
		return ref, nil
	}

	return nil, fmt.Errorf("dependency entry %d has no source key (git, id, path, name, or marketplace)", idx)
}

// parseSkillsField validates and normalizes a dependency dict's `skills:`
// sequence (BUG-2, prd.md B2-1: SKILL_BUNDLE subset selection), mirroring
// Python's reference.py:920-940 exactly: the field must be a non-empty
// YAML sequence of non-empty string scalars; each name is trimmed, then
// the result is deduplicated and sorted. A skill name later becomes a
// filesystem path segment during deploy, so names are additionally
// rejected if they are "." / ".." or contain a path separator ("/" or
// "\\") -- apm-go's simplified, whole-name traversal guard (Python's
// validate_path_segments splits on separators and rejects "."/".."
// per-segment; skill names are never expected to contain a separator at
// all, so rejecting one outright is at least as strict).
func parseSkillsField(node *yaml.Node, idx int) ([]string, error) {
	// An explicit `skills: null` (or bare `skills:` with no value) means
	// "no subset declared", identical to the key being absent entirely.
	// This is deliberate Python parity, not fail-open leniency: Python's
	// reference.py reads the field with entry.get("skills") and skips the
	// whole validation block when it is None, so a YAML null never errors
	// there either (codex flagged this as fail-open; parity wins by task
	// convention and the choice is locked by an explicit test).
	if node.Kind == yaml.ScalarNode && node.Tag == "!!null" {
		return nil, nil
	}
	if node.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("dependency entry %d: 'skills' field must be a list of skill names", idx)
	}
	if len(node.Content) == 0 {
		return nil, fmt.Errorf("dependency entry %d: 'skills' must contain at least one name; remove the field to install all skills in the bundle", idx)
	}

	seen := make(map[string]bool, len(node.Content))
	validated := make([]string, 0, len(node.Content))
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			return nil, fmt.Errorf("dependency entry %d: each entry in 'skills' must be a non-empty string", idx)
		}
		name := strings.TrimSpace(item.Value)
		if name == "" {
			return nil, fmt.Errorf("dependency entry %d: each entry in 'skills' must be a non-empty string", idx)
		}
		// ":" additionally rejects Windows drive/volume forms ("C:") that
		// ContainsAny's separator check alone would let through. URL-encoded
		// forms ("%2e%2e") are deliberately NOT decoded-and-rejected: nothing
		// in the deploy chain URL-decodes a skill name -- the subset is a
		// whitelist matched against on-disk skill directory names, never a
		// path constructor -- so the encoded form is just a name that will
		// never match anything.
		if name == "." || name == ".." || strings.ContainsAny(name, `/\:`) {
			return nil, fmt.Errorf("dependency entry %d: invalid skill name %q", idx, name)
		}
		if !seen[name] {
			seen[name] = true
			validated = append(validated, name)
		}
	}
	sort.Strings(validated)
	return validated, nil
}

// ValidateResolved returns an error if d is still an unresolved marketplace
// dependency (Source=="marketplace") -- mkt-030's "resolve before persist"
// invariant. A dependencies.apm dict entry ({name, marketplace, version})
// only ever comes out of ParseDepDict in this state; before any code path
// writes a DependencyReference back into apm.yml it must first be collapsed
// into an ordinary git/local reference via marketplace.ResolvePlugin
// (mkt-029). Mirrors the Python original's raise ValueError guard in
// to_apm_yml_entry() -- an unresolved marketplace ref must never be
// serialized.
func (d *DependencyReference) ValidateResolved() error {
	if d.Source == "marketplace" {
		return fmt.Errorf("cannot write unresolved marketplace dependency %q (marketplace %q) to apm.yml; resolve it via ResolvePlugin first", d.MarketplacePluginName, d.MarketplaceName)
	}
	return nil
}

// ToCanonical returns the canonical form of a dependency reference.
// GROUNDWORK: no CLI caller in Phase 1; normalize stays byte-exact.
func (d *DependencyReference) ToCanonical(defaultHost string) string {
	if d.IsLocal {
		return d.LocalPath
	}
	if d.IsParent {
		return "parent"
	}

	// Local git repo (git: ./path) — Owner/Repo empty, RepoURL is the path
	if d.Owner == "" && d.Repo == "" && d.RepoURL != "" {
		return d.RepoURL
	}

	var sb strings.Builder
	if d.Host != "" && !strings.EqualFold(d.Host, defaultHost) {
		sb.WriteString(d.Host)
		sb.WriteByte('/')
	}
	sb.WriteString(d.Owner)
	sb.WriteByte('/')
	sb.WriteString(strings.TrimSuffix(d.Repo, ".git"))
	if d.VirtualPath != "" {
		sb.WriteByte('/')
		sb.WriteString(d.VirtualPath)
	}
	if d.Reference != "" {
		sb.WriteByte('#')
		sb.WriteString(d.Reference)
	}
	return sb.String()
}

// IdentityKey returns the identity used to compare a dependency reference
// against a lockfile entry (LockedDep.UniqueKey()) or another reference,
// deliberately ignoring Reference (git ref/tag) and Alias -- un-011: two
// references to the same repo_url[/virtual_path] that only differ by ref or
// alias are the same uninstall target. Mirrors deploy.DepRefKey exactly
// (kept as a separate copy rather than an internal/manifest -> internal/deploy
// import to avoid a package cycle: internal/deploy already imports
// internal/manifest). Local and parent references have no stable identity
// (matching deploy.DepRefKey's "" for IsLocal/IsParent) and always return "".
func (d *DependencyReference) IdentityKey() string {
	if d.IsLocal || d.IsParent {
		return ""
	}
	if d.VirtualPath != "" {
		return d.RepoURL + "/" + d.VirtualPath
	}
	return d.RepoURL
}

// IsAbsoluteLocalPath reports whether s is an OS-absolute filesystem path in
// any form apm.yml/the CLI may need to round-trip: POSIX ("/..."), Windows
// drive-letter ("C:\..." or "C:/..."), or UNC ("\\host\share..."). Checked
// via filepath.IsAbs/filepath.VolumeName (native to the running GOOS) plus
// explicit POSIX "/" and UNC "\\" prefix checks, so a path written on one OS
// still parses as absolute when apm.yml is later read on another.
func IsAbsoluteLocalPath(s string) bool {
	if filepath.IsAbs(s) {
		return true
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\\`) {
		return true
	}
	return filepath.VolumeName(s) != ""
}

func classifyVirtualPath(vp string) string {
	for _, ext := range virtualFileExtensions {
		if strings.HasSuffix(vp, ext) {
			return "file"
		}
	}
	return "subdirectory"
}

func anyEmpty(parts []string) bool {
	for _, part := range parts {
		if part == "" {
			return true
		}
	}
	return false
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func isGitHubHost(host string) bool {
	lower := strings.ToLower(host)
	return lower == "github.com" || strings.HasSuffix(lower, ".github.com") || strings.HasSuffix(lower, ".ghe.com")
}

func isGitLabHost(host string) bool {
	lower := strings.ToLower(host)
	return lower == "gitlab.com" || strings.HasSuffix(lower, ".gitlab.com")
}

func isVisualStudioLegacyHost(host string) bool {
	return strings.HasSuffix(strings.ToLower(host), ".visualstudio.com")
}

func isAzureDevOpsHost(host string) bool {
	lower := strings.ToLower(host)
	return lower == "dev.azure.com" || isVisualStudioLegacyHost(lower)
}

func removeGitMarker(parts []string) []string {
	for i, part := range parts {
		if part == "_git" {
			return append(append([]string(nil), parts[:i]...), parts[i+1:]...)
		}
	}
	return parts
}

func artifactoryPathPrefix(parts []string) (string, bool) {
	if len(parts) < 4 || !strings.EqualFold(parts[0], "artifactory") {
		return "", false
	}
	return "artifactory/" + parts[1], true
}

func virtualFileTail(parts []string) bool {
	if len(parts) == 0 {
		return false
	}
	last := parts[len(parts)-1]
	for _, ext := range virtualFileExtensions {
		if strings.HasSuffix(last, ext) {
			return true
		}
	}
	return false
}

func gitLabRepoSegmentCount(parts []string) int {
	n := len(parts)
	if !virtualFileTail(parts) {
		return n
	}
	for i := 2; i < n; i++ {
		if parts[i] == "prompts" || parts[i] == "instructions" || parts[i] == "collections" {
			return i
		}
	}
	switch {
	case n == 3:
		return 2
	case n == 4:
		return 3
	default:
		return 3
	}
}

func repositoryFields(repoURL string) (owner, repo string) {
	parts := strings.Split(repoURL, "/")
	if len(parts) < 2 {
		return "", ""
	}
	return strings.Join(parts[:len(parts)-1], "/"), parts[len(parts)-1]
}

func setRepositoryFields(d *DependencyReference, repoURL string, ado, allowPercentEncoded bool) error {
	if err := validateRepositoryPath(repoURL, ado, allowPercentEncoded); err != nil {
		return err
	}
	d.Owner, d.Repo = repositoryFields(repoURL)
	return nil
}

func validateRepositoryPath(repoURL string, ado, allowPercentEncoded bool) error {
	parts := strings.Split(repoURL, "/")
	if len(parts) < 2 {
		if ado {
			return fmt.Errorf("Invalid Azure DevOps repository format: %s. Expected 'org/project/repo'", repoURL)
		}
		return fmt.Errorf("invalid repository format: %s", repoURL)
	}
	if err := validateEncodedPathSegments(repoURL, "repository path"); err != nil {
		return err
	}
	for _, part := range parts {
		if part == "" || isDotSegment(part) {
			return fmt.Errorf("Invalid repository path component: %s", part)
		}
		if ado {
			if !adoSegmentRe.MatchString(part) {
				return fmt.Errorf("Invalid repository path component: %s", part)
			}
		} else if allowPercentEncoded {
			if !percentEncodedRepoCharRe.MatchString(part) {
				return fmt.Errorf("Invalid repository path component: %s", part)
			}
		} else if !repoCharRe.MatchString(part) {
			return fmt.Errorf("Invalid repository path component: %s", part)
		}
	}
	return nil
}

// splitRef splits a trailing "#ref" fragment off s, mirroring the Oracle's
// fragment handling (reference.py:580-583): the ref is WHITESPACE-STRIPPED
// and an empty result means "no ref" (`fragment.strip() or None`), so a
// bare trailing "#" or "#   " parses the same as no fragment at all. No
// charset validation happens here or at any parse-time call site -- the
// Oracle performs none (probed directly: '#-evil', '#a b', '#v1..2' all
// parse), and apm-go's git invocations are already argument-injection-safe
// without one (gitops/clone.go passes the ref only as `--branch <ref>`'s
// value and terminates option parsing with `--` before positionals).
func splitRef(s string) (ref, rest string) {
	idx := strings.LastIndex(s, "#")
	if idx < 0 {
		return "", s
	}
	return strings.TrimSpace(s[idx+1:]), s[:idx]
}

// splitPathAlias ports the Oracle's path-level @alias handling for explicit
// SSH forms (reference.py:564-580 and 1254-1259). The alias is separated
// after the URL fragment has been removed, so both ssh://host/owner/repo@name
// and git@host:owner/repo@name preserve the same Alias field.
func splitPathAlias(s string) (path, alias string) {
	idx := strings.LastIndexByte(s, '@')
	if idx < 0 {
		return s, ""
	}
	return s[:idx], strings.TrimSpace(s[idx+1:])
}

// netlocHostPort ports urllib.parse's urlsplit netloc semantics for the
// https/http/ssh URL forms (reference.py's _parse_standard_url and
// _parse_ssh_protocol_url both read parsed.hostname / parsed.port):
// userinfo -- everything through the LAST "@" -- is dropped (conformance
// rows url-userinfo), the hostname is LOWERCASED (url-uppercase-host), an
// empty port ("host:") counts as absent (url-port-empty), and a present
// port must be all digits in 0-65535 -- port ZERO is valid
// (url-port-zero/ssh-port-zero), unlike the shorthand form's
// parseHostPort below, whose own grammar rejects it (shorthand-port-zero:
// the Oracle's _split_shorthand_host_port raises for port 0 while
// urlsplit does not).
func netlocHostPort(netloc string) (host string, port int, err error) {
	return parseNetlocHostPort(netloc, true)
}

// netlocHostPortSSH matches urllib.parse.urlsplit's SSH behavior: the
// Oracle's ssh:// parser accepts the host string returned by urlsplit without
// applying the HTTP/shorthand host character or FQDN gates (reference.py:
// 558-560). SSH hosts such as "host!bang", "host_name", and the encoded
// "host%20name" therefore remain accepted verbatim.
func netlocHostPortSSH(netloc string) (host string, port int, err error) {
	return parseNetlocHostPort(netloc, false)
}

func parseNetlocHostPort(netloc string, validateHost bool) (host string, port int, err error) {
	if at := strings.LastIndexByte(netloc, '@'); at >= 0 {
		netloc = netloc[at+1:]
	}
	host = netloc
	if idx := strings.LastIndexByte(netloc, ':'); idx >= 0 {
		host = netloc[:idx]
		ps := netloc[idx+1:]
		if ps != "" {
			for i := 0; i < len(ps); i++ {
				if ps[i] < '0' || ps[i] > '9' {
					return "", 0, fmt.Errorf("invalid port in %q", netloc)
				}
			}
			p, e := strconv.Atoi(ps)
			if e != nil || p > portRangeMax {
				return "", 0, fmt.Errorf("invalid port in %q", netloc)
			}
			port = p
		}
	}
	host = strings.ToLower(host)
	if host == "" || (validateHost && !hostCharRe.MatchString(host)) {
		return "", 0, fmt.Errorf("invalid host %q", host)
	}
	return host, port, nil
}

func parseHostPort(s string) (host string, port int, err error) {
	if idx := strings.LastIndex(s, ":"); idx >= 0 {
		host = s[:idx]
		p, e := strconv.Atoi(s[idx+1:])
		if e != nil || p < 1 || p > portRangeMax {
			return "", 0, fmt.Errorf("invalid port in %q", s)
		}
		port = p
	} else {
		host = s
	}
	if !hostCharRe.MatchString(host) {
		return "", 0, fmt.Errorf("invalid host %q", host)
	}
	return host, port, nil
}

func validateVirtualPath(vp string) error {
	if err := validateEncodedPathSegments(vp, "virtual path"); err != nil {
		return err
	}
	parts := strings.Split(vp, "/")
	for _, seg := range parts {
		if seg == "" {
			continue
		}
		if !segmentRe.MatchString(seg) {
			return fmt.Errorf("invalid virtual path segment %q", seg)
		}
		// path_security.py:64's validate_path_segments (called on both
		// repo_url and every virtual path) rejects a "." or ".." segment
		// outright, even though such a segment happens to match the
		// otherwise-permissive character class above (both consist only
		// of allowed "." characters) -- ticket 11 attempt 5's conformance
		// sweep found apm-go silently accepted "owner/../repo" as
		// Repo=".." VirtualPath="repo" instead of rejecting the traversal
		// segment, a real (if narrow) bug the sweep's own probes revealed,
		// not one of the evaluator's five named reproducers.
		if isDotSegment(seg) {
			return fmt.Errorf("invalid virtual path segment %q", seg)
		}
	}
	if strings.HasSuffix(vp, ".collection.yml") || strings.HasSuffix(vp, ".collection.yaml") {
		return fmt.Errorf(".collection.yml is no longer supported. Convert '%s' to an apm.yml with a 'dependencies' section. See: https://microsoft.github.io/apm/guides/dependencies/", vp)
	}
	if virtualFileTail(parts) {
		return nil
	}
	last := parts[len(parts)-1]
	if strings.Contains(last, ".") {
		return fmt.Errorf("Invalid virtual package path '%s'. Individual files must end with one of: .prompt.md, .instructions.md, .agent.md. For subdirectory packages, the path should not have a file extension.", vp)
	}
	return nil
}

// isDotSegment reports whether a path segment is exactly "." or ".." --
// the traversal markers path_security.py's validate_path_segments rejects
// wherever it runs (repo_url, virtual paths). ownerCharRe/repoCharRe's
// character class alone does not exclude either (both consist only of
// "."), so every owner/repo validation call site pairs its char-class
// check with this one.
func isDotSegment(s string) bool {
	return s == "." || s == ".."
}
