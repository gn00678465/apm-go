package gitops

import (
	"errors"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
)

// pyURL is the part of Python's urllib.parse.SplitResult the rewrite policy
// reads. The policy's verdicts depend on how the Oracle's parser splits a
// URL: "host:path" has the scheme "host", and "git@host:path" has none.
// net/url splits both differently, so the policy uses this port of
// urlsplit (CPython 3.12, Lib/urllib/parse.py) instead.
type pyURL struct {
	scheme string // lower-cased
	netloc string
}

var errPyURL = errors.New("invalid URL")

const pySchemeChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789+-."

var ipvFutureRe = regexp.MustCompile(`^v[a-fA-F0-9]+\..+$`)

// pyURLSplit mirrors urlsplit up to the netloc. The NFKC check on a
// non-ASCII netloc (_checknetloc) is not ported: it needs a normalization
// table the module does not depend on, and it only rejects more URLs.
func pyURLSplit(raw string) (pyURL, error) {
	u := strings.TrimLeft(raw, "\x00\x01\x02\x03\x04\x05\x06\x07\x08\t\n\x0b\x0c\r\x0e\x0f\x10\x11\x12\x13\x14\x15\x16\x17\x18\x19\x1a\x1b\x1c\x1d\x1e\x1f ")
	u = strings.NewReplacer("\t", "", "\r", "", "\n", "").Replace(u)

	var parsed pyURL
	if i := strings.IndexByte(u, ':'); i > 0 && isASCIILetter(u[0]) &&
		strings.Trim(u[:i], pySchemeChars) == "" {
		parsed.scheme, u = strings.ToLower(u[:i]), u[i+1:]
	}
	if !strings.HasPrefix(u, "//") {
		return parsed, nil
	}
	netloc := u[2:]
	if end := strings.IndexAny(netloc, "/?#"); end >= 0 {
		netloc = netloc[:end]
	}
	parsed.netloc = netloc

	open, closed := strings.Contains(netloc, "["), strings.Contains(netloc, "]")
	if open != closed {
		return pyURL{}, errPyURL
	}
	if !open {
		return parsed, nil
	}
	// _check_bracketed_netloc: the brackets hold an IPv6 or IPvFuture
	// address, with nothing around them but userinfo and a port.
	hostinfo := netloc[strings.LastIndexByte(netloc, '@')+1:]
	before, bracketed, hasBracket := strings.Cut(hostinfo, "[")
	var host string
	if hasBracket {
		var after string
		host, after, _ = strings.Cut(bracketed, "]")
		if before != "" || (after != "" && !strings.HasPrefix(after, ":")) {
			return pyURL{}, errPyURL
		}
	} else {
		host, _, _ = strings.Cut(hostinfo, ":")
	}
	if strings.HasPrefix(host, "v") {
		if !ipvFutureRe.MatchString(host) {
			return pyURL{}, errPyURL
		}
		return parsed, nil
	}
	if addr, err := netip.ParseAddr(host); err != nil || addr.Is4() {
		return pyURL{}, errPyURL
	}
	return parsed, nil
}

// hostinfo mirrors _NetlocResultMixinStr._hostinfo.
func (u pyURL) hostinfo() (host, port string) {
	hostinfo := u.netloc[strings.LastIndexByte(u.netloc, '@')+1:]
	if _, bracketed, found := strings.Cut(hostinfo, "["); found {
		var after string
		host, after, _ = strings.Cut(bracketed, "]")
		_, port, _ = strings.Cut(after, ":")
		return host, port
	}
	host, port, _ = strings.Cut(hostinfo, ":")
	return host, port
}

// hostname mirrors SplitResult.hostname: lower-cased except for an IPv6
// zone, "" where Python has None.
func (u pyURL) hostname() string {
	host, _ := u.hostinfo()
	name, zone, hasZone := strings.Cut(host, "%")
	if hasZone {
		return strings.ToLower(name) + "%" + zone
	}
	return strings.ToLower(name)
}

// userinfo mirrors SplitResult.username and .password as "is it set".
func (u pyURL) userinfo() (hasUsername, hasPassword bool) {
	at := strings.LastIndexByte(u.netloc, '@')
	if at < 0 {
		return false, false
	}
	return true, strings.Contains(u.netloc[:at], ":")
}

// containsCredentials mirrors _url_contains_credentials (git_env.py:712-716).
// An ssh user name ("git@host") is not a credential.
func (u pyURL) containsCredentials() bool {
	hasUsername, hasPassword := u.userinfo()
	if hasPassword {
		return true
	}
	switch u.scheme {
	case "http", "https", "file":
		return hasUsername
	}
	return false
}

// pyOrigin mirrors _url_origin's tuple (git_env.py:702-709); port 0 stands
// for Python's None.
type pyOrigin struct {
	scheme, host string
	port         int
}

func (u pyURL) origin() (pyOrigin, error) {
	origin := pyOrigin{scheme: u.scheme, host: u.hostname()}
	_, port := u.hostinfo()
	if port == "" {
		switch u.scheme {
		case "http":
			origin.port = 80
		case "https":
			origin.port = 443
		}
		return origin, nil
	}
	if strings.Trim(port, "0123456789") != "" {
		return pyOrigin{}, errPyURL
	}
	n, err := strconv.Atoi(port)
	if err != nil || n > 65535 {
		return pyOrigin{}, errPyURL
	}
	// An explicit ":0" must stay different from "no port".
	if n == 0 {
		n = -1
	}
	origin.port = n
	return origin, nil
}
