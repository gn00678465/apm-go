package manifest

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v4"
)

type MCPDependency struct {
	Name      string
	Transport string
	Command   string
	Args      *[]string // nil = absent; empty slice = explicit []
	URL       string
	Env       map[string]string
	Headers   map[string]string
	Registry  any    // nil=default, false=self-defined, string=custom URL
	Version   string // pinned registry entry version; only meaningful when Registry is not false
}

func ParseMCPEntry(entry *yaml.Node) (*MCPDependency, error) {
	if entry.Kind == yaml.ScalarNode {
		return &MCPDependency{Name: entry.Value}, nil
	}
	if entry.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("MCP entry must be a string or mapping")
	}

	m := &MCPDependency{}
	for i := 0; i < len(entry.Content)-1; i += 2 {
		k := entry.Content[i].Value
		v := entry.Content[i+1]
		switch k {
		case "name":
			m.Name = v.Value
		case "transport":
			m.Transport = v.Value
		case "command":
			m.Command = v.Value
		case "url":
			m.URL = v.Value
		case "version":
			m.Version = v.Value
		case "registry":
			if v.Tag == "!!bool" || v.Value == "false" || v.Value == "true" {
				if strings.EqualFold(v.Value, "false") {
					m.Registry = false
				} else {
					m.Registry = true
				}
			} else if v.Kind == yaml.ScalarNode {
				m.Registry = v.Value
			}
		case "args":
			args := []string{}
			if v.Kind == yaml.SequenceNode {
				for _, a := range v.Content {
					args = append(args, a.Value)
				}
			}
			m.Args = &args
		case "env":
			if v.Kind == yaml.MappingNode {
				m.Env = make(map[string]string)
				for j := 0; j < len(v.Content)-1; j += 2 {
					m.Env[v.Content[j].Value] = v.Content[j+1].Value
				}
			}
		case "headers":
			if v.Kind == yaml.MappingNode {
				m.Headers = make(map[string]string)
				for j := 0; j < len(v.Content)-1; j += 2 {
					m.Headers[v.Content[j].Value] = v.Content[j+1].Value
				}
			}
		}
	}
	return m, nil
}

// allowedMCPURLSchemes mirrors Python's _ALLOWED_URL_SCHEMES
// (models/dependency/mcp.py:40): only http/https literal MCP server URLs
// are accepted.
var allowedMCPURLSchemes = map[string]bool{"http": true, "https": true}

func ValidateMCP(m *MCPDependency) error {
	isSelfDefined := m.Registry == false
	if !isSelfDefined {
		return nil
	}

	if m.Transport == "" {
		return fmt.Errorf("self-defined MCP server requires 'transport'")
	}

	switch m.Transport {
	case "stdio":
		if m.Command == "" {
			return fmt.Errorf("MCP transport 'stdio' requires 'command'")
		}
		if strings.ContainsAny(m.Command, " \t") && m.Args == nil {
			// Never echo m.Command: a user who mistakenly passes a whole
			// shell command line as a single --mcp -- "..." argument
			// (instead of separate argv tokens) could have a secret
			// embedded in it (found by codex review).
			return fmt.Errorf("MCP command for %q contains whitespace but no 'args' key is present; put arguments in 'args', not 'command'", m.Name)
		}
	case "http", "sse", "streamable-http":
		if m.URL == "" {
			return fmt.Errorf("MCP transport %q requires 'url'", m.Transport)
		}
		// Reject URL-embedded credentials (userinfo): they would otherwise
		// be persisted verbatim into apm.yml (typically git-committed) and
		// written into the deployed target config file in plaintext
		// (found by codex review of the --mcp CLI feature; enforced here
		// so every self-defined MCP entry is covered, not just --mcp's).
		// A coarse "@" check runs first and fails closed on its own: a
		// malformed URL that still embeds a literal "@" (credentials) must
		// not slip through just because url.Parse errors on it -- the
		// original version only checked u.User on a successful parse,
		// silently skipping the guard on a parse error (found in a
		// follow-up codex review round). None of these error messages echo
		// m.URL: a malformed-but-tokened URL (e.g. an invalid percent-escape
		// alongside a "?token=..." query) must not leak through the error
		// text either (found in a further follow-up round).
		if strings.Contains(m.URL, "@") {
			return fmt.Errorf("MCP server %q: url must not contain embedded credentials", m.Name)
		}
		// mf-013 placeholders (${VAR}, ${env:VAR}, ${input:...}, ${{ ... }})
		// are resolved by plain, position-agnostic substring substitution
		// (manifest.ResolvePlaceholders never parses the surrounding URL
		// grammar), so a placeholder can legitimately land anywhere --
		// including the port ("https://host:${PORT}/x") or an IPv6 host
		// ("https://[${HOST}]/x"). An earlier version of this check
		// substituted every placeholder with a fixed "x" token and fully
		// re-parsed the result as a structured URL, but that rejected
		// exactly these legitimate positions ("x" is not a valid port or
		// IPv6 literal) (found in a further follow-up round). Checking for
		// a malformed percent-escape directly on the raw value instead
		// (the specific defect class this was trying to catch, e.g.
		// "https://example.com/%zz/${TOKEN}") is independent of URL
		// grammar position, so it needs no placeholder-aware substitution.
		if hasMalformedPercentEscape(m.URL) {
			return fmt.Errorf("MCP server %q: url is not a valid URL", m.Name)
		}
		// The remaining checks (full parse, credential-on-parse-success,
		// absolute-URL) only make sense for a fully literal value -- a
		// placeholder-containing URL is not real yet at declaration time,
		// it's resolved later per target (bake) or preserved verbatim for
		// runtime resolution (translate, e.g. Copilot's
		// "${input:mcp-url}").
		if !HasPlaceholder(m.URL) {
			u, err := url.Parse(m.URL)
			if err != nil {
				return fmt.Errorf("MCP server %q: url is not a valid URL", m.Name)
			}
			if u.User != nil {
				return fmt.Errorf("MCP server %q: url must not contain embedded credentials", m.Name)
			}
			// Require an absolute URL (scheme + host): url.Parse accepts a
			// bare relative string like "example.com/mcp" without error
			// (Go treats it as a relative reference with an empty
			// Scheme/Host), which would otherwise pass validation, get
			// persisted to apm.yml, and only fail silently at deploy time
			// against the writer's own https-prefix guard (found by codex
			// review).
			if u.Scheme == "" || u.Host == "" {
				return fmt.Errorf("MCP server %q: url must be absolute (scheme://host/...)", m.Name)
			}
			// Mirror Python's _ALLOWED_URL_SCHEMES = frozenset({"http",
			// "https"}) (models/dependency/mcp.py:40,249): reject any other
			// literal scheme (e.g. ftp://, ws://) instead of silently
			// persisting it into apm.yml only to fail later at deploy time
			// (live-CLI finding: `install --mcp --url ftp://...` used to
			// exit 0). Only the scheme is named in the error, not the rest
			// of the URL, since a query string can carry a token (mirrors
			// this function's existing "never echo secretish values"
			// convention).
			if !allowedMCPURLSchemes[strings.ToLower(u.Scheme)] {
				return fmt.Errorf("MCP server %q: url scheme %q is not supported; use http:// or https://", m.Name, u.Scheme)
			}
		}
	default:
		return fmt.Errorf("unknown MCP transport %q", m.Transport)
	}

	return nil
}

// hasMalformedPercentEscape reports whether s contains a "%" not followed by
// two hex digits -- url.Parse rejects this as "invalid URL escape", but
// checking it directly (rather than via url.Parse on a placeholder-
// substituted skeleton) works regardless of where an mf-013 placeholder
// appears in the string.
func hasMalformedPercentEscape(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+2 >= len(s) || !isHexDigit(s[i+1]) || !isHexDigit(s[i+2]) {
			return true
		}
	}
	return false
}

func isHexDigit(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

// ── Placeholder recognition (mf-013) — recognition only, no parse-time rejection ──

var (
	EnvVarRe   = regexp.MustCompile(`\$\{(?:env:)?([A-Za-z_][A-Za-z0-9_]*)\}`)
	InputVarRe = regexp.MustCompile(`\$\{input:([^}]+)\}`)
	ActionsRe  = regexp.MustCompile(`\$\{\{.*?\}\}`)
)

type PlaceholderType int

const (
	PlaceholderEnv PlaceholderType = iota
	PlaceholderInput
	PlaceholderActions
)

type Placeholder struct {
	Type    PlaceholderType
	Raw     string
	VarName string
}

func RecognizePlaceholders(s string) []Placeholder {
	var result []Placeholder

	for _, m := range ActionsRe.FindAllString(s, -1) {
		result = append(result, Placeholder{Type: PlaceholderActions, Raw: m})
	}
	for _, m := range InputVarRe.FindAllStringSubmatch(s, -1) {
		result = append(result, Placeholder{Type: PlaceholderInput, Raw: m[0], VarName: m[1]})
	}
	for _, m := range EnvVarRe.FindAllStringSubmatch(s, -1) {
		result = append(result, Placeholder{Type: PlaceholderEnv, Raw: m[0], VarName: m[1]})
	}
	return result
}

// ── Marketplace source validation (mf-017) ──

// marketplaceHostPattern/marketplaceSegmentPattern/marketplaceOwnerRepoPattern
// mirror upstream's _HOST_PAT/_SEGMENT_PAT/_OWNER_REPO_PAT exactly
// (apm/src/apm_cli/marketplace/yml_schema.py:93-97, external audit round 5,
// 2026-07-30): the host segment must look like an FQDN (one or more dotted
// labels, then a final label starting with a letter) to disambiguate
// "host/owner/repo" from plain "owner/repo"; each owner/repo segment is
// restricted to [A-Za-z0-9._-] -- i.e. it cannot contain "@", ":", "?", or
// "/", the exact characters a userinfo, port, query string, or SCP-style SSH
// remote ("git@host:path") would need to embed.
const (
	marketplaceHostPattern      = `(?:[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?\.)+[A-Za-z][A-Za-z0-9-]*`
	marketplaceSegmentPattern   = `[A-Za-z0-9._-]+`
	marketplaceOwnerRepoPattern = marketplaceSegmentPattern + `/` + marketplaceSegmentPattern
	// marketplaceHTTPSRepoPattern is the full-URL form's repository path:
	// two or more segments, mirroring v0.28.0's _HTTPS_REPOSITORY_PAT
	// (yml_schema.py:98, PR #2439) -- nested paths like
	// "group/subgroup/repo" are allowed on the https:// form only; the
	// host-prefixed and bare shorthands stay exactly owner/repo.
	marketplaceHTTPSRepoPattern = marketplaceSegmentPattern + `(?:/` + marketplaceSegmentPattern + `)+`
)

// marketplaceSourceRe mirrors upstream's SOURCE_RE (yml_schema.py:100-107):
// exactly four accepted shapes -- 'owner/repo', 'host.tld/owner/repo',
// 'https://host.tld/owner/repo[.git]', or './...' (a local path, matching
// anything after the "./" prefix). Everything else is rejected.
//
// BLOCKING 1 (external audit round 5, 2026-07-30): the previous
// implementation was a hand-picked blocklist (absolute/UNC paths, ".."
// segments) followed by a hand-picked allowlist (a "://"-containing URL,
// anything starting with ".") that fell through to an unconditional
// "shorthand form -- accepted" for anything else -- a fail-OPEN default.
// That silently accepted an SCP-style SSH remote ("git@host:path" never
// contains "://", so the https-only branch never saw it), an arbitrary SSH
// destination ("git@evil.example.com:x/y"), and a Windows drive-relative
// path with no separator after the colon ("C:foo", "c:foo", "C:" --
// isAbsoluteOrUNCSource's own doc comment used to explicitly exclude this
// shape as "not a filesystem path shape ... left to the existing
// shorthand/URL branches", which was exactly the bug). Matching against
// this fixed four-shape grammar instead is fail-CLOSED: nothing reaches an
// implicit accept branch, since there is no such branch left. This one
// regex closes all three of the above without any additional prefix
// special-case: none of "git@" (no "/" between "git@host" and ":path" in a
// way SEGMENT_PAT would accept), "ssh://" (not the literal "https://"
// prefix), or "C:foo" (contains ":" -- not a SEGMENT_PAT character, and no
// "/" at all) can match any of the four alternatives below.
var marketplaceSourceRe = regexp.MustCompile(
	`^(?:https://` + marketplaceHostPattern + `/` + marketplaceHTTPSRepoPattern + `(?:\.git)?` +
		`|` + marketplaceHostPattern + `/` + marketplaceOwnerRepoPattern +
		`|` + marketplaceOwnerRepoPattern +
		`|\./.*` +
		`)$`,
)

func ValidateMarketplaceSource(source string) error {
	if source == "" {
		return fmt.Errorf("marketplace source is empty")
	}

	if !marketplaceSourceRe.MatchString(source) {
		return fmt.Errorf("marketplace source %q must be one of 'owner/repo', 'host.tld/owner/repo', 'https://host.tld/owner/repo[.git]' (nested paths allowed), or './path'", source)
	}

	// The grammar match above already makes a userinfo ("user@"), a port
	// (":8080"), a query string ("?x=1"), an SCP-style SSH remote
	// ("git@host:path"), an absolute filesystem path, or a UNC path
	// structurally impossible in anything that reaches this point: none of
	// "@", ":", or "?" is a marketplaceSegmentPattern/marketplaceHostPattern
	// character, so a source containing any of them already failed the check
	// above -- no separate URL-parsing branch is needed for those cases
	// anymore (BLOCKING 1, external audit round 5, 2026-07-30).
	isLocal := strings.HasPrefix(source, "./")

	// '..' (and, for a non-local source, a bare '.') segment check: mirrors
	// upstream's validate_path_segments(source, allow_current_dir=is_local)
	// (path_security.py), run unconditionally once the shape match succeeds
	// -- marketplaceSegmentPattern's character class permits a literal "."
	// or ".." as an ordinary segment (e.g. "owner/.." matches the grammar's
	// owner/repo shape structurally), so the grammar match alone is not
	// sufficient; this second stage is what actually rejects those. Both "/"
	// and "\" are treated as separators before splitting: a forward-slash-
	// only split lets a Windows-style "..\" segment (e.g.
	// "./..\\..\\outside") slip through unrejected on any OS, since Go
	// source-code string literals don't get OS-specific separator
	// translation the way filepath does (BLOCKING 1, external audit round 3,
	// 2026-07-30). This is one of two independent layers: see
	// authoring/refcheck.go's resolveCloneURL for the second (a
	// resolved-path-stays-within-root check).
	reject := map[string]bool{"..": true}
	if !isLocal {
		// A bare "." segment is only meaningful for a local path (e.g.
		// "./foo/./bar"); upstream rejects it for a non-local (remote
		// shorthand or URL) source too (path_security.py's
		// allow_current_dir=False default), even though
		// marketplaceSegmentPattern's character class would otherwise accept
		// a single "." character as an ordinary segment.
		reject["."] = true
	}
	normalizedSource := strings.ReplaceAll(source, "\\", "/")
	for _, seg := range strings.Split(normalizedSource, "/") {
		if reject[seg] {
			return fmt.Errorf("marketplace source %q contains %q path segment", source, seg)
		}
		// MAJOR 1 (external audit, 2026-07-30): upstream's
		// validate_path_segments (path_security.py:64) percent-decodes each
		// segment (up to 8 rounds) before comparing it against "."/"..", so a
		// percent-encoded (or multiply percent-encoded) traversal marker --
		// e.g. "%2e%2e", "%252e%252e" (needs 2 rounds), "%2E%2E" -- cannot
		// bypass the literal check above. This mirrors that: a segment
		// containing an ordinary "%" that is not part of a traversal escape
		// (e.g. "50%25off") simply fails to decode further and is left
		// untouched, so it is never rejected.
		if decoded := decodePercentEncodedSegment(seg); decoded != seg && reject[decoded] {
			return fmt.Errorf("marketplace source %q contains a percent-encoded %q path segment", source, decoded)
		}
	}

	return nil
}

// maxPercentDecodeRounds bounds decodePercentEncodedSegment's iterative
// percent-decode, mirroring upstream path_security.py's own decode budget.
const maxPercentDecodeRounds = 8

// decodePercentEncodedSegment iteratively percent-decodes seg (as
// net/url.PathUnescape would) up to maxPercentDecodeRounds times, stopping
// as soon as a round leaves the string unchanged or fails to decode further.
// It never returns an error: a segment that cannot be fully decoded is
// simply returned as far as decoding got, since the only thing the caller
// checks is whether the fully-decoded result is an exact "." or ".."
// traversal marker.
func decodePercentEncodedSegment(seg string) string {
	decoded := seg
	for i := 0; i < maxPercentDecodeRounds; i++ {
		next, err := url.PathUnescape(decoded)
		if err != nil || next == decoded {
			break
		}
		decoded = next
	}
	return decoded
}
