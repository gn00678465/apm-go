package build

import (
	"encoding/json"
	"testing"

	"github.com/apm-go/apm/internal/marketplace/authoring"
)

func TestComposeRemoteSource_ClaudeStylesPreserveShapeAndKeyOrder(t *testing.T) {
	tests := []struct {
		name  string
		style ClaudeSourceStyle
		pkg   ResolvedPackage
		want  string
	}{
		{
			name:  "github plain shorthand",
			style: ClaudeSourceStyleGithub,
			pkg:   ResolvedPackage{SourceRepo: "alice/tool", Ref: "v1.2.3", SHA: "abc", EffectiveTagPattern: "v{version}"},
			want:  `{"source":"github","repo":"alice/tool","ref":"v1.2.3","sha":"abc","tag_pattern":"v{version}"}`,
		},
		{
			name:  "github plain url",
			style: ClaudeSourceStyleURL,
			pkg:   ResolvedPackage{SourceRepo: "alice/tool", Ref: "v1.2.3", SHA: "abc", EffectiveTagPattern: "v{version}"},
			want:  `{"source":"url","url":"https://github.com/alice/tool","ref":"v1.2.3","sha":"abc","tag_pattern":"v{version}"}`,
		},
		{
			name:  "github subdir shorthand",
			style: ClaudeSourceStyleGithub,
			pkg:   ResolvedPackage{SourceRepo: "alice/mono", Subdir: "plugins/tool", Ref: "v2.0.0", SHA: "def"},
			want:  `{"source":"git-subdir","url":"alice/mono","path":"plugins/tool","ref":"v2.0.0","sha":"def"}`,
		},
		{
			name:  "github subdir url",
			style: ClaudeSourceStyleURL,
			pkg:   ResolvedPackage{SourceRepo: "alice/mono", Subdir: "plugins/tool", Ref: "v2.0.0", SHA: "def"},
			want:  `{"source":"git-subdir","url":"https://github.com/alice/mono","path":"plugins/tool","ref":"v2.0.0","sha":"def"}`,
		},
		{
			name:  "non-default host plain shorthand style",
			style: ClaudeSourceStyleGithub,
			pkg:   ResolvedPackage{Host: "git.example.com", SourceRepo: "acme/tool", Ref: "v3.0.0"},
			want:  `{"source":"url","url":"https://git.example.com/acme/tool","ref":"v3.0.0"}`,
		},
		{
			name:  "non-default host plain url style",
			style: ClaudeSourceStyleURL,
			pkg:   ResolvedPackage{Host: "git.example.com", SourceRepo: "acme/tool", Ref: "v3.0.0"},
			want:  `{"source":"url","url":"https://git.example.com/acme/tool","ref":"v3.0.0"}`,
		},
		{
			name:  "non-default host subdir shorthand style",
			style: ClaudeSourceStyleGithub,
			pkg:   ResolvedPackage{Host: "git.example.com", SourceRepo: "acme/mono", Subdir: "plugins/tool", SHA: "ghi"},
			want:  `{"source":"git-subdir","url":"https://git.example.com/acme/mono","path":"plugins/tool","sha":"ghi"}`,
		},
		{
			name:  "non-default host subdir url style",
			style: ClaudeSourceStyleURL,
			pkg:   ResolvedPackage{Host: "git.example.com", SourceRepo: "acme/mono", Subdir: "plugins/tool", SHA: "ghi"},
			want:  `{"source":"git-subdir","url":"https://git.example.com/acme/mono","path":"plugins/tool","sha":"ghi"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(composeRemoteSource(tt.pkg, tt.style))
			if err != nil {
				t.Fatalf("json.Marshal() error = %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("JSON = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestClaudeMapper_ZeroSourceStyleKeepsGithubDefault(t *testing.T) {
	doc, _, err := ClaudeMapper{}.Compose(&authoring.AuthoringConfig{Name: "m"}, []ResolvedPackage{
		{SourceRepo: "alice/tool", Entry: authoring.PackageEntry{Name: "tool", Source: "alice/tool"}},
	})
	if err != nil {
		t.Fatalf("Compose() error = %v", err)
	}
	src, ok := doc.Plugins[0].Source.(*RemoteSource)
	if !ok {
		t.Fatalf("Source = %#v, want *RemoteSource", doc.Plugins[0].Source)
	}
	if src.Source != "github" || src.Repo != "alice/tool" {
		t.Errorf("source = %#v, want github/alice/tool", src)
	}
}

func TestClaudeMapper_UnknownSourceStyleReturnsError(t *testing.T) {
	_, _, err := (ClaudeMapper{SourceStyle: ClaudeSourceStyle("ssh")}).Compose(
		&authoring.AuthoringConfig{Name: "m"}, nil,
	)
	if err == nil {
		t.Fatal("unknown source style succeeded, want an error")
	}
}
