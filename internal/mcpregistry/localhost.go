package mcpregistry

import (
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

// LocalHost returns the host of registryURL, lowercased as Python's urlparse
// gives it, when it is a loopback, private, link-local, multicast or
// unspecified address, which the Oracle warns about for every registry it
// did not choose itself (install/mcp/registry.py:47-63,197-217).
func LocalHost(registryURL string) (string, bool) {
	u, err := url.Parse(registryURL)
	if err != nil {
		return "", false
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", false
	}
	if isLoopbackHost(host) {
		return host, true
	}
	addr, ok := parseHostAddress(host)
	if !ok {
		return "", false
	}
	return host, addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || pyIsPrivate(addr) || addr.IsMulticast() || addr.IsUnspecified()
}

// isLoopbackHost ports utils/net.py:39-58.
func isLoopbackHost(host string) bool {
	h := strings.TrimSuffix(strings.TrimSpace(host), ".")
	switch h {
	case "localhost", "ip6-localhost", "ip6-loopback":
		return true
	}
	addr, err := netip.ParseAddr(h)
	if err != nil {
		return false
	}
	if addr.Is4() {
		return addr.IsLoopback()
	}
	return addr == netip.IPv6Loopback()
}

// parseHostAddress ports utils/net.py:9-36: an IP literal, or an IPv4
// address written as one decimal or hexadecimal integer.
func parseHostAddress(host string) (netip.Addr, bool) {
	h := strings.TrimRight(strings.TrimSpace(host), ".")
	if h == "" {
		return netip.Addr{}, false
	}
	if addr, err := netip.ParseAddr(h); err == nil {
		return addr, true
	}
	var v uint64
	var err error
	switch {
	case strings.HasPrefix(h, "0x"):
		v, err = strconv.ParseUint(h[2:], 16, 64)
	case strings.Trim(h, "0123456789") == "":
		v, err = strconv.ParseUint(h, 10, 64)
	default:
		return netip.Addr{}, false
	}
	if err != nil || v > 0xFFFFFFFF {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
}

// The ranges Python 3.12's ipaddress takes as private, less the ones it
// excepts (Lib/ipaddress.py, _private_networks and
// _private_networks_exceptions).
var (
	pyPrivate4 = prefixes("0.0.0.0/8", "10.0.0.0/8", "127.0.0.0/8", "169.254.0.0/16", "172.16.0.0/12",
		"192.0.0.0/24", "192.0.0.170/31", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "255.255.255.255/32")
	pyPrivate4Except = prefixes("192.0.0.9/32", "192.0.0.10/32")
	pyPrivate6       = prefixes("::1/128", "::/128", "::ffff:0:0/96", "64:ff9b:1::/48", "100::/64", "2001::/23",
		"2001:db8::/32", "2002::/16", "3fff::/20", "fc00::/7", "fe80::/10")
	pyPrivate6Except = prefixes("2001:1::1/128", "2001:1::2/128", "2001:3::/32", "2001:4:112::/48",
		"2001:20::/28", "2001:30::/28")
)

func prefixes(ss ...string) []netip.Prefix {
	out := make([]netip.Prefix, len(ss))
	for i, s := range ss {
		out[i] = netip.MustParsePrefix(s)
	}
	return out
}

// pyIsPrivate is Python 3.12's is_private; an IPv4-mapped IPv6 address is
// judged as its IPv4 address.
func pyIsPrivate(addr netip.Addr) bool {
	addr = addr.WithZone("")
	if addr.Is4In6() {
		addr = addr.Unmap()
	}
	in := func(ps []netip.Prefix) bool {
		for _, p := range ps {
			if p.Contains(addr) {
				return true
			}
		}
		return false
	}
	if addr.Is4() {
		return in(pyPrivate4) && !in(pyPrivate4Except)
	}
	return in(pyPrivate6) && !in(pyPrivate6Except)
}
