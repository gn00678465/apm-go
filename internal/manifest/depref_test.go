package manifest

import (
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
		{"owner/repo", "owner", "repo", "", "", "", ""},
		{"owner/repo#v1.0.0", "owner", "repo", "", "v1.0.0", "", ""},
		{"owner/repo#^1.0.0", "owner", "repo", "", "^1.0.0", "", ""},
		{"github.com/owner/repo", "owner", "repo", "github.com", "", "", ""},
		{"gitlab.com/owner/repo#main", "owner", "repo", "gitlab.com", "main", "", ""},
		{"gitlab.com/owner/repo/skills/my-skill", "owner", "repo", "gitlab.com", "", "skills/my-skill", "subdirectory"},
		{"owner/repo/prompts/review.prompt.md", "owner", "repo", "", "", "prompts/review.prompt.md", "file"},
		{"owner/repo/instructions/demo.instructions.md", "owner", "repo", "", "", "instructions/demo.instructions.md", "file"},
		{"owner/repo/agents/helper.agent.md", "owner", "repo", "", "", "agents/helper.agent.md", "file"},
		{"owner/repo/modes/pair.chatmode.md", "owner", "repo", "", "", "modes/pair.chatmode.md", "file"},
		{"owner/repo/sub/dir", "owner", "repo", "", "", "sub/dir", "subdirectory"},
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
		{"/etc/passwd", "absolute"},
		{"/absolute/path", "absolute"},
		{"/tmp/malicious", "absolute"},
		{"just-one-word", "does not match"},
		{"https://", "requires host"},
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
		{"modes/pair.chatmode.md", "file"},
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
