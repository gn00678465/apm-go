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
// (models/dependency/reference.py:831-846), but for the rows marked as
// deviations.
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
		// Deviation: the Oracle strips the whitespace of a path in both
		// forms, apm-go in neither.
		{"'./dep '", "./dep "},
		// Deviations: the Oracle refuses these two. The string form takes
		// them, and install writes `path:` for what the string form takes.
		{"//x", "//x"},
		{`\\host\share`, `\\host\share`},
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
		// Deviation, as above: the Oracle reads this one as `./dep`.
		{"' ./dep '", `dependency entry 3 (path: " ./dep "): ` + notLocal},
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

// install writes `path:` for a string the string form reads as a local path,
// so the two forms have to agree on every value.
func TestParseDepDict_PathAgreesWithStringForm(t *testing.T) {
	tests := []struct {
		value string
		local bool
	}{
		{"./dep", true},
		{"/abs/dep", true},
		{"//x", true},
		{"~/x", true},
		{`.\dep`, true},
		{`\\host\share`, true},
		{"dep", false},
		{"sub/dep", false},
		{"owner/repo", false},
		{"./dep ", true},
		{" ./dep", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			str, strErr := ParseDepString(tt.value)
			obj, objErr := ParseDepDict(pathEntry(t, "'"+tt.value+"'"), 0)
			if !tt.local {
				if strErr == nil && str.IsLocal {
					t.Errorf("string form: local path %q, want not local", str.LocalPath)
				}
				if objErr == nil {
					t.Errorf("object form: accepted with LocalPath %q, want an error", obj.LocalPath)
				}
				return
			}
			if strErr != nil || !str.IsLocal || str.LocalPath != tt.value {
				t.Errorf("string form: %+v, error %v; want local path %q", str, strErr, tt.value)
			}
			if objErr != nil || !obj.IsLocal || obj.LocalPath != tt.value {
				t.Errorf("object form: %+v, error %v; want local path %q", obj, objErr, tt.value)
			}
		})
	}
}
