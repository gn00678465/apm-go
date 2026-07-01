package credsec

import (
	"fmt"
	"net/http"
)

// maxRedirects mirrors net/http's default stopAfter cap.
const maxRedirects = 10

// credentialHeaders are dropped when a redirect crosses host classes (req-sc-003).
var credentialHeaders = []string{"Authorization", "Proxy-Authorization", "Cookie"}

// NewAuthDropRedirect returns a value for http.Client.CheckRedirect that drops
// the originating Authorization header (and other credential material) before
// following an HTTP 3xx whose target host classifies into a DIFFERENT host class
// than the original request (req-sc-003). Same-class redirects keep credentials.
// Host-class equivalence is computed per req-sc-005 (eTLD+1 or explicit aliases);
// CNAME / TLS SAN / shared redirects are never used.
//
// Stdlib interaction (important for the wiring phase): net/http.Client strips
// Authorization/Cookie on ANY cross-host redirect using its own host/subdomain
// rules BEFORE invoking CheckRedirect. So this policy reliably delivers the
// DROP guarantee (never leaks across host classes), but it cannot by itself
// deliver the KEEP guarantee for same-eTLD+1 different-host redirects (e.g.
// api.github.com -> codeload.github.com) — stdlib will have already removed the
// header. Whoever wires this into a live registry fetcher must re-attach the
// credential only after SameHostClass approves (custom RoundTripper), or follow
// redirects manually with http.ErrUseLastResponse. This is a drop-side policy.
func NewAuthDropRedirect(aliases map[string][]string) func(req *http.Request, via []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		origin := via[0].URL.Host
		if !SameHostClass(origin, req.URL.Host, aliases) {
			for _, h := range credentialHeaders {
				req.Header.Del(h)
			}
		}
		return nil
	}
}
