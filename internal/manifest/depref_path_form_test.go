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

// Every row's verdict and reason is what the Oracle v0.33.0's
// DependencyReference.parse_from_dict gave for the same YAML
// (models/dependency/reference.py:831-846).
func TestParseDepDict_PathTakesOnlyLocalPaths(t *testing.T) {
	const (
		notLocal = "Object-style dependency must have a 'git' field, or 'path' must be a local filesystem path (starting with './', '../', '/', or '~')"
		empty    = "dependency entry 3: 'path' field must be a non-empty string"
	)

	accepted := []struct{ yaml, localPath string }{
		{"./dep", "./dep"},
		{"./sub/dep", "./sub/dep"},
		{"/abs/dep", "/abs/dep"},
		{"~/x", "~/x"},
		{"' ./dep '", "./dep"},
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
		{"''", empty},
		{"' '", empty},
		{"null", empty},
		{"5", empty},
		{"[a]", empty},
		// Deviation kept from before this rule: the Oracle takes `../dep`.
		{"../dep", `dependency path "../dep" escapes project root`},
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
