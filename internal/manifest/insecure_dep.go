package manifest

import "fmt"

// CheckInsecureDependencyScheme refuses a non-TLS http:// git dependency
// unless allowInsecure was passed (apm-go install's --allow-insecure flag).
// The gate is flag-only with NO host exemption -- loopback and RFC1918-
// private hosts are refused like any other -- mirroring the Python reference
// implementation's _check_insecure_dependencies (insecure_policy.py). It is
// a no-op for any other scheme (https, ssh, git, local/parent refs whose
// Scheme is always "") and for a nil dep. defaultHost only feeds the error
// message's display URL when the dep carries no explicit Host.
func CheckInsecureDependencyScheme(dep *DependencyReference, allowInsecure bool, defaultHost string) error {
	if dep == nil || dep.Scheme != "http" {
		return nil
	}
	if allowInsecure {
		return nil
	}
	return fmt.Errorf(
		"%s -- HTTP dependency (unencrypted); refused by default. Pass --allow-insecure to apm-go install to permit it",
		insecureDependencyDisplayURL(dep, defaultHost),
	)
}

// insecureDependencyDisplayURL reconstructs a display URL for an insecure
// dependency error message. DependencyReference never stores credentials for
// git URL forms (parseHTTPURL has no userinfo support), so this never leaks
// them.
func insecureDependencyDisplayURL(dep *DependencyReference, defaultHost string) string {
	host := dep.Host
	if host == "" {
		host = defaultHost
	}
	display := fmt.Sprintf("http://%s/%s/%s", host, dep.Owner, dep.Repo)
	if dep.VirtualPath != "" {
		display += "/" + dep.VirtualPath
	}
	if dep.Reference != "" {
		display += "#" + dep.Reference
	}
	return display
}
