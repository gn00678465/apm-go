package manifest

import (
	"strings"
	"testing"
)

// TestCheckInsecureDependencyScheme_HTTPRefusedByDefault is the RED case for
// the P0 gate this file adds: a non-TLS http:// git dependency must be
// refused unless --allow-insecure was passed, and the error must name the
// offending dependency and point at the remediation.
func TestCheckInsecureDependencyScheme_HTTPRefusedByDefault(t *testing.T) {
	dep := &DependencyReference{
		Scheme: "http",
		Host:   "example.com",
		Owner:  "owner",
		Repo:   "repo",
		Source: "git",
	}
	err := CheckInsecureDependencyScheme(dep, false, "github.com")
	if err == nil {
		t.Fatal("expected error for http:// dependency without --allow-insecure, got nil")
	}
	if !strings.Contains(err.Error(), "http://example.com/owner/repo") {
		t.Errorf("error should name the offending dependency, got: %v", err)
	}
	if !strings.Contains(err.Error(), "--allow-insecure") {
		t.Errorf("error should point at the --allow-insecure remediation, got: %v", err)
	}
}

// TestCheckInsecureDependencyScheme_AllHostsRefusedWithoutFlag mirrors the
// Python reference implementation's _check_insecure_dependencies exactly: the
// gate is flag-only, with NO host exemption -- loopback, RFC1918-private, and
// public hosts alike are all refused without --allow-insecure.
func TestCheckInsecureDependencyScheme_AllHostsRefusedWithoutFlag(t *testing.T) {
	hosts := []string{
		// loopback / local
		"127.0.0.1",
		"127.5.5.5",
		"::1",
		"localhost",
		// RFC1918 private
		"10.0.0.5",
		"172.16.0.5",
		"192.168.1.10",
		// public
		"203.0.113.5",
		"example.com",
		"8.8.8.8",
	}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			dep := &DependencyReference{
				Scheme: "http",
				Host:   host,
				Owner:  "owner",
				Repo:   "repo",
				Source: "git",
			}
			if err := CheckInsecureDependencyScheme(dep, false, "github.com"); err == nil {
				t.Errorf("expected host %q to be refused without --allow-insecure (no host exemption, Python parity)", host)
			}
		})
	}
}

// TestCheckInsecureDependencyScheme_AllowInsecurePermits proves
// --allow-insecure permits an otherwise-refused http:// dependency for every
// host class -- public, loopback, and RFC1918-private alike.
func TestCheckInsecureDependencyScheme_AllowInsecurePermits(t *testing.T) {
	hosts := []string{"example.com", "127.0.0.1", "localhost", "192.168.1.10"}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			dep := &DependencyReference{
				Scheme: "http",
				Host:   host,
				Owner:  "owner",
				Repo:   "repo",
				Source: "git",
			}
			if err := CheckInsecureDependencyScheme(dep, true, "github.com"); err != nil {
				t.Errorf("expected --allow-insecure to permit http:// dependency to %q, got: %v", host, err)
			}
		})
	}
}

// TestCheckInsecureDependencyScheme_NonHTTPSchemesUnaffected proves https/ssh
// (and a nil dep) are never touched by this gate.
func TestCheckInsecureDependencyScheme_NonHTTPSchemesUnaffected(t *testing.T) {
	if err := CheckInsecureDependencyScheme(nil, false, "github.com"); err != nil {
		t.Errorf("nil dep should never error, got: %v", err)
	}
	for _, scheme := range []string{"https", "ssh", "git", ""} {
		dep := &DependencyReference{Scheme: scheme, Host: "example.com", Owner: "owner", Repo: "repo", Source: "git"}
		if err := CheckInsecureDependencyScheme(dep, false, "github.com"); err != nil {
			t.Errorf("scheme %q should never be refused by the http-only gate, got: %v", scheme, err)
		}
	}
}

// TestCheckInsecureDependencyScheme_DisplayIncludesVirtualPathAndRef proves
// the reconstructed display URL includes the virtual path and ref when
// present, matching the dependency the user actually declared.
func TestCheckInsecureDependencyScheme_DisplayIncludesVirtualPathAndRef(t *testing.T) {
	dep := &DependencyReference{
		Scheme:      "http",
		Host:        "example.com",
		Owner:       "owner",
		Repo:        "repo",
		VirtualPath: "sub/pkg",
		Reference:   "main",
		Source:      "git",
	}
	err := CheckInsecureDependencyScheme(dep, false, "github.com")
	if err == nil {
		t.Fatal("expected error for http:// dependency without --allow-insecure")
	}
	want := "http://example.com/owner/repo/sub/pkg#main"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error should include virtual path and ref (%q), got: %v", want, err)
	}
}

// TestCheckInsecureDependencyScheme_MissingHostUsesDefaultHost proves a dep
// with no explicit Host (e.g. shorthand parsed against DefaultHost) is still
// refused and the error reconstructs the URL from the manifest's default
// host so the message names an actual location.
func TestCheckInsecureDependencyScheme_MissingHostUsesDefaultHost(t *testing.T) {
	dep := &DependencyReference{Scheme: "http", Owner: "owner", Repo: "repo", Source: "git"}
	err := CheckInsecureDependencyScheme(dep, false, "github.com")
	if err == nil {
		t.Fatal("expected error: empty Host is refused like any other http:// dependency")
	}
	if !strings.Contains(err.Error(), "http://github.com/owner/repo") {
		t.Errorf("error should reconstruct the URL using defaultHost, got: %v", err)
	}
}
