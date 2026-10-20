package main

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/apm-go/apm/internal/deploy"
	"github.com/apm-go/apm/internal/lockfile"
	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/resolver"
	"github.com/apm-go/apm/internal/ux"
)

// The Oracle's HTTP dependency policy (install/insecure_policy.py). A direct
// http:// dependency needs allow_insecure: true and --allow-insecure; a
// transitive one needs its host in the allow-list: --allow-insecure-host, or
// with --allow-insecure the host of a direct http:// dependency. Each http://
// dependency fetched gets a warning, and no other one is fetched over http.

// fqdnPattern is is_valid_fqdn's (utils/github_host.py:1009-1037).
var fqdnPattern = regexp.MustCompile(`^[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?(?:\.[a-zA-Z0-9](?:[a-zA-Z0-9-]*[a-zA-Z0-9])?)+$`)

// normalizeInsecureHosts is _allow_insecure_host_callback
// (install/insecure_policy.py:112-139): each --allow-insecure-host stripped
// and lowercased, once; a value that is no hostname is a usage error.
func normalizeInsecureHosts(raw []string) ([]string, error) {
	var hosts []string
	for _, h := range raw {
		n := strings.ToLower(strings.TrimSpace(h))
		if !fqdnPattern.MatchString(strings.SplitN(n, "/", 2)[0]) {
			return nil, fmt.Errorf("Invalid value for '--allow-insecure-host': Invalid hostname '%s'. Use a bare hostname like 'mirror.example.com'.", h)
		}
		if !containsStr(hosts, n) {
			hosts = append(hosts, n)
		}
	}
	return hosts, nil
}

// checkInsecureDirect is _check_insecure_dependencies over the direct
// dependencies, before anything is resolved or fetched.
func checkInsecureDirect(deps []*manifest.DependencyReference, allowFlag bool, defaultHost string) error {
	for _, dep := range deps {
		if err := manifest.CheckInsecureDependency(dep, allowFlag, defaultHost); err != nil {
			printInsecureRefusal(err.Error())
			return withSilentExitCode(1, err)
		}
	}
	return nil
}

// printInsecureRefusal prints the reason of a refused http dependency as the
// Oracle does: an error line, then its steps as they are.
func printInsecureRefusal(reason string) {
	lines := strings.Split(reason, "\n")
	ux.Error(os.Stdout, "%s", lines[0])
	for _, l := range lines[1:] {
		ux.Plain(os.Stdout, "%s", l)
	}
}

// insecurePolicy decides, for each http:// dependency, whether it may be
// fetched. direct are the keys of the direct dependencies, which
// checkInsecureDirect has already let through.
type insecurePolicy struct {
	direct      map[string]bool
	allowed     map[string]bool
	defaultHost string
}

func newInsecurePolicy(direct []*manifest.DependencyReference, allowFlag bool, hosts []string, defaultHost string) *insecurePolicy {
	p := &insecurePolicy{direct: map[string]bool{}, allowed: map[string]bool{}, defaultHost: defaultHost}
	for _, h := range hosts {
		p.allowed[h] = true
	}
	for _, d := range direct {
		p.direct[deploy.DepRefKey(d)] = true
		if allowFlag && d.IsInsecure() {
			if h := insecureURLHost(manifest.InsecureDependencyURL(d, defaultHost)); h != "" {
				p.allowed[h] = true
			}
		}
	}
	return p
}

// approve marks ref for an http fetch when the policy allows it, and reports
// whether it does.
func (p *insecurePolicy) approve(ref *manifest.DependencyReference) bool {
	if !ref.IsInsecure() {
		return true
	}
	if p.direct[deploy.DepRefKey(ref)] || p.allowed[insecureURLHost(manifest.InsecureDependencyURL(ref, p.defaultHost))] {
		ref.HTTPApproved = true
	}
	return ref.HTTPApproved
}

// insecureURLHost is _get_insecure_dependency_host (insecure_policy.py:142-146).
func insecureURLHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

// insecureLoader approves the http:// dependencies the policy allows before
// the resolver lists their tags or fetches them, and fetches none of the
// others: the Oracle refuses them once the graph is resolved
// (_guard_transitive_insecure_dependencies).
type insecureLoader struct {
	next   resolver.PackageLoader
	policy *insecurePolicy
	refs   map[string]*manifest.DependencyReference
}

func (l *insecureLoader) LoadPackage(ref *manifest.DependencyReference, resolvedRef string) (*manifest.Manifest, error) {
	l.refs[deploy.DepRefKey(ref)] = ref
	if !l.policy.approve(ref) {
		return nil, nil
	}
	m, err := l.next.LoadPackage(ref, resolvedRef)
	if m != nil {
		for _, d := range m.ParsedDeps {
			l.policy.approve(d)
		}
	}
	return m, err
}

// reportInsecure prints a warning for each http:// dependency of the
// resolved graph, in its order (_warn_insecure_dependencies), then refuses
// the hosts of the transitive ones the policy did not allow, sorted.
func (l *insecureLoader) reportInsecure(deps []resolver.ResolvedDep) error {
	var blocked []string
	for _, d := range deps {
		ref := l.refs[d.Key]
		if ref == nil || !ref.IsInsecure() {
			continue
		}
		u := manifest.InsecureDependencyURL(ref, l.policy.defaultHost)
		line := "Insecure HTTP fetch (unencrypted): " + u
		if !l.policy.direct[d.Key] {
			if parent := l.refs[d.ResolvedBy]; parent != nil {
				line += " (transitive, introduced by " + resolvingName(parent) + ")"
			}
			if !ref.HTTPApproved {
				if h := insecureURLHost(u); h != "" && !containsStr(blocked, h) {
					blocked = append(blocked, h)
				}
			}
		}
		ux.Warn(os.Stdout, "%s", line)
	}
	if len(blocked) == 0 {
		return nil
	}
	return refuseInsecureHosts(blocked)
}

// refuseInsecureHosts is the error of _guard_transitive_insecure_dependencies
// (install/insecure_policy.py:175-201).
func refuseInsecureHosts(blocked []string) error {
	sort.Strings(blocked)
	flags := make([]string, len(blocked))
	for i, h := range blocked {
		flags[i] = "--allow-insecure-host " + h
	}
	msg := fmt.Sprintf("Re-run with %s to allow transitive HTTP dependencies from unapproved host(s): %s.", strings.Join(flags, " "), strings.Join(blocked, ", "))
	ux.Error(os.Stdout, "%s", msg)
	return withSilentExitCode(1, errors.New(msg))
}

func containsStr(items []string, s string) bool {
	for _, item := range items {
		if item == s {
			return true
		}
	}
	return false
}

// checkFrozenInsecure applies the policy to the http:// entries of the lock a
// frozen install fetches from, as the Oracle's --frozen, an ordinary install,
// does (r6-or/httpfresh): it warns for each, in the lock's order, refuses a
// transitive one from a host the policy does not allow, and returns the keys
// it may fetch over http.
func checkFrozenInsecure(lock *lockfile.Lockfile, policy *insecurePolicy) (map[string]bool, error) {
	approved := map[string]bool{}
	var blocked []string
	byKey := map[string]*lockfile.LockedDep{}
	for i := range lock.Dependencies {
		byKey[lock.Dependencies[i].UniqueKey()] = &lock.Dependencies[i]
	}
	for i := range lock.Dependencies {
		d := &lock.Dependencies[i]
		if !d.IsInsecure {
			continue
		}
		ref := lockedInsecureRef(d)
		u := manifest.InsecureDependencyURL(ref, policy.defaultHost)
		line := "Insecure HTTP fetch (unencrypted): " + u
		if !policy.direct[d.UniqueKey()] {
			if parent := byKey[d.ResolvedBy]; parent != nil {
				line += " (transitive, introduced by " + lockedDisplayName(parent) + ")"
			}
		}
		ux.Warn(os.Stdout, "%s", line)
		if policy.approve(ref) {
			approved[d.UniqueKey()] = true
		} else if h := insecureURLHost(u); h != "" && !containsStr(blocked, h) {
			blocked = append(blocked, h)
		}
	}
	if len(blocked) == 0 {
		return approved, nil
	}
	return nil, refuseInsecureHosts(blocked)
}

// lockedInsecureRef is the reference of an http:// lock entry.
func lockedInsecureRef(d *lockfile.LockedDep) *manifest.DependencyReference {
	return &manifest.DependencyReference{RepoURL: d.RepoURL, Host: d.Host, Port: d.Port, VirtualPath: d.VirtualPath,
		Reference: d.ResolvedRef, Scheme: "http", AllowInsecure: d.AllowInsecure}
}

// lockedDisplayName is get_display_name of a lock entry.
func lockedDisplayName(d *lockfile.LockedDep) string {
	switch {
	case d.Source == "local" && d.LocalPath != "":
		return d.LocalPath
	case d.Alias != "":
		return d.Alias
	case d.VirtualPath != "":
		return virtualPackageName(d.RepoURL, d.VirtualPath)
	}
	return d.RepoURL
}
