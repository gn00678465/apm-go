package manifest

import (
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v4"
)

func TestParseDepString_Shorthand(t *testing.T) {
	tests := []struct {
		input     string
		wantOwner string
		wantRepo  string
		wantHost  string
		wantRef   string
		wantVP    string
		wantVT    string
	}{
		{"owner/repo", "owner", "repo", "github.com", "", "", ""},
		{"owner/repo#v1.0.0", "owner", "repo", "github.com", "v1.0.0", "", ""},
		{"owner/repo#^1.0.0", "owner", "repo", "github.com", "^1.0.0", "", ""},
		{"github.com/owner/repo", "owner", "repo", "github.com", "", "", ""},
		{"gitlab.com/owner/repo#main", "owner", "repo", "gitlab.com", "main", "", ""},
		{"gitlab.com/owner/repo/skills/my-skill", "owner/repo/skills", "my-skill", "gitlab.com", "", "", ""},
		{"owner/repo/prompts/review.prompt.md", "owner", "repo", "github.com", "", "prompts/review.prompt.md", "file"},
		{"owner/repo/instructions/demo.instructions.md", "owner", "repo", "github.com", "", "instructions/demo.instructions.md", "file"},
		{"owner/repo/agents/helper.agent.md", "owner", "repo", "github.com", "", "agents/helper.agent.md", "file"},
		{"owner/repo/sub/dir", "owner", "repo", "github.com", "", "sub/dir", "subdirectory"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDepString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", d.Owner, tt.wantOwner)
			}
			if d.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", d.Repo, tt.wantRepo)
			}
			if d.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", d.Host, tt.wantHost)
			}
			if d.Reference != tt.wantRef {
				t.Errorf("Reference = %q, want %q", d.Reference, tt.wantRef)
			}
			if d.VirtualPath != tt.wantVP {
				t.Errorf("VirtualPath = %q, want %q", d.VirtualPath, tt.wantVP)
			}
			if tt.wantVT != "" && d.VirtualType != tt.wantVT {
				t.Errorf("VirtualType = %q, want %q", d.VirtualType, tt.wantVT)
			}
		})
	}
}

func TestParseDepString_URLForm(t *testing.T) {
	tests := []struct {
		input      string
		wantScheme string
		wantHost   string
		wantOwner  string
		wantRepo   string
		wantPort   int
		wantRef    string
	}{
		{"https://gitlab.com/acme/repo.git", "https", "gitlab.com", "acme", "repo", 0, ""},
		{"https://gitlab.com/acme/repo.git#v2.0", "https", "gitlab.com", "acme", "repo", 0, "v2.0"},
		{"http://internal.example.com/team/project", "http", "internal.example.com", "team", "project", 0, ""},
		{"ssh://git@host:7999/owner/repo.git", "ssh", "host", "owner", "repo", 7999, ""},
		{"ssh://git@gitlab.com/acme/tools.git#main", "ssh", "gitlab.com", "acme", "tools", 0, "main"},
		{"git@gitlab.com:acme/repo.git", "git", "gitlab.com", "acme", "repo", 0, ""},
		{"git@github.com:owner/repo.git#v1.0.0", "git", "github.com", "owner", "repo", 0, "v1.0.0"},
		{"ssh://git@host:7999/owner/repo/skills/foo#v1", "ssh", "host", "owner", "repo", 7999, "v1"},
		{"git@gitlab.com:acme/repo/sub/path.git", "git", "gitlab.com", "acme", "repo", 0, ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDepString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Scheme != tt.wantScheme {
				t.Errorf("Scheme = %q, want %q", d.Scheme, tt.wantScheme)
			}
			if d.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", d.Host, tt.wantHost)
			}
			if d.Owner != tt.wantOwner {
				t.Errorf("Owner = %q, want %q", d.Owner, tt.wantOwner)
			}
			if d.Repo != tt.wantRepo {
				t.Errorf("Repo = %q, want %q", d.Repo, tt.wantRepo)
			}
			if d.Port != tt.wantPort {
				t.Errorf("Port = %d, want %d", d.Port, tt.wantPort)
			}
			if d.Reference != tt.wantRef {
				t.Errorf("Reference = %q, want %q", d.Reference, tt.wantRef)
			}
		})
	}
}

func TestParseDepString_Ticket16SSHRows(t *testing.T) {
	accepted := []struct {
		input, scheme, host, alias string
	}{
		{"ssh://host.io/owner/repo@alias", "ssh", "host.io", "alias"},
		{"git@host.io:owner/repo@alias", "git", "host.io", "alias"},
		{"ssh://host!bang/owner/repo", "ssh", "host!bang", ""},
		{"ssh://host_name/owner/repo", "ssh", "host_name", ""},
		{"ssh://host%20name/owner/repo", "ssh", "host%20name", ""},
	}
	for _, tt := range accepted {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDepString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Scheme != tt.scheme || d.Host != tt.host || d.Owner != "owner" || d.Repo != "repo" || d.Alias != tt.alias {
				t.Errorf("parsed reference = %+v, want scheme=%q host=%q owner/repo and alias=%q", d, tt.scheme, tt.host, tt.alias)
			}
		})
	}

	_, err := ParseDepString("ssh://alice:p%25@host.io/owner/repo")
	const wantError = "Percent-encoded characters are not allowed in SSH userinfo. Use the literal username (e.g. 'ssh://myuser@host/...')."
	if err == nil {
		t.Fatal("expected the Oracle's percent-encoded SSH userinfo rejection")
	} else if err.Error() != wantError {
		t.Errorf("error = %q, want %q", err, wantError)
	}
}

// TestParseDepString_Ticket16BacklogRound2 locks the six verifier-brief
// divergences against the pinned Oracle: SCP port diagnostics, retired bare
// aliases, the three-file virtual whitelist, Artifactory route prefixes,
// Azure DevOps path boundaries, and nested GitLab/generic-host paths.
func TestParseDepString_Ticket16BacklogRound2(t *testing.T) {
	t.Run("accepted fields", func(t *testing.T) {
		tests := []struct {
			input, host, repoURL, owner, repo, reference, alias, virtualPath, virtualType, artPrefix string
			port                                                                                     int
		}{
			{"git@host.io:0/owner/repo", "host.io", "0/owner/repo", "0", "owner", "", "", "", "", "", 0},
			{"git@host.io:65536/owner/repo", "host.io", "65536/owner/repo", "65536", "owner", "", "", "", "", "", 0},
			{"git@host.io:abc/owner/repo", "host.io", "abc/owner/repo", "abc", "owner", "", "", "", "", "", 0},
			{"art.corp/artifactory/github/owner/repo", "art.corp", "owner/repo", "owner", "repo", "", "", "", "", "artifactory/github", 0},
			{"art.corp/artifactory/github/owner/repo/sub", "art.corp", "owner/repo", "owner", "repo", "", "", "sub", "subdirectory", "artifactory/github", 0},
			{"Art.corp/Artifactory/GitHub/owner/repo", "Art.corp", "owner/repo", "owner", "repo", "", "", "", "", "artifactory/GitHub", 0},
			{"art.corp/artifactory/github/owner", "art.corp", "artifactory/github/owner", "artifactory/github", "owner", "", "", "", "", "", 0},
			{"https://art.corp/artifactory/github/owner/repo", "art.corp", "owner/repo", "owner", "repo", "", "", "", "", "artifactory/github", 0},
			{"https://art.corp/artifactory/github/owner/repo/sub", "art.corp", "owner/repo/sub", "owner/repo", "sub", "", "", "", "", "artifactory/github", 0},
			{"dev.azure.com/org/project/_git/repo", "dev.azure.com", "org/project/repo", "org/project", "repo", "", "", "", "", "", 0},
			{"dev.azure.com/org/project/repo", "dev.azure.com", "org/project/repo", "org/project", "repo", "", "", "", "", "", 0},
			{"dev.azure.com/org/project/_git/repo/sub/path", "dev.azure.com", "org/project/repo", "org/project", "repo", "", "", "sub/path", "subdirectory", "", 0},
			{"dev.azure.com/org/project/_git/repo/x.prompt.md", "dev.azure.com", "org/project/repo", "org/project", "repo", "", "", "x.prompt.md", "file", "", 0},
			{"myorg.visualstudio.com/project/_git/repo", "myorg.visualstudio.com", "myorg/project/repo", "myorg/project", "repo", "", "", "", "", "", 0},
			{"myorg.visualstudio.com/project/repo", "myorg.visualstudio.com", "myorg/project/repo", "myorg/project", "repo", "", "", "", "", "", 0},
			{"https://dev.azure.com/org/project/_git/repo/sub/path", "dev.azure.com", "org/project/repo", "org/project", "repo", "", "", "sub/path", "subdirectory", "", 0},
			{"https://myorg.visualstudio.com/project/_git/repo", "myorg.visualstudio.com", "myorg/project/repo", "myorg/project", "repo", "", "", "", "", "", 0},
			{"gitlab.com/group/repo", "gitlab.com", "group/repo", "group", "repo", "", "", "", "", "", 0},
			{"gitlab.com/group/subgroup/repo", "gitlab.com", "group/subgroup/repo", "group/subgroup", "repo", "", "", "", "", "", 0},
			{"gitlab.com/group/subgroup/deep/repo", "gitlab.com", "group/subgroup/deep/repo", "group/subgroup/deep", "repo", "", "", "", "", "", 0},
			{"gitlab.com/group/repo/prompts/x.prompt.md", "gitlab.com", "group/repo", "group", "repo", "", "", "prompts/x.prompt.md", "file", "", 0},
			{"gitlab.com/group/subgroup/repo/prompts/x.prompt.md", "gitlab.com", "group/subgroup/repo", "group/subgroup", "repo", "", "", "prompts/x.prompt.md", "file", "", 0},
			{"gitlab.com/group/subgroup/deep/repo/prompts/x.prompt.md", "gitlab.com", "group/subgroup/deep/repo", "group/subgroup/deep", "repo", "", "", "prompts/x.prompt.md", "file", "", 0},
			{"x.io/group/subgroup/repo", "x.io", "group/subgroup/repo", "group/subgroup", "repo", "", "", "", "", "", 0},
			{"x.io/group/subgroup/repo/prompts/x.prompt.md", "x.io", "group/subgroup", "group", "subgroup", "", "", "repo/prompts/x.prompt.md", "file", "", 0},
		}
		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				got, err := ParseDepString(tt.input)
				if err != nil {
					t.Fatalf("ParseDepString(%q): %v", tt.input, err)
				}
				if got.Host != tt.host || got.RepoURL != tt.repoURL || got.Owner != tt.owner || got.Repo != tt.repo || got.Reference != tt.reference || got.Alias != tt.alias || got.VirtualPath != tt.virtualPath || got.VirtualType != tt.virtualType || got.ArtifactoryPrefix != tt.artPrefix || got.Port != tt.port {
					t.Errorf("parsed reference = %+v, want host=%q repo_url=%q owner=%q repo=%q ref=%q alias=%q virtual=%q/%q artifactory=%q port=%d", got, tt.host, tt.repoURL, tt.owner, tt.repo, tt.reference, tt.alias, tt.virtualPath, tt.virtualType, tt.artPrefix, tt.port)
				}
			})
		}
	})

	t.Run("exact rejection messages", func(t *testing.T) {
		tests := []struct {
			input, want string
		}{
			{"git@host.io:2222/owner/repo", "It looks like '2222' in 'git@host.io:2222/owner/repo' is a port number, but SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form instead:\n  ssh://git@host.io:2222/owner/repo"},
			{"git@host.io:2222", "It looks like '2222' in 'git@host.io:2222' is a port number, but no repository path follows it. SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form: ssh://git@host.io:2222/<owner>/<repo>.git"},
			{"git@host.io:2222/owner/repo.git", "It looks like '2222' in 'git@host.io:2222/owner/repo' is a port number, but SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form instead:\n  ssh://git@host.io:2222/owner/repo.git"},
			{"git@host.io:2222/owner/repo#main", "It looks like '2222' in 'git@host.io:2222/owner/repo' is a port number, but SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form instead:\n  ssh://git@host.io:2222/owner/repo#main"},
			{"git@host.io:2222/owner/repo@alias", "It looks like '2222' in 'git@host.io:2222/owner/repo' is a port number, but SCP-style URLs (<user>@host:path) cannot carry a port. Use the ssh:// URL form instead:\n  ssh://git@host.io:2222/owner/repo@alias"},
			{"owner/repo@alias", "Shorthand '@alias' is not supported in 'owner/repo@alias'. Use object form with 'git:', optional 'path:', and 'alias:' fields to install a dependency under a custom directory name. See: https://microsoft.github.io/apm/consumer/manage-dependencies/#reference-formats"},
			{"owner/repo#v1@alias", "Shorthand '@alias' is not supported in 'owner/repo#v1@alias'. Use object form with 'git:', optional 'path:', and 'alias:' fields to install a dependency under a custom directory name. See: https://microsoft.github.io/apm/consumer/manage-dependencies/#reference-formats"},
			{"owner/repo#package@v1.0.1-rc.1+build", "Shorthand '@alias' is not supported in 'owner/repo#package@v1.0.1-rc.1+build'. Use object form with 'git:', optional 'path:', and 'alias:' fields to install a dependency under a custom directory name. See: https://microsoft.github.io/apm/consumer/manage-dependencies/#reference-formats"},
			{"owner/repo#package@notaversion", "Shorthand '@alias' is not supported in 'owner/repo#package@notaversion'. Use object form with 'git:', optional 'path:', and 'alias:' fields to install a dependency under a custom directory name. See: https://microsoft.github.io/apm/consumer/manage-dependencies/#reference-formats"},
			{"owner/repo/prompts/x.chatmode.md", "Invalid virtual package path 'prompts/x.chatmode.md'. Individual files must end with one of: .prompt.md, .instructions.md, .agent.md. For subdirectory packages, the path should not have a file extension."},
			{"owner/repo/prompts/x.md", "Invalid virtual package path 'prompts/x.md'. Individual files must end with one of: .prompt.md, .instructions.md, .agent.md. For subdirectory packages, the path should not have a file extension."},
			{"owner/repo/prompts/x.collection.yml", ".collection.yml is no longer supported. Convert 'prompts/x.collection.yml' to an apm.yml with a 'dependencies' section. See: https://microsoft.github.io/apm/guides/dependencies/"},
		}
		for _, tt := range tests {
			t.Run(tt.input, func(t *testing.T) {
				_, err := ParseDepString(tt.input)
				if err == nil || err.Error() != tt.want {
					t.Errorf("error = %q (len=%d), want %q (len=%d)", errorString(err), len(errorString(err)), tt.want, len(tt.want))
				}
			})
		}
	})
}

func TestParseDepString_LocalPath(t *testing.T) {
	tests := []struct {
		input     string
		wantLocal string
	}{
		{"./packages/local", "./packages/local"},
		{"./foo/bar", "./foo/bar"},
		{"~/my-skills", "~/my-skills"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDepString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !d.IsLocal {
				t.Error("expected IsLocal=true")
			}
			if d.LocalPath != tt.wantLocal {
				t.Errorf("LocalPath = %q, want %q", d.LocalPath, tt.wantLocal)
			}
		})
	}
}

func TestParseDepString_Rejection(t *testing.T) {
	tests := []struct {
		input string
		errSS string
	}{
		{"", "empty"},
		{"not valid string", "does not match"},
		{"../../../etc/passwd", "escapes project root"},
		{"just-one-word", "does not match"},
		{"https://", "requires host"},
		// mkt-033 negative test (a): apm.yml never accepts the CLI's
		// PLUGIN@MARKETPLACE shorthand as a dependencies.apm string -- only
		// the dict form ({name, marketplace, version}) is supported there.
		{"pkg@mkt", "Shorthand '@alias' is not supported"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			_, err := ParseDepString(tt.input)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errSS) {
				t.Errorf("error %q should contain %q", err.Error(), tt.errSS)
			}
		})
	}
}

// TestParseDepString_AbsolutePath covers this task's approved design point
// 3: an OS-absolute local filesystem path is now ACCEPTED as a local
// dependency (IsLocal=true, Source="local"), reversing the previous blanket
// "dependency path %q is absolute; only relative paths are allowed"
// rejection -- inverted from what TestParseDepString_Rejection asserted
// before this fix ("/etc/passwd", "/absolute/path", "/tmp/malicious" all
// used to error with "absolute"). This is what lets (a) a plain
// `apm install /abs/path` local git dependency parse at all, and (b)
// mkt-025's local-marketplace fast path round-trip an out-of-project-tree
// absolute canonical back through apm.yml. containsEscape must NOT run on
// an explicitly-absolute path (it is user-intended, not a traversal
// attempt) -- exercised here with a path that would itself look like it
// "escapes" if the relative-path escape check ran on it.
func TestParseDepString_AbsolutePath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		winOnly bool
	}{
		{"posix absolute", "/etc/passwd", false},
		{"posix absolute nested", "/absolute/path", false},
		{"posix absolute tmp", "/tmp/malicious", false},
		{"windows drive letter backslash", `C:\Users\me\plugins\p`, true},
		{"windows drive letter forward-slash", "C:/Users/me/plugins/p", true},
		{"windows UNC", `\\myserver\share\plugin`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// windows drive-letter absolute-path detection (ParseDepString's
			// use of filepath.IsAbs) only matches on GOOS=windows -- ticket
			// 09 added this guard so `go test ./...` passes cleanly in CI on
			// a Linux runner instead of always showing this one pre-existing,
			// documented (AGENTS.md) platform gap as a failure.
			if tt.winOnly && runtime.GOOS != "windows" {
				t.Skip("drive-letter absolute-path detection only passes on windows")
			}
			d, err := ParseDepString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !d.IsLocal {
				t.Error("expected IsLocal=true")
			}
			if d.Source != "local" {
				t.Errorf("Source = %q, want local", d.Source)
			}
			if d.LocalPath != tt.input {
				t.Errorf("LocalPath = %q, want %q", d.LocalPath, tt.input)
			}
		})
	}
}

// TestParseDepString_AbsolutePathSkipsEscapeGuard covers the design's
// explicit carve-out: an absolute path containing a literal ".." segment is
// still accepted (never routed through containsEscape), since an explicitly
// absolute path is user-intended and the RELATIVE-path escape guard only
// makes sense for a path meant to stay inside the project root.
func TestParseDepString_AbsolutePathSkipsEscapeGuard(t *testing.T) {
	d, err := ParseDepString("/abs/foo/../bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !d.IsLocal || d.LocalPath != "/abs/foo/../bar" {
		t.Errorf("d = %+v, want IsLocal=true LocalPath=/abs/foo/../bar", d)
	}
}

// TestParseDepString_PercentEncodedShorthand is the regression for upstream
// commit 645a5a53: reference.py:1774-1779 no longer decodes the whole
// dependency string before shorthand parsing. Encoded shorthand segments
// therefore remain presentation text and fail the ordinary shorthand
// character grammar, while malformed percent escapes remain ordinary invalid
// characters.
func TestParseDepString_PercentEncodedShorthand(t *testing.T) {
	for _, input := range []string{"owner/%72epo", "owner/%zzrepo"} {
		t.Run(input, func(t *testing.T) {
			_, err := ParseDepString(input)
			if err == nil {
				t.Fatal("expected encoded shorthand to be rejected")
			}
			if !strings.Contains(err.Error(), "Invalid repository path component") {
				t.Errorf("error = %q, want invalid repository path component", err)
			}
		})
	}
}

// TestParseDepString_PercentEncodedTraversal is the regression for the
// strict shorthand path validation introduced by upstream commit 645a5a53.
// The encoded traversal marker must be rejected before character matching,
// with the Oracle's path_security.py:123-173 diagnostic preserved.
func TestParseDepString_PercentEncodedTraversal(t *testing.T) {
	_, err := ParseDepString("%2e%2e/%2e%2e/etc/passwd")
	if err == nil {
		t.Fatal("expected an error for a percent-encoded traversal, got nil")
	}
	want := "Invalid repository path '%2e%2e/%2e%2e': segment '%2e%2e' is a traversal sequence"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

func TestParseURLPathSegments_EmptyPath(t *testing.T) {
	_, _, err := parseURLPathSegments("/", "repository URL path")
	if got, want := errorString(err), "Invalid repository URL path: path segments must not be empty"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestValidateVirtualPath_Traversal(t *testing.T) {
	err := validateVirtualPath("../secret")
	if got, want := errorString(err), "Invalid virtual path '../secret': segment '..' is a traversal sequence"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

// TestParseDepString_FQDNHostGate is ticket 11 eval attempt 4's reproducer
// 2, probed directly against the pinned Oracle: the HTTPS/HTTP URL form and
// shorthand's host-qualified form ("host.tld/owner/repo") both reject a
// non-FQDN host (github_host.py:1074-1102's is_valid_fqdn, via
// is_supported_git_host), while ssh:// and SCP (git@host:...) do NOT --
// probed directly, both accept a bare non-dotted host verbatim (see
// isValidFQDN's doc comment on manifest.ParseDepString for the full
// evidence). TestParseDepString_URLForm's existing
// "ssh://git@host:7999/..." cases already lock down the ungated forms.
func TestParseDepString_FQDNHostGate(t *testing.T) {
	rejected := []string{
		"https://x/owner/repo", // no dot at all
		"-x.io/owner/repo",     // label starts with a hyphen
		"x-.io/owner/repo",     // label ends with a hyphen
		"x..io/owner/repo",     // empty label (doubled dot)
	}
	for _, in := range rejected {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseDepString(in); err == nil {
				t.Error("expected an error for a non-FQDN host, got nil")
			}
		})
	}

	accepted := []struct {
		input    string
		wantHost string
	}{
		{"https://x.io/owner/repo", "x.io"},
		{"github.com/owner/repo", "github.com"},
	}
	for _, tt := range accepted {
		t.Run(tt.input, func(t *testing.T) {
			d, err := ParseDepString(tt.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.Host != tt.wantHost {
				t.Errorf("Host = %q, want %q", d.Host, tt.wantHost)
			}
		})
	}
}

func TestParseDepDict_GitParent(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		entry := buildMappingNode(map[string]string{
			"git":  "parent",
			"path": "prompts/review.prompt.md",
		})
		d, err := ParseDepDict(entry, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !d.IsParent {
			t.Error("expected IsParent=true")
		}
		if d.VirtualType != "file" {
			t.Errorf("VirtualType = %q, want file", d.VirtualType)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		entry := buildMappingNode(map[string]string{"git": "parent"})
		_, err := ParseDepDict(entry, 0)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "path") {
			t.Errorf("error should mention path: %v", err)
		}
	})

	t.Run("type forbidden", func(t *testing.T) {
		entry := buildMappingNode(map[string]string{
			"git":  "parent",
			"path": "skills/foo",
			"type": "gitlab",
		})
		_, err := ParseDepDict(entry, 0)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "type") {
			t.Errorf("error should mention type: %v", err)
		}
	})
}

func TestParseDepDict_BothIdGit(t *testing.T) {
	entry := buildMappingNode(map[string]string{
		"id":  "acme/foo",
		"git": "acme/foo",
	})
	_, err := ParseDepDict(entry, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "id") || !strings.Contains(err.Error(), "git") {
		t.Errorf("error should mention id and git: %v", err)
	}
}

func TestParseDepDict_NoSourceKey(t *testing.T) {
	entry := buildMappingNode(map[string]string{
		"alias": "foo",
		"ref":   "main",
	})
	_, err := ParseDepDict(entry, 0)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source") {
		t.Errorf("error should mention source: %v", err)
	}
}

func TestToCanonical(t *testing.T) {
	tests := []struct {
		name        string
		dep         DependencyReference
		defaultHost string
		want        string
	}{
		{
			"default host stripped",
			DependencyReference{Host: "github.com", Owner: "owner", Repo: "repo"},
			"github.com",
			"owner/repo",
		},
		{
			"non-default host kept",
			DependencyReference{Host: "gitlab.com", Owner: "owner", Repo: "repo"},
			"github.com",
			"gitlab.com/owner/repo",
		},
		{
			"no host",
			DependencyReference{Owner: "owner", Repo: "repo"},
			"github.com",
			"owner/repo",
		},
		{
			"with ref",
			DependencyReference{Owner: "owner", Repo: "repo", Reference: "v1.0.0"},
			"github.com",
			"owner/repo#v1.0.0",
		},
		{
			"with virtual path",
			DependencyReference{Owner: "owner", Repo: "repo", VirtualPath: "skills/foo"},
			"github.com",
			"owner/repo/skills/foo",
		},
		{
			"strip .git",
			DependencyReference{Host: "github.com", Owner: "owner", Repo: "repo.git"},
			"github.com",
			"owner/repo",
		},
		{
			"local path",
			DependencyReference{IsLocal: true, LocalPath: "./packages/foo"},
			"github.com",
			"./packages/foo",
		},
		{
			"parent",
			DependencyReference{IsParent: true},
			"github.com",
			"parent",
		},
		{
			"case insensitive host match",
			DependencyReference{Host: "GitHub.com", Owner: "owner", Repo: "repo"},
			"github.com",
			"owner/repo",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.dep.ToCanonical(tt.defaultHost)
			if got != tt.want {
				t.Errorf("ToCanonical = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestClassifyVirtualPath(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"prompts/review.prompt.md", "file"},
		{"instructions/demo.instructions.md", "file"},
		{"agents/helper.agent.md", "file"},
		{"modes/pair.chatmode.md", "subdirectory"},
		{"skills/my-skill", "subdirectory"},
		{"some/other/path", "subdirectory"},
		{"file.md", "subdirectory"},
		{"prompts", "subdirectory"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := classifyVirtualPath(tt.path)
			if got != tt.want {
				t.Errorf("classifyVirtualPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// helper to build yaml mapping nodes for tests
func buildMappingNode(kv map[string]string) *yaml.Node {
	n := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	for k, v := range kv {
		n.Content = append(n.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v},
		)
	}
	return n
}

// TestParseDepDict_NameBranch_RejectsExtTransport is the parse-layer half of
// the git-ext RCE fix: a bare {name: ...} entry must be validated as a
// git-repo shorthand, so an "ext::..." remote-helper string is rejected
// instead of stored verbatim and later handed to `git clone`.
func TestParseDepDict_NameBranch_RejectsExtTransport(t *testing.T) {
	bad := []string{
		"ext::sh -c 'id'",
		"fd::17/foo",
		"-oProxyCommand=id",
		"not a repo",
	}
	for _, name := range bad {
		entry := buildMappingNode(map[string]string{"name": name})
		if _, err := ParseDepDict(entry, 0); err == nil {
			t.Errorf("ParseDepDict{name: %q} = nil error, want rejection", name)
		}
	}
}

// TestParseDepDict_NameBranch_AcceptsShorthand confirms a legitimate
// owner/repo name still parses (now as a proper git ref with Owner/Repo set,
// so resolveCloneURL builds a real https URL rather than cloning the raw
// string).
func TestParseDepDict_NameBranch_AcceptsShorthand(t *testing.T) {
	entry := buildMappingNode(map[string]string{"name": "owner/repo"})
	ref, err := ParseDepDict(entry, 0)
	if err != nil {
		t.Fatalf("ParseDepDict{name: owner/repo}: %v", err)
	}
	if ref.Owner != "owner" || ref.Repo != "repo" || ref.Source != "git" {
		t.Errorf("got Owner=%q Repo=%q Source=%q, want owner/repo/git", ref.Owner, ref.Repo, ref.Source)
	}
}

// TestParseDepDict_GitBranch_LocalShapeAndFileSchemeRejected locks two facts
// the resolver's transitive-local supply-chain guard (HIGH-B) depends on:
//  1. `git: <localpath>` parses to exactly the shape resolveCloneURL and the
//     guard treat as local -- Source=git, Owner=="" && Repo=="", RepoURL==path,
//     IsLocal=false.
//  2. `git: file://...` is rejected at parse, so a file-transport URL can never
//     reach the resolver or the git clone in the first place.
func TestParseDepDict_GitBranch_LocalShapeAndFileSchemeRejected(t *testing.T) {
	for _, p := range []string{"/abs/repo", "./rel/repo"} {
		ref, err := ParseDepDict(buildMappingNode(map[string]string{"git": p}), 0)
		if err != nil {
			t.Fatalf("ParseDepDict{git: %q}: %v", p, err)
		}
		if ref.Source != "git" || ref.Owner != "" || ref.Repo != "" || ref.RepoURL != p || ref.IsLocal {
			t.Errorf("git: %q -> Source=%q Owner=%q Repo=%q RepoURL=%q IsLocal=%v; want git/\"\"/\"\"/%q/false",
				p, ref.Source, ref.Owner, ref.Repo, ref.RepoURL, ref.IsLocal, p)
		}
	}
	for _, bad := range []string{"file:///abs/repo", "file://host/repo"} {
		if _, err := ParseDepDict(buildMappingNode(map[string]string{"git": bad}), 0); err == nil {
			t.Errorf("ParseDepDict{git: %q} = nil error, want rejection (file:// must not be parseable)", bad)
		}
	}
}
