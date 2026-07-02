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

func ValidateMarketplaceSource(source string) error {
	if source == "" {
		return fmt.Errorf("marketplace source is empty")
	}

	// (a) reject .. segments
	for _, seg := range strings.Split(source, "/") {
		if seg == ".." {
			return fmt.Errorf("marketplace source %q contains '..' path segment", source)
		}
	}

	// local path must start with ./
	if strings.HasPrefix(source, ".") {
		if !strings.HasPrefix(source, "./") {
			return fmt.Errorf("local marketplace source must start with './'")
		}
		return nil
	}

	// remote URL
	if strings.Contains(source, "://") {
		u, err := url.Parse(source)
		if err != nil {
			return fmt.Errorf("marketplace source %q is not a valid URL", source)
		}
		// (c) https only for remote
		if u.Scheme != "https" {
			return fmt.Errorf("remote marketplace source must use https://, got %q", u.Scheme)
		}
		// (b) no userinfo
		if u.User != nil {
			return fmt.Errorf("marketplace source %q must not contain userinfo", source)
		}
		// (b) no port
		if u.Port() != "" {
			return fmt.Errorf("marketplace source %q must not contain a port", source)
		}
		// (b) no query
		if u.RawQuery != "" {
			return fmt.Errorf("marketplace source %q must not contain a query string", source)
		}
		return nil
	}

	// shorthand form (host/owner/repo or owner/repo) — accepted
	return nil
}
