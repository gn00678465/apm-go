// Package registry implements the OpenAPM v0.1 registry HTTP download consumer
// (registry-http-api.md §3.1/§3.2), wiring the internal/credsec credential
// controls (sc-003/005/007/008) onto the real archive fetch.
package registry

import (
	"encoding/base64"
	"os"
	"strings"
)

// Credential is the resolved auth material for a registry. An empty Scheme means
// anonymous (no Authorization header).
type Credential struct {
	Scheme string // "bearer" | "basic" | "" (none)
	Value  string // bearer: token; basic: base64(user:pass)
	// redact holds every literal that must be scrubbed from diagnostics (sc-007):
	// for Basic that includes the raw user and pass, not only the base64 blob a
	// server could echo back decoded.
	redact []string
}

// Header returns the Authorization header value, or "" when anonymous.
func (c Credential) Header() string {
	switch c.Scheme {
	case "bearer":
		return "Bearer " + c.Value
	case "basic":
		return "Basic " + c.Value
	}
	return ""
}

// envSuffix derives the {NAME} env-var suffix from a registry name: uppercase,
// with '-' and '.' mapped to '_' (registry-http-api.md §2.3).
func envSuffix(registryName string) string {
	n := strings.ToUpper(registryName)
	n = strings.ReplaceAll(n, "-", "_")
	n = strings.ReplaceAll(n, ".", "_")
	return n
}

// ResolveCredential resolves the credential for a registry from environment
// variables (sc-007: referenced by env descriptor, never by literal). Bearer
// wins when both Bearer and Basic vars are set. Returns an empty Credential when
// none are configured (anonymous).
func ResolveCredential(registryName string) Credential {
	n := envSuffix(registryName)
	if tok := os.Getenv("APM_REGISTRY_TOKEN_" + n); tok != "" {
		return Credential{Scheme: "bearer", Value: tok, redact: []string{tok}}
	}
	user := os.Getenv("APM_REGISTRY_USER_" + n)
	pass := os.Getenv("APM_REGISTRY_PASS_" + n)
	if user != "" && pass != "" {
		enc := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		return Credential{Scheme: "basic", Value: enc, redact: []string{user, pass, enc}}
	}
	return Credential{}
}
