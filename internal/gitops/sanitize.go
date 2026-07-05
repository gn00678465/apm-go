package gitops

import "regexp"

// credentialInURLRe matches the "scheme://userinfo@" prefix of a URL
// embedding basic-auth-style credentials (e.g.
// "https://x-access-token:ghp_xxx@github.com/owner/repo.git"), capturing
// the scheme so it can be preserved while the userinfo is dropped.
var credentialInURLRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^/@\s]*@`)

// SanitizeGitOutput strips any "scheme://user:token@" userinfo embedded in
// s, mirroring the Python original's _sanitize_url/redact_token pair. Git
// subprocess stdout/stderr can otherwise echo a clone URL's embedded
// credentials back verbatim (e.g. a private HTTPS remote configured with a
// PAT) -- every marketplace git error message must be passed through this
// before it reaches a user-facing error or log line.
func SanitizeGitOutput(s string) string {
	return credentialInURLRe.ReplaceAllString(s, "$1")
}
