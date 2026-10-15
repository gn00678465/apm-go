package manifest

import "testing"

// Every row is what the Oracle (microsoft/apm v0.33.0, 18c4c43) gave for the
// same value through DependencyReference.parse; the `path:` object form hands
// the value to the same parse, so both forms get the same verdict and text.
func TestParseDepString_LocalPathNamesADirectory(t *testing.T) {
	named := func(p string) string {
		return "Local path '" + p + "' does not resolve to a named directory. " +
			"Use a path that ends with a directory name (e.g., './my-package' instead of './')."
	}

	accepted := []string{
		"../dep",
		"../../root/dep",
		"../secret",
		"../../../etc/passwd",
		"./dep/.",
		"./x/../dep",
		"~/",
		`..\dep`,
		`.\dep\.`,
		`~\`,
		"/a/b/",
	}
	for _, p := range accepted {
		t.Run(p, func(t *testing.T) {
			str, err := ParseDepString(p)
			if err != nil || !str.IsLocal || str.LocalPath != p {
				t.Errorf("string form: %+v, error %v; want local path %q", str, err, p)
			}
			obj, err := ParseDepDict(pathEntry(t, "'"+p+"'"), 0)
			if err != nil || !obj.IsLocal || obj.LocalPath != p {
				t.Errorf("object form: %+v, error %v; want local path %q", obj, err, p)
			}
		})
	}

	rejected := []struct{ value, path string }{
		{"./", "./"},
		{"./.", "./."},
		{"./dep/..", "./dep/.."},
		{"/", "/"},
		{"../", "../"},
		{" ./ ", "./"},
		{`.\`, `.\`},
		{`..\`, `..\`},
		{`.\dep\..`, `.\dep\..`},
	}
	for _, tt := range rejected {
		t.Run(tt.value, func(t *testing.T) {
			want := named(tt.path)
			if _, err := ParseDepString(tt.value); err == nil || err.Error() != want {
				t.Errorf("string form: error %v, want %s", err, want)
			}
			if _, err := ParseDepDict(pathEntry(t, "'"+tt.value+"'"), 0); err == nil || err.Error() != want {
				t.Errorf("object form: error %v, want %s", err, want)
			}
		})
	}
}

// `.` and `..` are not local paths (they lack the `./` prefix), so the
// named-directory check never sees them and they keep the refusals they had.
// The string form's text is apm-go's own; the Oracle says "Use 'user/repo'
// or 'github.com/user/repo' or 'dev.azure.com/org/project/repo' format".
func TestParseDepString_DotAndDotDotKeepTheirRefusal(t *testing.T) {
	const notLocal = "Object-style dependency must have a 'git' field, or 'path' must be a local filesystem path (starting with './', '../', '/', or '~')"
	tests := []struct{ value, str, obj string }{
		{".", `dependency "." does not match any valid form (url, shorthand, or local-path)`, `dependency entry 0 (path: "."): ` + notLocal},
		{"..", `dependency ".." does not match any valid form (url, shorthand, or local-path)`, `dependency entry 0 (path: ".."): ` + notLocal},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			if _, err := ParseDepString(tt.value); err == nil || err.Error() != tt.str {
				t.Errorf("string form: error %v, want %s", err, tt.str)
			}
			if _, err := ParseDepDict(pathEntry(t, "'"+tt.value+"'"), 0); err == nil || err.Error() != tt.obj {
				t.Errorf("object form: error %v, want %s", err, tt.obj)
			}
		})
	}
}
