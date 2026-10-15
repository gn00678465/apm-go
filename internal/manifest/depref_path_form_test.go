package manifest

import (
	"testing"

	"go.yaml.in/yaml/v4"
)

func pathEntry(t *testing.T, value string) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("- path: "+value+"\n"), &doc); err != nil {
		t.Fatalf("yaml %q: %v", value, err)
	}
	return doc.Content[0].Content[0]
}

// Every row's verdict, path and reason is what the Oracle (microsoft/apm
// 18c4c43) gave for the same value, by DependencyReference.parse_from_dict
// for the object form and DependencyReference.parse for the string form.
func TestParseDepDict_PathTakesOnlyLocalPaths(t *testing.T) {
	const (
		notLocal = "Object-style dependency must have a 'git' field, or 'path' must be a local filesystem path (starting with './', '../', '/', or '~')"
		empty    = "dependency entry 3: 'path' field must be a non-empty string"
	)

	accepted := []struct{ yaml, localPath string }{
		{"./dep", "./dep"},
		{"./sub/dep", "./sub/dep"},
		{"../dep", "../dep"},
		{"/abs/dep", "/abs/dep"},
		{"~/x", "~/x"},
		{"' ./dep'", "./dep"},
		{"'./dep '", "./dep"},
		{"' ./dep '", "./dep"},
		{`"\t./dep"`, "./dep"},
	}
	for _, tt := range accepted {
		t.Run(tt.yaml, func(t *testing.T) {
			d, err := ParseDepDict(pathEntry(t, tt.yaml), 3)
			if err != nil {
				t.Fatal(err)
			}
			if !d.IsLocal || d.Source != "local" || d.LocalPath != tt.localPath {
				t.Errorf("IsLocal %v, Source %q, LocalPath %q; want true, local, %q", d.IsLocal, d.Source, d.LocalPath, tt.localPath)
			}
		})
	}

	rejected := []struct{ yaml, want string }{
		{"dep", `dependency entry 3 (path: "dep"): ` + notLocal},
		{"sub/dep", `dependency entry 3 (path: "sub/dep"): ` + notLocal},
		{"//x", `dependency entry 3 (path: "//x"): ` + notLocal},
		{"' //x'", `dependency entry 3 (path: "//x"): ` + notLocal},
		{`\\host\share`, `dependency entry 3 (path: "\\\\host\\share"): ` + notLocal},
		{"''", empty},
		{"' '", empty},
		{"null", empty},
		{"5", empty},
		{"[a]", empty},
	}
	for _, tt := range rejected {
		t.Run(tt.yaml, func(t *testing.T) {
			_, err := ParseDepDict(pathEntry(t, tt.yaml), 3)
			if err == nil || err.Error() != tt.want {
				t.Errorf("error %v, want %s", err, tt.want)
			}
		})
	}
}

// install moves a string between the string form and the `path:` form, so
// the two have to read it as the same path or both refuse it. localPath is
// what the Oracle reads; "" is a value it refuses as a path in both forms.
func TestParseDepDict_PathAgreesWithStringForm(t *testing.T) {
	tests := []struct{ value, localPath string }{
		{"./dep", "./dep"},
		{"/abs/dep", "/abs/dep"},
		{"~/x", "~/x"},
		{`.\dep`, `.\dep`},
		{" ./dep", "./dep"},
		{"./dep ", "./dep"},
		{" ./dep ", "./dep"},
		{"//x", ""},
		{`\\host\share`, ""},
		{"dep", ""},
		{"sub/dep", ""},
		{"owner/repo", ""},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			str, strErr := ParseDepString(tt.value)
			obj, objErr := ParseDepDict(pathEntry(t, "'"+tt.value+"'"), 0)
			if tt.localPath == "" {
				if strErr == nil && str.IsLocal {
					t.Errorf("string form: local path %q, want not local", str.LocalPath)
				}
				if objErr == nil {
					t.Errorf("object form: accepted with LocalPath %q, want an error", obj.LocalPath)
				}
				return
			}
			if strErr != nil || !str.IsLocal || str.LocalPath != tt.localPath {
				t.Errorf("string form: %+v, error %v; want local path %q", str, strErr, tt.localPath)
			}
			if objErr != nil || !obj.IsLocal || obj.LocalPath != tt.localPath {
				t.Errorf("object form: %+v, error %v; want local path %q", obj, objErr, tt.localPath)
			}
		})
	}
}

// The string form's local-path step, with the Oracle's results for the same
// strings (DependencyReference.parse, models/dependency/reference.py:1806-1832).
func TestParseDepString_LocalPathFollowsOracle(t *testing.T) {
	const protocolRelative = "Protocol-relative URLs are not supported\n\n" +
		"Invalid Git host: '//...'.\n\n" +
		"APM supports any valid FQDN as a Git host, including:\n" +
		"  * github.com\n" +
		"  * *.ghe.com (GitHub Enterprise Cloud)\n" +
		"  * dev.azure.com, *.visualstudio.com (Azure DevOps)\n" +
		"  * gitlab.com, bitbucket.org, or any self-hosted Git server\n\n" +
		"To use '//...', set the appropriate environment variable:\n\n" +
		"  For GitHub Enterprise Server:\n" +
		"  # Linux/macOS:\n" +
		"  export GITHUB_HOST=//...\n\n" +
		"  # Windows (PowerShell):\n" +
		"  $env:GITHUB_HOST = \"//...\"\n\n" +
		"  # Windows (Command Prompt):\n" +
		"  set GITHUB_HOST=//...\n\n" +
		"  For on-prem Azure DevOps Server:\n" +
		"  # Linux/macOS:\n" +
		"  export ADO_HOST=//...\n\n" +
		"  # Windows (PowerShell):\n" +
		"  $env:ADO_HOST = \"//...\"\n\n" +
		"  # Windows (Command Prompt):\n" +
		"  set ADO_HOST=//...\n"
	const shorthand = "Use 'user/repo' or 'github.com/user/repo' or 'dev.azure.com/org/project/repo' format"

	for _, value := range []string{" ./dep", "./dep ", " ./dep "} {
		t.Run(value, func(t *testing.T) {
			d, err := ParseDepString(value)
			if err != nil || !d.IsLocal || d.LocalPath != "./dep" {
				t.Errorf("%+v, error %v; want local path ./dep", d, err)
			}
		})
	}

	rejected := []struct{ value, want string }{
		{"//x", protocolRelative},
		{"//tmp/dep", protocolRelative},
		{"//x ", protocolRelative},
		{`\\host\share`, shorthand},
		{` \\host\share`, shorthand},
		// The wording of this one is apm-go's (lower-case d); the check runs
		// before the whitespace is looked at, as in the Oracle.
		{"\t./dep", "dependency string contains invalid control characters"},
	}
	for _, tt := range rejected {
		t.Run(tt.value, func(t *testing.T) {
			_, err := ParseDepString(tt.value)
			if err == nil || err.Error() != tt.want {
				t.Errorf("error %v, want %s", err, tt.want)
			}
		})
	}
}
