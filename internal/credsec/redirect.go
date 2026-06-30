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
