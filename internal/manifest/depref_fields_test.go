package manifest

import (
	"testing"

	"github.com/apm-go/apm/internal/yamlcore"
)

func parseEntry(t *testing.T, src string) (*DependencyReference, error) {
	t.Helper()
	node, err := yamlcore.SafeLoad([]byte(src))
	if err != nil {
		t.Fatalf("SafeLoad: %v", err)
	}
	return ParseDepDict(node.Content[0], 0)
}

// TestParseDepDict_RefusesWhatTheOracleRefuses holds the reasons Oracle 0.33.0
// prints for each entry (~/.cache/apm-go-verify/issue151/r3-or/<case>/i1.out,
// models/dependency/object_fields.py:26-72, reference.py:831-891), behind
// apm-go's own `dependency entry <N>: `.
func TestParseDepDict_RefusesWhatTheOracleRefuses(t *testing.T) {
	cases := []struct{ name, entry, want string }{
		{"ukpath", "path: ./dep\nfoo: 1\n", "Unsupported field(s) for path dependency: 'foo'"},
		{"ukgit", "git: https://127.0.0.1/o/pkg.git\nreff: v1.0.0\n", "Unsupported field(s) for git dependency: 'reff'"},
		{"ukver", "git: https://127.0.0.1/o/pkg.git\nversion: 1.0.0\n", "Git dependency field 'version' is unsupported; use 'ref' for a branch, tag, or commit"},
		{"uktwo", "git: https://127.0.0.1/o/pkg.git\nzz: 1\naa: 2\n", "Unsupported field(s) for git dependency: 'aa', 'zz'"},
		{"ukparent", "git: parent\npath: x\nskills: [a]\n", "Unsupported field(s) for parent git dependency: 'skills'"},
		{"ukparenttype", "git: parent\npath: x\ntype: github\n", "Unsupported field(s) for parent git dependency: 'type'"},
		{"parentnopath", "git: parent\nfoo: 1\n", "Unsupported field(s) for parent git dependency: 'foo'"},
		{"httpbad", "git: https://127.0.0.1/o/pkg.git\nallow_insecure: yes-please\n", "'allow_insecure' field must be a boolean"},
		{"httpnull", "git: https://127.0.0.1/o/pkg.git\nallow_insecure:\n", "'allow_insecure' field must be a boolean"},
		{"aliasbad", "git: https://127.0.0.1/o/skill.git\nalias: ../x\n", "Invalid alias: ../x. Aliases can only contain letters, numbers, dots, underscores, and hyphens"},
		{"aliasdot", "git: https://127.0.0.1/o/skill.git\nalias: ..\n", "Invalid dependency alias '..': '.' and '..' are reserved directory names. Set alias to a name such as 'my-skill.v2' in apm.yml and rerun apm-go install."},
		{"aliasblank", "path: ./dep\nalias: '  '\n", "'alias' field must be a non-empty string"},
		{"aliasint", "id: acme/sample\nversion: 1.0.0\nalias: 12\n", "'alias' field must be a non-empty string"},
		{"aliasparent", "git: parent\npath: x\nalias: a/b\n", "Invalid alias: a/b. Aliases can only contain letters, numbers, dots, underscores, and hyphens"},
		{"registry", "id: acme/sample\nversion: 1.0.0\nfoo: 1\n", "Object-form registry entry has unknown fields: ['foo']. Known fields: ['alias', 'id', 'path', 'registry', 'skills', 'targets', 'version']"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseEntry(t, tc.entry)
			if want := "dependency entry 0: " + tc.want; err == nil || err.Error() != want {
				t.Errorf("error %v\nwant      %s", err, want)
			}
		})
	}
}

// TestParseDepDict_TakesWhatTheOracleTakes is entries both install.
func TestParseDepDict_TakesWhatTheOracleTakes(t *testing.T) {
	cases := []struct {
		name, entry, alias string
		insecure           bool
	}{
		{"path alias", "path: ./dep\nalias: ' ldep '\n", "ldep", false},
		{"git type", "git: https://127.0.0.1/o/pkg.git\ntype: github\nref: main\n", "", false},
		{"git alias", "git: https://127.0.0.1/o/pkg.git\nalias: my.pkg_v2-x\n", "my.pkg_v2-x", false},
		{"git insecure", "git: http://127.0.0.1/o/pkg.git\nallow_insecure: true\n", "", true},
		{"git insecure no", "git: http://127.0.0.1/o/pkg.git\nallow_insecure: no\n", "", false},
		{"git alias null", "git: https://127.0.0.1/o/pkg.git\nalias:\n", "", false},
		{"registry alias", "id: acme/sample\nversion: 1.0.0\nalias: regal\n", "regal", false},
		{"parent alias", "git: parent\npath: x\nref: main\nalias: px\n", "px", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d, err := parseEntry(t, tc.entry)
			if err != nil {
				t.Fatal(err)
			}
			if d.Alias != tc.alias || d.AllowInsecure != tc.insecure {
				t.Errorf("alias %q insecure %v, want %q %v", d.Alias, d.AllowInsecure, tc.alias, tc.insecure)
			}
		})
	}
}
