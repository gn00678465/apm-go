// Package credsec implements the OpenAPM v0.1 §10.3 credential / host-class
// safety controls: host-class classification (req-sc-005), cross-host-class
// redirect credential dropping (req-sc-003), git-over-HTTP credential gating
// (req-sc-008), and credential / secret-file redaction (req-sc-007).
package credsec

import (
	"strings"

	"golang.org/x/net/publicsuffix"
)

// HostClass returns the credential-scope identity of a host: its registrable
// domain (eTLD+1 per the Public Suffix List). Port is stripped. Hosts the PSL
// cannot classify (IPs, single-label hosts like "localhost") fail safe to a
// singleton class — the host verbatim — so they never share credentials with
// any other host (req-sc-005).
func HostClass(host string) string {
	host = stripPort(strings.TrimSpace(host))
	host = strings.ToLower(host)
	if host == "" {
		return ""
	}
	etld1, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil || etld1 == "" {
		return host // singleton: degenerate host is its own class
	}
	return etld1
}

// SameHostClass reports whether a and b belong to one credential scope: identical
// eTLD+1, OR an explicit alias relationship declared in registries.<n>.aliases.
// Classification MUST NOT use CNAME chains, TLS SAN, or HTTP redirects
// (req-sc-005). aliases maps a registry's primary host to its additional hosts.
func SameHostClass(a, b string, aliases map[string][]string) bool {
	a = strings.ToLower(stripPort(strings.TrimSpace(a)))
	b = strings.ToLower(stripPort(strings.TrimSpace(b)))
	if a == "" || b == "" {
		return false
	}
	if a == b {
		return true
	}
	if HostClass(a) == HostClass(b) {
		return true
	}
	return aliasLinked(a, b, aliases)
}

// aliasLinked reports whether a and b fall in the same explicit alias group.
// Each map entry {primary: [alias...]} forms one group {primary} ∪ aliases.
func aliasLinked(a, b string, aliases map[string][]string) bool {
	for primary, alts := range aliases {
		group := map[string]bool{strings.ToLower(primary): true}
		for _, h := range alts {
			group[strings.ToLower(stripPort(h))] = true
		}
		if group[a] && group[b] {
			return true
		}
	}
	return false
}

func stripPort(host string) string {
	// IPv6 literals are bracketed: [::1]:443 -> ::1
	if strings.HasPrefix(host, "[") {
		if i := strings.Index(host, "]"); i >= 0 {
			return host[1:i]
		}
	}
	if i := strings.LastIndex(host, ":"); i >= 0 && !strings.Contains(host[i+1:], ":") {
		return host[:i]
	}
	return host
}
