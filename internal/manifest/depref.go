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
	RepoURL     string
	Host        string
	Owner       string
	Repo        string
	Reference   string
	VirtualPath string
	VirtualType string // "file" or "subdirectory"
	Alias       string
	IsLocal     bool
	LocalPath   string
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
	".prompt.md", ".instructions.md", ".agent.md", ".chatmode.md",
}

var (
	ownerCharRe  = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	repoCharRe   = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	hostCharRe   = regexp.MustCompile(`^[A-Za-z0-9.-]+$`)
	segmentRe    = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	portRangeMax = 65535

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
// This is an APPROXIMATION of the Oracle's DependencyReference.parse
// (reference.py), not a claimed exact port -- that function is its own
// multi-hundred-line module covering GitLab nested groups, Azure DevOps
// org/project/repo and legacy *.visualstudio.com shapes, and Artifactory
// VCS paths that this function does not implement at all. The actual,
// current scope statement is spec/conformance/depref-accept.json: a table
// generated directly from the pinned Oracle's own parse() (see
// tools/depref_conformance_gen.py), asserted against this function by
// TestParseDepString_OracleConformance. Every row without a `known_gap`
// entry must match; every row with one names a specific, deliberate
// remaining divergence. Earlier comments in this file and in
// internal/marketplace/models.go claimed narrower, one-off "ports X
// exactly" scopes that each turned out to still be missing accepted Oracle
// grammar (an arbitrary SSH user, a URL query string, a shorthand
// host:port split) -- the conformance table exists specifically so a
// future gap shows up as a fixture-regeneration diff instead of another
// evaluator-supplied reproducer.
func ParseDepString(s string) (*DependencyReference, error) {
	if s == "" {
		return nil, fmt.Errorf("empty dependency string")
	}

	// reference.py:1748: `dependency_str = urllib.parse.unquote(dependency_str)`
	// runs ONCE on the whole string, before local-path detection, host
	// parsing, everything -- so every check below (including error message
	// interpolation) operates on the DECODED string, exactly like the
	// Oracle reassigning its own local variable. Percent-decoding first
	// (rather than, say, only within a virtual-path segment) is also what
	// makes a percent-encoded traversal marker (e.g. "%2e%2e") visible to
	// containsEscape below instead of slipping past it as an opaque
	// "%2e%2e" segment -- see TestParseDepString_PercentEncodedTraversal.
	s = lenientUnquote(s)

	// reference.py:1750-1751: `if any(ord(c) < 32 for c in dependency_str)`
	// runs AFTER the decode, on the decoded string.
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

// lenientUnquote ports Python's urllib.parse.unquote (used as
// reference.py:1748's whole-string percent-decode) exactly: percent-decode
// is lenient, not strict. An invalid escape (a "%" not followed by two hex
// digits) is left completely unconsumed -- the literal "%" and whatever
// follows it pass through unchanged, rather than erroring like
// net/url.PathUnescape would. The resulting bytes are then UTF-8-decoded
// with errors='replace' semantics: an invalid byte sequence becomes one
// U+FFFD per Go's utf8.DecodeRune (which, like CPython's UTF-8 codec,
// implements the Unicode "maximal subpart" replacement algorithm).
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

	// _validate_url_repo_path (reference.py:1495-1502): the URL path is
	// stripped of slashes on BOTH ends (so leading "//" and a trailing "/"
	// both collapse -- conformance rows url-leading-double-slash and
	// url-trailing-slash), a terminal ".git" comes off the raw path before
	// splitting, and every part is percent-unquoted AGAIN on top of
	// parse()'s whole-string unquote at reference.py:1748 -- a genuine
	// double decode (conformance row url-double-encoded:
	// "owner/%2572epo" -> "%72epo" -> "repo").
	path := strings.Trim(rawPath, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("dependency %q: url-form requires host/owner/repo", s)
	}
	owner := lenientUnquote(parts[0])
	repo := lenientUnquote(parts[1])

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
		Scheme:  scheme,
		Source:  "git",
	}
	if ref != "" {
		d.Reference = ref
	}
	if len(parts) == 3 && parts[2] != "" {
		// _validate_url_repo_path unquotes EVERY path part (the terminal
		// ".git" already came off the raw path end above, pre-split).
		segs := strings.Split(parts[2], "/")
		for i, sg := range segs {
			segs[i] = lenientUnquote(sg)
		}
		vp := strings.Join(segs, "/")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
	}
	return d, nil
}

func parseSSHURL(s string) (*DependencyReference, error) {
	rest := strings.TrimPrefix(s, "ssh://")

	// reference.py:558-571 (_parse_ssh_protocol_url): userinfo is whatever
	// precedes the first "@" that itself precedes the first "/" (the host
	// boundary) -- ANY valid SSH user, defaulting to "git" when absent.
	// Ticket 11 eval attempt 4's reproducer 1.
	user := "git"
	if at, slash := strings.IndexByte(rest, '@'), strings.IndexByte(rest, '/'); at >= 0 && (slash < 0 || at < slash) {
		candidate := rest[:at]
		if err := validateSSHUser(candidate); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		user = candidate
		rest = rest[at+1:]
	}

	ref, rest := splitRef(rest)
	rest = stripQuery(rest) // urlparse separates query from path -- reproducer 2
	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return nil, fmt.Errorf("dependency %q: ssh url-form requires host/owner/repo", s)
	}
	netloc, rawPath := rest[:slash], rest[slash+1:]

	host, port, err := netlocHostPort(netloc)
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
	path := strings.TrimLeft(rawPath, "/")
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
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return nil, fmt.Errorf("dependency %q: SCP form requires user@host:owner/repo", s)
	}
	owner := parts[0]
	repo := strings.TrimSuffix(parts[1], ".git")

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
		RepoURL: owner + "/" + repo,
		Scheme:  "git",
		Source:  "git",
	}
	if user != "git" {
		d.SSHUser = user
	}
	if ref != "" {
		d.Reference = ref
	}
	if len(parts) == 3 && parts[2] != "" {
		vp := strings.TrimSuffix(parts[2], ".git")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
		}
		d.VirtualPath = vp
		d.VirtualType = classifyVirtualPath(vp)
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

	var host, owner, repo string
	var vpParts []string

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
		owner = parts[1]
		repo = parts[2]
		if len(parts) > 3 {
			vpParts = parts[3:]
		}
	} else {
		owner = first
		repo = parts[1]
		if len(parts) > 2 {
			vpParts = parts[2:]
		}
	}

	repo = strings.TrimSuffix(repo, ".git")

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
		RepoURL: owner + "/" + repo,
		Source:  "git",
	}
	if host != "" {
		d.Port = port
	}
	if ref != "" {
		d.Reference = ref
	}
	if len(vpParts) > 0 {
		vp := strings.Join(vpParts, "/")
		if err := validateVirtualPath(vp); err != nil {
			return nil, fmt.Errorf("dependency %q: %w", s, err)
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
	if !hostCharRe.MatchString(host) {
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
	for _, seg := range strings.Split(vp, "/") {
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
