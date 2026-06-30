package credsec

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ShouldAttachCredential reports whether a credential may be attached to a
// git-over-HTTP fetch at rawURL (req-sc-008). https:// always permits it.
// http:// permits it only when the target host is loopback (127.0.0.0/8, ::1)
// or the registry is declared insecure:true. Any other scheme refuses.
func ShouldAttachCredential(rawURL string, insecure bool) (bool, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false, fmt.Errorf("parse url: %w", err)
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true, nil
	case "http":
		return insecure || isLoopbackHost(u.Hostname()), nil
	default:
		return false, nil
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}
