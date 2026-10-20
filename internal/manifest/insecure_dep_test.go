package manifest

import "testing"

// TestCheckInsecureDependency holds the reasons Oracle 0.33.0 prints
// (~/.cache/apm-go-verify/issue151/r3-or/http, httpflag, httpai: i1.out),
// with apm-go for apm in the step. An http dependency needs both
// allow_insecure: true and --allow-insecure, whatever its host
// (install/insecure_policy.py:204-264).
func TestCheckInsecureDependency(t *testing.T) {
	both := "http://127.0.0.1/o/pkg#main -- HTTP dependency (unencrypted)\nTo install:\n  1. Set allow_insecure: true on the dep in apm.yml\n  2. Pass --allow-insecure to apm-go install"
	field := "http://127.0.0.1/o/pkg#main -- HTTP dependency (unencrypted)\nTo install:\n  1. Set allow_insecure: true on the dep in apm.yml"
	flag := "http://127.0.0.1/o/pkg#main -- HTTP dependency (unencrypted)\nTo install:\n  1. Pass --allow-insecure to apm-go install"
	cases := []struct {
		name       string
		allowField bool
		allowFlag  bool
		want       string
	}{
		{"neither", false, false, both},
		{"flag only", false, true, field},
		{"field only", true, false, flag},
		{"both", true, true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dep, err := ParseDepString("http://127.0.0.1/o/pkg.git#main")
			if err != nil {
				t.Fatal(err)
			}
			dep.AllowInsecure = tc.allowField
			err = CheckInsecureDependency(dep, tc.allowFlag, "github.com")
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != tc.want {
				t.Errorf("got %q\nwant %q", got, tc.want)
			}
		})
	}
	for _, raw := range []string{"https://127.0.0.1/o/pkg.git", "o/pkg", "./local"} {
		dep, err := ParseDepString(raw)
		if err != nil {
			t.Fatal(err)
		}
		if err := CheckInsecureDependency(dep, false, "github.com"); err != nil {
			t.Errorf("%s refused: %v", raw, err)
		}
	}
}

// TestInsecureDependencyURL is the git value to_apm_yml_entry writes for an
// http dependency (models/dependency/reference.py:1956-1961): host and port,
// the repository, no virtual path, then the ref.
func TestInsecureDependencyURL(t *testing.T) {
	cases := map[string]string{
		"http://127.0.0.1/o/pkg.git":      "http://127.0.0.1/o/pkg",
		"http://127.0.0.1:18115/o/x.git":  "http://127.0.0.1:18115/o/x",
		"http://example.com/o/pkg.git#v1": "http://example.com/o/pkg#v1",
	}
	for raw, want := range cases {
		dep, err := ParseDepString(raw)
		if err != nil {
			t.Fatal(err)
		}
		if got := InsecureDependencyURL(dep, "github.com"); got != want {
			t.Errorf("%s: got %s, want %s", raw, got, want)
		}
	}
}
