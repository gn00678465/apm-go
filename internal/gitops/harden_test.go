package gitops

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/manifest"
)

func TestValidateCloneURL(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"ext helper", "ext::sh -c 'id'", true},
		{"fd helper", "fd::17/foo", true},
		{"double colon anywhere", "https://h/o/r::x", true},
		{"leading dash option", "-oProxyCommand=id", true},
		{"plain https", "https://github.com/owner/repo.git", false},
		{"ssh", "ssh://git@github.com/owner/repo.git", false},
		{"scp", "git@github.com:owner/repo.git", false},
		{"local relative", "./vendor/pkg", false},
		{"local absolute posix", "/srv/pkg", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCloneURL(tt.url)
			if tt.wantErr && err == nil {
				t.Errorf("validateCloneURL(%q) = nil, want error", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Errorf("validateCloneURL(%q) = %v, want nil", tt.url, err)
			}
		})
	}
}

func TestIsLocalCloneURL(t *testing.T) {
	cases := []struct {
		url   string
		local bool
	}{
		// Local paths (may use the file transport).
		{"./upstream", true},
		{"../vendor/pkg", true},
		{"~/repos/pkg", true},
		{"/srv/git/pkg", true},
		{"remote", true},
		{"sub/remote", true},
		// Remote / network (must NEVER be classified local -> never get file).
		{"https://github.com/o/r.git", false},
		{"ssh://git@github.com/o/r.git", false},
		{"git@github.com:o/r.git", false},
		{"github.com:o/r", false},                    // SCP without user@
		{`\\evil\share\repo`, false},                 // UNC
		{"//evil/share/repo", false},                 // network path
		{`\/evil\share`, false},                      // mixed-slash UNC
		{`/\evil/share`, false},                      // mixed-slash UNC
		{`DOMAIN\user@evil.example:repo.git`, false}, // SCP with backslash before ':'
		{"ext::sh -c id", false},                     // helper (also rejected by validateCloneURL)
	}
	for _, tt := range cases {
		t.Run(tt.url, func(t *testing.T) {
			if got := isLocalCloneURL(tt.url); got != tt.local {
				t.Errorf("isLocalCloneURL(%q) = %v, want %v", tt.url, got, tt.local)
			}
		})
	}

	// A Windows drive path is local only on Windows; on other OSes "C:/x"
	// is not a drive path and stays classified remote (conservative -> no
	// file transport).
	wantDriveLocal := runtime.GOOS == "windows"
	for _, u := range []string{`C:\Users\me\pkg`, "C:/Users/me/pkg"} {
		if got := isLocalCloneURL(u); got != wantDriveLocal {
			t.Errorf("isLocalCloneURL(%q) = %v, want %v on %s", u, got, wantDriveLocal, runtime.GOOS)
		}
	}
}

func TestSecureGitEnv_RestrictsProtocols(t *testing.T) {
	env := SecureGitEnv()
	var got string
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_ALLOW_PROTOCOL=") {
			got = strings.TrimPrefix(kv, "GIT_ALLOW_PROTOCOL=")
		}
	}
	if got == "" {
		t.Fatal("SecureGitEnv() must set GIT_ALLOW_PROTOCOL")
	}
	// The allow-list must NOT include the ext remote-helper transport.
	for _, proto := range strings.Split(got, ":") {
		if proto == "ext" {
			t.Fatalf("GIT_ALLOW_PROTOCOL must not allow ext; got %q", got)
		}
	}
	if !strings.Contains(got, "https") || !strings.Contains(got, "ssh") {
		t.Errorf("GIT_ALLOW_PROTOCOL should permit https and ssh; got %q", got)
	}
}

// TestLoadPackage_RejectsExtTransportRCE is the definitive supply-chain-RCE
// regression: it hands LoadPackage a dependency whose RepoURL is an
// "ext::<command>" remote-helper spec (the shape the unvalidated {name: ...}
// manifest branch used to produce) DIRECTLY -- bypassing the parse-layer
// guard -- and asserts the lower defense layers (validateCloneURL +
// GIT_ALLOW_PROTOCOL) block it: LoadPackage must error, and the command
// embedded in the URL must NOT have run (no marker file created).
func TestLoadPackage_RejectsExtTransportRCE(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "PWNED")
	// A payload that, if git ran the ext helper, would create the marker.
	payload := "ext::sh -c 'touch " + marker + "'"

	r := &RealPackageLoader{ModulesDir: filepath.Join(dir, "apm_modules")}
	ref := &manifest.DependencyReference{RepoURL: payload, Source: "git"}

	_, err := r.LoadPackage(ref, "")
	if err == nil {
		t.Fatal("LoadPackage accepted an ext:: remote-helper URL; expected rejection")
	}
	if _, statErr := os.Stat(marker); statErr == nil {
		t.Fatalf("RCE: the ext:: helper command executed (marker %s was created)", marker)
	}
}
