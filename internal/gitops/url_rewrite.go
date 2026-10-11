package gitops

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Git URL rewrite safety check: a port of the Oracle v0.33.0 check in
// utils/git_env.py (:136-219 errors and probe, :529-604 config read,
// :607-690 authorization, :702-891 policy). A `url.<base>.insteadOf` rule in
// the user's git configuration silently changes where a network git
// subprocess connects; ValidateURLRewriteSafety rejects the rules that would
// downgrade the transport, add a credential, or move an authenticated or
// network remote to another host.
//
// Not ported from the same file, because apm-go has no code that feeds them:
// trusted git executable resolution (:222-309), the config snapshot written
// into GIT_CONFIG_COUNT / GIT_CONFIG_KEY_n and its re-validation (:894-1052,
// :1197-1207), and the auth fence (:1082-1130).

const (
	urlRewriteInspection = `inspect matching rules with 'git config --show-origin --get-regexp ^url\..*\.insteadOf$'`

	// URLRewriteRecoveryHint is the Oracle's GitUrlRewriteError.recovery_hint
	// (git_env.py:141,150).
	URLRewriteRecoveryHint = urlRewriteInspection + " and remove the unsafe rule"
)

// URLRewriteError is a rejected rewrite (git_env.py:144-151). Reason is the
// Oracle's machine-readable reason code.
type URLRewriteError struct {
	Reason  string
	message string
}

func (e *URLRewriteError) Error() string {
	return e.message + "; " + URLRewriteRecoveryHint
}

// URLRewriteProbeError is a failure to inspect the effective git
// configuration (git_env.py:154-163). It never carries git's own output.
type URLRewriteProbeError struct {
	Category string
}

func (e *URLRewriteProbeError) Error() string {
	return fmt.Sprintf("Unable to verify Git URL rewrite safety (%s); check Git configuration and retry; %s",
		e.Category, urlRewriteInspection)
}

// urlRewriteParseError is the Oracle's bare ValueError for a URL its parser
// rejects (git_env.py:772-773).
type urlRewriteParseError struct{}

func (urlRewriteParseError) Error() string { return "Unable to verify Git URL rewrite safety" }

// ConfigProbe is one `git config` inspection the check needs to run.
type ConfigProbe struct {
	Args []string
	Env  []string
	// RepoNeutral asks for a working directory where no repository's local
	// configuration is visible.
	RepoNeutral bool
}

// ConfigProbeFunc runs one ConfigProbe. exitCode is git's exit status when it
// ran to completion. err reports that it did not: exec.ErrNotFound when there
// is no git, context.DeadlineExceeded when ctx expired, anything else when it
// could not be started.
type ConfigProbeFunc func(ctx context.Context, p ConfigProbe) (stdout []byte, exitCode int, err error)

var configProbe ConfigProbeFunc = execConfigProbe

// configProbeTimeouts is the Oracle's one retry for slow CI runners
// (git_env.py:136,206-219).
var configProbeTimeouts = []time.Duration{10 * time.Second, 30 * time.Second}

// configProbeWaitDelay bounds the wait for a killed git's pipes, which a
// surviving grandchild would otherwise hold open past the deadline.
const configProbeWaitDelay = time.Second

func execConfigProbe(ctx context.Context, p ConfigProbe) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, "git", p.Args...)
	cmd.Env = p.Env
	cmd.WaitDelay = configProbeWaitDelay
	if p.RepoNeutral && cmd.Err == nil {
		// git_env.py:669: the directory of the git executable is never inside
		// the user's repository.
		if resolved, err := filepath.EvalSymlinks(cmd.Path); err == nil {
			cmd.Dir = filepath.Dir(resolved)
		}
	}
	out, err := cmd.Output()
	if err == nil {
		return out, 0, nil
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, 0, context.DeadlineExceeded
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out, ee.ExitCode(), nil
	}
	return nil, 0, err
}

// errGitNotFound separates "no git" from a probe failure. The Oracle raises
// FileNotFoundError from get_git_executable before the probe starts
// (git_env.py:538, outside the try at :559), and every caller reports it as
// a missing git, not as an unverifiable configuration.
var errGitNotFound = errors.New("git executable not found")

func runConfigProbe(p ConfigProbe, timeoutCategory string) ([]byte, int, error) {
	for _, timeout := range configProbeTimeouts {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		out, code, err := configProbe(ctx, p)
		cancel()
		switch {
		case err == nil:
			return out, code, nil
		case errors.Is(err, exec.ErrNotFound):
			return nil, 0, errGitNotFound
		case errors.Is(err, context.DeadlineExceeded):
			continue
		}
		return nil, 0, err
	}
	return nil, 0, &URLRewriteProbeError{Category: timeoutCategory}
}

type gitConfigEntry struct {
	key, value string
}

type urlRewriteRule struct {
	replacement, prefix string
}

type gitConfigSnapshot struct {
	rewrites    []urlRewriteRule
	httpHeaders []gitConfigEntry
}

// ValidateURLRewriteSafety must pass before any git subprocess connects to
// remoteURL. It returns nil when no insteadOf rule applies to remoteURL, a
// *URLRewriteError when the rule that applies is unsafe, and a
// *URLRewriteProbeError when the configuration could not be read.
//
// A missing git returns nil: the network subprocess that follows fails the
// same lookup, and each call site keeps its own report for that.
//
// Deviation from the Oracle: it probes from the git executable's directory
// and runs ls-remote there too (git_env.py:546-550,1295), so a repository's
// local configuration never applies. apm-go's network git runs in the
// process working directory (a relative local URL depends on it), where
// ls-remote and fetch do read the local configuration, so the probe runs
// there as well. For clone, which ignores the local configuration, an unsafe
// local rule is still rejected.
func ValidateURLRewriteSafety(remoteURL string) error {
	err := validateURLRewriteSafety(remoteURL)
	if errors.Is(err, errGitNotFound) {
		return nil
	}
	return err
}

func validateURLRewriteSafety(remoteURL string) error {
	snapshot, err := readEffectiveGitConfig()
	if err != nil {
		return err
	}
	effectiveURL, rewritten := resolveURLRewrite(remoteURL, snapshot.rewrites)
	if !rewritten {
		return nil
	}
	// git_env.py:864-871 evaluates the authorization probe before the policy,
	// so its failure wins over a rejection.
	hasAuthorization, err := hasApplicableHTTPAuthorization(effectiveURL, snapshot.httpHeaders)
	if err != nil {
		return err
	}
	return validateResolvedURLRewrite(remoteURL, effectiveURL, hasAuthorization)
}

// readEffectiveGitConfig mirrors _read_effective_git_config
// (git_env.py:529-604).
func readEffectiveGitConfig() (gitConfigSnapshot, error) {
	out, code, err := runConfigProbe(ConfigProbe{
		Args: []string{"config", "--null", "--show-scope", "--list"},
		Env:  SecureGitEnv(),
	}, "Git config probe timed out")
	if err != nil {
		var probeErr *URLRewriteProbeError
		if errors.Is(err, errGitNotFound) || errors.As(err, &probeErr) {
			return gitConfigSnapshot{}, err
		}
		return gitConfigSnapshot{}, &URLRewriteProbeError{Category: "Git config probe could not start"}
	}
	if code != 0 {
		return gitConfigSnapshot{}, &URLRewriteProbeError{Category: "Git config probe failed"}
	}
	return parseGitConfigList(out)
}

const insteadOfSuffix = ".insteadof"

func parseGitConfigList(out []byte) (gitConfigSnapshot, error) {
	malformed := &URLRewriteProbeError{Category: "Git config returned malformed output"}
	var fields [][]byte
	for _, field := range bytes.Split(out, []byte{0}) {
		if len(field) > 0 {
			fields = append(fields, field)
		}
	}
	if len(fields)%2 != 0 {
		return gitConfigSnapshot{}, malformed
	}

	var snapshot gitConfigSnapshot
	for i := 0; i < len(fields); i += 2 {
		scope, payload := fields[i], fields[i+1]
		rawKey, rawValue, found := bytes.Cut(payload, []byte{'\n'})
		if !found {
			return gitConfigSnapshot{}, malformed
		}
		if !utf8.Valid(scope) || !utf8.Valid(rawKey) || !utf8.Valid(rawValue) {
			return gitConfigSnapshot{}, &URLRewriteProbeError{Category: "Git config returned non-UTF-8 output"}
		}
		key, value := string(rawKey), string(rawValue)
		normalized := strings.ToLower(key)
		if strings.HasPrefix(normalized, "http.") {
			if strings.HasSuffix(normalized, ".extraheader") && isValidHTTPExtraHeaderValue(value) {
				snapshot.httpHeaders = append(snapshot.httpHeaders, gitConfigEntry{key: key, value: value})
			}
			continue
		}
		if !strings.HasPrefix(normalized, "url.") || !strings.HasSuffix(normalized, insteadOfSuffix) {
			continue
		}
		replacement := ""
		if end := len(key) - len(insteadOfSuffix); end > len("url.") {
			replacement = key[len("url."):end]
		}
		if replacement == "" || value == "" {
			return gitConfigSnapshot{}, &URLRewriteProbeError{Category: "Git config returned an incomplete rewrite"}
		}
		snapshot.rewrites = append(snapshot.rewrites, urlRewriteRule{replacement: replacement, prefix: value})
	}
	return snapshot, nil
}

// resolveURLRewrite applies git's longest-prefix insteadOf rule; the first of
// equally long prefixes wins (git_env.py:732-743).
func resolveURLRewrite(remoteURL string, rewrites []urlRewriteRule) (string, bool) {
	best := -1
	for i, rule := range rewrites {
		if !strings.HasPrefix(remoteURL, rule.prefix) {
			continue
		}
		if best < 0 || len(rule.prefix) > len(rewrites[best].prefix) {
			best = i
		}
	}
	if best < 0 {
		return "", false
	}
	return rewrites[best].replacement + remoteURL[len(rewrites[best].prefix):], true
}

var (
	remoteHelperRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*::`)
	scpHostRe      = regexp.MustCompile(`^(?:[^/@:\s]+@)?(\[[^\]]+\]|[^/:@\s]+):`)
)

// validateResolvedURLRewrite mirrors validate_resolved_git_url_rewrite
// (git_env.py:754-839). The order of the rules decides which message a URL
// that breaks several of them gets.
func validateResolvedURLRewrite(remoteURL, effectiveURL string, hasAuthorization bool) error {
	if remoteHelperRe.MatchString(effectiveURL) {
		return &URLRewriteError{"insecure-transport", "Git URL rewrite must not select remote-helper syntax"}
	}
	source, err := pyURLSplit(remoteURL)
	if err != nil {
		return urlRewriteParseError{}
	}
	target, err := pyURLSplit(effectiveURL)
	if err != nil {
		return urlRewriteParseError{}
	}
	sourceOrigin, err := source.origin()
	if err != nil {
		return urlRewriteParseError{}
	}
	targetOrigin, err := target.origin()
	if err != nil {
		return urlRewriteParseError{}
	}

	if target.containsCredentials() {
		return &URLRewriteError{"credentials", "Git URL rewrite replacement must not contain credentials"}
	}
	if target.scheme == "http" && source.scheme != "http" {
		if source.scheme == "https" {
			return &URLRewriteError{"https-downgrade", "HTTPS Git remote must not rewrite to insecure HTTP"}
		}
		return &URLRewriteError{"insecure-transport", "Git remote must not rewrite to insecure HTTP"}
	}
	switch target.scheme {
	case "", "file", "https", "ssh":
	default:
		return &URLRewriteError{"insecure-transport", "HTTPS Git remote must not rewrite to an insecure transport"}
	}
	if source.containsCredentials() {
		return &URLRewriteError{"credential-origin", "Credential-bearing Git remote must not be rewritten"}
	}
	// The Oracle's managed-auth rule (git_env.py:805-818) is not ported: it is
	// keyed on APM_GIT_MANAGED_AUTH_INTENT, which only the Oracle's own auth
	// resolver sets on a child environment, and apm-go has no such resolver.
	targetIsHTTP := target.scheme == "http" || target.scheme == "https"
	if targetIsHTTP && hasAuthorization && sourceOrigin != targetOrigin {
		return &URLRewriteError{"credential-origin",
			"Authenticated Git remote must not rewrite to a different " + strings.ToUpper(target.scheme) + " origin"}
	}
	sourceHost := gitURLHost(remoteURL, source)
	targetIsNetwork := targetIsHTTP || target.scheme == "ssh" ||
		(target.scheme == "" && scpHostRe.MatchString(effectiveURL))
	if sourceHost != "" && targetIsNetwork && sourceHost != gitURLHost(effectiveURL, target) {
		return &URLRewriteError{"cross-host", "Git remote must not rewrite to a different network host"}
	}
	return nil
}

// gitURLHost mirrors _git_url_host (git_env.py:719-729): the network host of
// a URL or of git's SCP-style syntax, "" when there is none.
func gitURLHost(raw string, parsed pyURL) string {
	switch parsed.scheme {
	case "http", "https", "ssh":
		return parsed.hostname()
	case "":
	default:
		return ""
	}
	m := scpHostRe.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	return strings.ToLower(strings.Trim(m[1], "[]"))
}

// hasApplicableHTTPAuthorization mirrors _has_applicable_http_authorization
// (git_env.py:607-631): whether git would send a credential-bearing header to
// remoteURL, from GIT_HTTP_EXTRAHEADER or from the http.<url>.extraHeader
// group git selects for it.
func hasApplicableHTTPAuthorization(remoteURL string, headers []gitConfigEntry) (bool, error) {
	parsed, err := pyURLSplit(remoteURL)
	if err != nil || (parsed.scheme != "http" && parsed.scheme != "https") {
		return false, nil
	}
	if isCredentialBearingHTTPHeader(os.Getenv("GIT_HTTP_EXTRAHEADER")) {
		return true, nil
	}
	selected, err := urlMatchedHeaderGroup(remoteURL, headers)
	if err != nil {
		return false, err
	}
	// An empty value resets the headers configured before it.
	var active []string
	for _, entry := range selected {
		if strings.TrimSpace(entry.value) == "" {
			active = nil
			continue
		}
		active = append(active, entry.value)
	}
	for _, value := range active {
		if isCredentialBearingHTTPHeader(value) {
			return true, nil
		}
	}
	return false, nil
}

const urlMatchProbeHeader = "X-Apm-Config-Probe: "

// urlMatchedHeaderGroup mirrors _urlmatched_header_group (git_env.py:634-690).
// Git's URL matching decides which http.<url>.extraHeader key applies; rather
// than reimplement it, the probe replays every collected key with its index
// as the value and asks git which one it selects.
func urlMatchedHeaderGroup(remoteURL string, headers []gitConfigEntry) ([]gitConfigEntry, error) {
	if len(headers) == 0 {
		return nil, nil
	}
	var env []string
	for _, kv := range SecureGitEnv() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT" ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM="+os.DevNull,
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_COUNT="+strconv.Itoa(len(headers)),
	)
	for i, entry := range headers {
		env = append(env,
			fmt.Sprintf("GIT_CONFIG_KEY_%d=%s", i, entry.key),
			fmt.Sprintf("GIT_CONFIG_VALUE_%d=%s%d", i, urlMatchProbeHeader, i),
		)
	}

	out, code, err := runConfigProbe(ConfigProbe{
		Args:        []string{"config", "--null", "--get-urlmatch", "http.extraHeader", remoteURL},
		Env:         env,
		RepoNeutral: true,
	}, "Git URL-match probe timed out")
	if err != nil {
		var probeErr *URLRewriteProbeError
		if errors.Is(err, errGitNotFound) || errors.As(err, &probeErr) {
			return nil, err
		}
		return nil, &URLRewriteProbeError{Category: "Git URL-match probe could not start"}
	}
	if code == 1 {
		return nil, nil
	}
	if code != 0 {
		return nil, &URLRewriteProbeError{Category: fmt.Sprintf("Git URL-match probe exited with status %d", code)}
	}
	malformed := &URLRewriteProbeError{Category: "Git URL-match probe returned malformed output"}
	index, found := strings.CutPrefix(strings.TrimRight(string(out), "\x00\n"), urlMatchProbeHeader)
	if !found {
		return nil, malformed
	}
	selected, convErr := strconv.Atoi(index)
	if convErr != nil || selected < 0 || selected >= len(headers) {
		return nil, malformed
	}
	var group []gitConfigEntry
	for _, entry := range headers {
		if strings.EqualFold(entry.key, headers[selected].key) {
			group = append(group, entry)
		}
	}
	return group, nil
}

var (
	httpHeaderNameRe = regexp.MustCompile("^[!#$%&'*+\\-.^_`|~0-9A-Za-z]+$")
	authSchemeRe     = regexp.MustCompile(`(?i)^\s*(?:basic|bearer|token)\s+\S+`)
	headerNameSplit  = regexp.MustCompile(`[^a-z0-9]+`)

	// barePlatformTokenRe is _BARE_PLATFORM_TOKEN_RE (git_env.py:103-112). Go's
	// regexp has no lookaround, so the Oracle's (?<![A-Za-z0-9_]) and
	// (?![A-Za-z0-9_]) are spelled as a consumed boundary character; that
	// matches the same inputs when only "is there a match" is asked.
	barePlatformTokenRe = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:` +
		`github_pat_[A-Za-z0-9_]{20,}|` +
		`gh[oprsu]_[A-Za-z0-9_]{6,}|` +
		`gl(?:agent|cbt|ft|pat|ptt|rt|soat)[-_][A-Za-z0-9_-]{6,}|` +
		`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{6,}\.[A-Za-z0-9_-]{6,}|` +
		`[A-Za-z0-9]{75}AZDO[A-Za-z0-9]{5}|` +
		`[A-Za-z0-9]{52}` +
		`)(?:$|[^A-Za-z0-9_])`)

	sensitiveHTTPHeaderParts = map[string]bool{
		"auth": true, "authorization": true, "cookie": true, "credential": true,
		"key": true, "password": true, "secret": true, "token": true,
	}
)

// isValidHTTPExtraHeaderValue mirrors _is_valid_http_extraheader_value
// (git_env.py:387-398): empty, or one well-formed header line.
func isValidHTTPExtraHeaderValue(value string) bool {
	if strings.TrimSpace(value) == "" {
		return true
	}
	for _, r := range value {
		if (r < 32 && r != '\t') || r == 127 {
			return false
		}
	}
	name, _, found := strings.Cut(value, ":")
	return found && httpHeaderNameRe.MatchString(name)
}

// isCredentialBearingHTTPHeader mirrors _is_credential_bearing_http_header
// (git_env.py:369-384).
func isCredentialBearingHTTPHeader(value string) bool {
	if !isValidHTTPExtraHeaderValue(value) {
		return false
	}
	stripped := strings.TrimSpace(value)
	if stripped == "" {
		return false
	}
	// A valid header value has a colon, so the Oracle's branch for a value
	// with none cannot be reached here and is not ported.
	name, fieldValue, _ := strings.Cut(stripped, ":")
	if barePlatformTokenRe.MatchString(fieldValue) || authSchemeRe.MatchString(fieldValue) {
		return true
	}
	for _, part := range headerNameSplit.Split(strings.ToLower(name), -1) {
		if sensitiveHTTPHeaderParts[part] {
			return true
		}
	}
	return false
}
