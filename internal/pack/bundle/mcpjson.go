package bundle

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The six secret-value regexes below are ported LITERALLY from
// core/plugin_manifest.py:100-153 (_URL_USERINFO_RE, _INLINE_SECRET_ARG_RE,
// _SPACE_SECRET_ARG_RE, _ENV_ASSIGN_SECRET_RE, _AUTH_SCHEME_RE,
// _KNOWN_SECRET_TOKEN_RE) -- pattern text and application order must match
// that file exactly (design.md Review Gate B). Go's RE2 (regexp package)
// supports every construct these patterns use (\b, \w, \S, character
// classes, non-capturing groups, case-insensitive (?i)); none use
// lookahead/lookbehind, which RE2 lacks.
var (
	urlUserinfoRe     = regexp.MustCompile(`\b([a-zA-Z][\w+.-]*://)([^/?#\s@]+)@`)
	inlineSecretArgRe = regexp.MustCompile(`(?i)(--?[\w.-]*(?:token|secret|password|credential|apikey|key)[\w.-]*=)(\S+)`)
	spaceSecretArgRe  = regexp.MustCompile(`(?i)(--?[\w.-]*(?:token|secret|password|credential|apikey|api-key|key)[\w.-]*\s+)(\S+)`)
	envAssignSecretRe = regexp.MustCompile(`(?i)\b([A-Za-z0-9_]*(?:token|secret|password|credential|apikey|api_key|key)[A-Za-z0-9_]*=)(\S+)`)
	authSchemeRe      = regexp.MustCompile(`(?i)\b(Bearer|Basic)\s+([A-Za-z0-9._~+/=-]{8,})`)
	// _KNOWN_SECRET_TOKEN_RE is deliberately case-SENSITIVE in the Python
	// original (no re.IGNORECASE) -- provider prefixes are canonical-case.
	knownSecretTokenRe = regexp.MustCompile(`\b(?:` +
		`gh[posur]_[A-Za-z0-9]{20,}` +
		`|github_pat_[A-Za-z0-9_]{20,}` +
		`|sk-(?:proj-)?[A-Za-z0-9_-]{20,}` +
		`|sk_(?:live|test)_[A-Za-z0-9]{20,}` +
		`|xox[baprs]-[A-Za-z0-9-]{10,}` +
		`|A(?:KIA|SIA)[A-Z0-9]{12,}` +
		`|AIza[A-Za-z0-9_-]{30,}` +
		`|glpat-[A-Za-z0-9_-]{20,}` +
		`|npm_[A-Za-z0-9]{20,}` +
		`|pypi-[A-Za-z0-9_-]{20,}` +
		`|hf_[A-Za-z0-9]{20,}` +
		`|SG\.[A-Za-z0-9_.-]{20,}` +
		`|sbp_[A-Za-z0-9]{20,}` +
		`|dapi[a-f0-9]{32}` +
		`)\b`)
	// secretFlagNameRe anchors the WHOLE string (^...$) -- it identifies a
	// bare array element that IS a secret flag name (e.g. "--token"), as
	// opposed to inlineSecretArgRe which matches "--token=value" inline.
	secretFlagNameRe = regexp.MustCompile(`(?i)^--?[\w.-]*(?:token|secret|password|credential|apikey|api-key|key)[\w.-]*$`)
)

const redacted = "***REDACTED***"

// redactSecretValues applies all six regexes in sequence (same order as
// _redact_secret_values, plugin_manifest.py:164-172) and reports whether
// anything changed.
func redactSecretValues(s string) (scrubbed string, changed bool) {
	scrubbed = urlUserinfoRe.ReplaceAllString(s, "${1}"+redacted+"@")
	scrubbed = inlineSecretArgRe.ReplaceAllString(scrubbed, "${1}"+redacted)
	scrubbed = spaceSecretArgRe.ReplaceAllString(scrubbed, "${1}"+redacted)
	scrubbed = envAssignSecretRe.ReplaceAllString(scrubbed, "${1}"+redacted)
	scrubbed = authSchemeRe.ReplaceAllString(scrubbed, "${1} "+redacted)
	scrubbed = knownSecretTokenRe.ReplaceAllString(scrubbed, redacted)
	return scrubbed, scrubbed != s
}

// sensitiveMCPKeyNames/sensitiveMCPKeySubstrings mirror
// _SENSITIVE_MCP_KEY_NAMES/_SENSITIVE_MCP_KEY_SUBSTRINGS
// (plugin_manifest.py:77-92).
var sensitiveMCPKeyNames = map[string]bool{
	"env": true, "environment": true, "headers": true, "authorization": true,
}
var sensitiveMCPKeySubstrings = []string{
	"token", "secret", "password", "credential", "apikey", "key",
}

// isSensitiveMCPKey mirrors _is_sensitive_mcp_key: lowercase + strip
// underscores, then check the exact-name set followed by the (deliberately
// over-broad) substring set.
func isSensitiveMCPKey(key string) bool {
	normalized := strings.ReplaceAll(strings.ToLower(key), "_", "")
	if sensitiveMCPKeyNames[normalized] {
		return true
	}
	for _, sub := range sensitiveMCPKeySubstrings {
		if strings.Contains(normalized, sub) {
			return true
		}
	}
	return false
}

// SanitizeValue recursively strips credential keys and redacts secret
// values under value, mirroring _sanitize_value (plugin_manifest.py:
// 175-214). path is the dotted/indexed path used to report dropped/redacted
// locations (starts as the server name for a top-level server object).
// dropped accumulates every path where a key was dropped or a value was
// redacted, in traversal order.
func SanitizeValue(value JSONValue, path string, dropped *[]string) JSONValue {
	switch value.Kind {
	case KindObject:
		return sanitizeObject(value, path, dropped)
	case KindArray:
		return sanitizeArray(value, path, dropped)
	case KindString:
		scrubbed, changed := redactSecretValues(value.S)
		if changed {
			*dropped = append(*dropped, path)
		}
		return StringValue(scrubbed)
	default:
		return value
	}
}

func sanitizeObject(value JSONValue, path string, dropped *[]string) JSONValue {
	cleaned := JSONValue{Kind: KindObject}
	for _, f := range value.O {
		child := f.Key
		if path != "" {
			child = path + "." + f.Key
		}
		if isSensitiveMCPKey(f.Key) {
			*dropped = append(*dropped, child)
			continue
		}
		cleaned.O = append(cleaned.O, JSONField{Key: f.Key, Val: SanitizeValue(f.Val, child, dropped)})
	}
	return cleaned
}

func sanitizeArray(value JSONValue, path string, dropped *[]string) JSONValue {
	cleaned := JSONValue{Kind: KindArray}
	redactNext := false
	for i, item := range value.A {
		child := fmt.Sprintf("%s[%d]", path, i)
		isFlag := item.Kind == KindString && secretFlagNameRe.MatchString(item.S)
		if redactNext && item.Kind == KindString && !isFlag {
			// Previous element was a bare secret flag (e.g. "--token");
			// this element is its space-separated value -- scrub whole.
			cleaned.A = append(cleaned.A, StringValue(redacted))
			*dropped = append(*dropped, child)
			redactNext = false
			continue
		}
		cleaned.A = append(cleaned.A, SanitizeValue(item, child, dropped))
		redactNext = isFlag
	}
	return cleaned
}

// SanitizeServers strips credential keys and redacts secret values across
// every server object in servers (an mcpServers-shaped JSONValue object),
// mirroring _sanitize_mcp_servers (plugin_manifest.py:258-278). Server
// names (the top-level keys) are never credential-tested themselves --
// only recursed into.
func SanitizeServers(servers JSONValue) (cleaned JSONValue, dropped []string) {
	cleaned = JSONValue{Kind: KindObject}
	for _, f := range servers.O {
		cleaned.O = append(cleaned.O, JSONField{Key: f.Key, Val: SanitizeValue(f.Val, f.Key, &dropped)})
	}
	return cleaned, dropped
}

// ReadMCPServers reads <rootDir>/.mcp.json's "mcpServers" object,
// mirroring collect_mcp_servers's file-reading half (plugin_manifest.py:
// 222-255, MINUS the sanitization call -- callers that need the sanitized
// form call SanitizeServers separately, since PluginManifestProducer and
// BundleProducer apply it at different points). Returns an empty object
// (never an error) when the file is absent, is a symlink, or cannot be
// parsed as a JSON object with an object-valued "mcpServers" key --
// matching Python's fail-open behavior.
func ReadMCPServers(rootDir string) JSONValue {
	empty := JSONValue{Kind: KindObject}
	path := filepath.Join(rootDir, ".mcp.json")
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || info.IsDir() {
		return empty
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return empty
	}
	root, err := DecodeJSONValue(data)
	if err != nil || root.Kind != KindObject {
		return empty
	}
	servers, ok := root.Get("mcpServers")
	if !ok || servers.Kind != KindObject {
		return empty
	}
	return servers
}
