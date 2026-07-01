package credsec

import (
	"path/filepath"
	"strings"
)

// redactedMarker replaces credential material in any user-facing surface.
const redactedMarker = "[REDACTED]"

// Redactor scrubs known credential literals (tokens, basic-auth passwords,
// bearer strings) out of any string before it reaches a diagnostic, log, error,
// lockfile, packed bundle, or audit record (req-sc-007). Credentials must be
// referenced by source descriptor (e.g. "GITHUB_APM_PAT env var"), never by value.
type Redactor struct {
	secrets []string
}

// NewRedactor builds a Redactor over the given credential literals; empty values
// are ignored.
func NewRedactor(values ...string) *Redactor {
	r := &Redactor{}
	for _, v := range values {
		if v != "" {
			r.secrets = append(r.secrets, v)
		}
	}
	return r
}

// Redact replaces every known credential literal in s with [REDACTED].
func (r *Redactor) Redact(s string) string {
	for _, sec := range r.secrets {
		s = strings.ReplaceAll(s, sec, redactedMarker)
	}
	return s
}

// MatchesSecretPattern reports whether a file path matches the default producer
// secret-pattern set (req-sc-007): .env, .env.*, *.pem, *.key, id_rsa,
// id_ed25519. The Producer toolchain (apm pack, Phase 7) refuses to pack such
// files; the set MAY be extended via policy.
func MatchesSecretPattern(p string) bool {
	// Case-insensitive: KEY.PEM / .ENV / ID_RSA must match too (esp. on
	// case-insensitive filesystems).
	base := strings.ToLower(filepath.Base(filepath.FromSlash(p)))
	switch base {
	case ".env", "id_rsa", "id_ed25519":
		return true
	}
	if strings.HasPrefix(base, ".env.") {
		return true
	}
	if strings.HasSuffix(base, ".pem") || strings.HasSuffix(base, ".key") {
		return true
	}
	return false
}
