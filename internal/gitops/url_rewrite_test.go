package gitops

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/apm-go/apm/internal/manifest"
	"github.com/apm-go/apm/internal/yamlcore"
)

const (
	rewriteInspectHint  = `inspect matching rules with 'git config --show-origin --get-regexp ^url\..*\.insteadOf$'`
	rewriteRecoveryHint = rewriteInspectHint + " and remove the unsafe rule"
)

func probeFailureText(category string) string {
	return "Unable to verify Git URL rewrite safety (" + category + "); check Git configuration and retry; " + rewriteInspectHint
}

// configList renders key/value pairs the way `git config --null --show-scope
// --list` prints them: "<scope>\0<key>\n<value>\0".
func configList(keyValues ...string) []byte {
	var out []byte
	for i := 0; i < len(keyValues); i += 2 {
		out = append(out, "global\x00"+keyValues[i]+"\n"+keyValues[i+1]+"\x00"...)
	}
	return out
}

// fakeGitConfig answers the two probes of the check: the --list read and the
// --get-urlmatch read.
type fakeGitConfig struct {
	list     func(ctx context.Context) ([]byte, int, error)
	urlMatch func(ctx context.Context) ([]byte, int, error)
	probes   []ConfigProbe
	timeouts []time.Duration
}

func (f *fakeGitConfig) run(ctx context.Context, p ConfigProbe) ([]byte, int, error) {
	f.probes = append(f.probes, p)
	if deadline, ok := ctx.Deadline(); ok {
		f.timeouts = append(f.timeouts, time.Until(deadline).Round(time.Second))
	}
	if strings.Contains(strings.Join(p.Args, " "), "--get-urlmatch") {
		return f.urlMatch(ctx)
	}
	return f.list(ctx)
}

func useGitConfig(t *testing.T, f *fakeGitConfig) {
	t.Helper()
	t.Cleanup(SetConfigProbeForTest(f.run))
}

func useGitConfigList(t *testing.T, list []byte) *fakeGitConfig {
	t.Helper()
	f := &fakeGitConfig{
		list:     func(context.Context) ([]byte, int, error) { return list, 0, nil },
		urlMatch: func(context.Context) ([]byte, int, error) { return nil, 1, nil },
	}
	useGitConfig(t, f)
	return f
}

type oracleTable struct {
	Policy []struct {
		Remote           string `json:"remote"`
		Effective        string `json:"effective"`
		HasAuthorization bool   `json:"has_authorization"`
		Reason           string `json:"reason"`
		Message          string `json:"message"`
	} `json:"policy"`
	Split []struct {
		URL      string `json:"url"`
		Error    bool   `json:"error"`
		Scheme   string `json:"scheme"`
		Netloc   string `json:"netloc"`
		Hostname string `json:"hostname"`
		Port     *int   `json:"port"`
		Username bool   `json:"username"`
		Password bool   `json:"password"`
	} `json:"split"`
	Headers []struct {
		Value      string `json:"value"`
		Valid      bool   `json:"valid"`
		Credential bool   `json:"credential"`
	} `json:"headers"`
}

// loadOracleTable reads the verdicts the Oracle itself gave for each input
// (testdata/url_rewrite_oracle_gen.py).
func loadOracleTable(t *testing.T) oracleTable {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "url_rewrite_oracle.json"))
	if err != nil {
		t.Fatal(err)
	}
	var table oracleTable
	if err := json.Unmarshal(data, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Policy) == 0 || len(table.Split) == 0 || len(table.Headers) == 0 {
		t.Fatal("oracle table is empty")
	}
	return table
}

// Every policy verdict of the Oracle, reached through the public check: one
// insteadOf rule maps the remote to the effective URL, and the authorization
// input comes from GIT_HTTP_EXTRAHEADER.
func TestValidateURLRewriteSafety_MatchesOraclePolicy(t *testing.T) {
	for _, tc := range loadOracleTable(t).Policy {
		name := tc.Remote + " -> " + tc.Effective
		if tc.HasAuthorization {
			name += " (authorized)"
		}
		t.Run(name, func(t *testing.T) {
			useGitConfigList(t, configList("url."+tc.Effective+".insteadOf", tc.Remote))
			header := ""
			if tc.HasAuthorization {
				header = "Authorization: Bearer abc"
			}
			t.Setenv("GIT_HTTP_EXTRAHEADER", header)

			err := ValidateURLRewriteSafety(tc.Remote)

			if tc.Message == "" {
				if err != nil {
					t.Fatalf("want the rewrite allowed, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("want %q, got nil", tc.Message)
			}
			if err.Error() != tc.Message {
				t.Errorf("message\n got %q\nwant %q", err.Error(), tc.Message)
			}
			var rejected *URLRewriteError
			gotReason := ""
			if errors.As(err, &rejected) {
				gotReason = rejected.Reason
			}
			if gotReason != tc.Reason {
				t.Errorf("reason = %q, want %q", gotReason, tc.Reason)
			}
		})
	}
}

// One literal message per reason, so a table regenerated from a drifted
// Oracle cannot silently redefine the contract.
func TestValidateURLRewriteSafety_RejectionMessages(t *testing.T) {
	tests := []struct {
		name, replacement, prefix, remote, header string
		wantReason, wantMessage                   string
	}{
		{"remote helper", "ext::sh -c evil #", "https://github.com/", "https://github.com/o/r.git", "",
			"insecure-transport", "Git URL rewrite must not select remote-helper syntax"},
		{"credentials in replacement", "https://user:pw@github.com/", "https://github.com/", "https://github.com/o/r.git", "",
			"credentials", "Git URL rewrite replacement must not contain credentials"},
		{"https to http", "http://github.com/", "https://github.com/", "https://github.com/o/r.git", "",
			"https-downgrade", "HTTPS Git remote must not rewrite to insecure HTTP"},
		{"ssh to http", "http://github.com/", "git@github.com:", "git@github.com:o/r.git", "",
			"insecure-transport", "Git remote must not rewrite to insecure HTTP"},
		{"git protocol", "git://github.com/", "https://github.com/", "https://github.com/o/r.git", "",
			"insecure-transport", "HTTPS Git remote must not rewrite to an insecure transport"},
		{"credentials in remote", "https://github.com/x/", "https://token@github.com/o/", "https://token@github.com/o/r.git", "",
			"credential-origin", "Credential-bearing Git remote must not be rewritten"},
		{"authorized origin change", "https://github.com:8443/", "https://github.com/", "https://github.com/o/r.git", "Authorization: Bearer abc",
			"credential-origin", "Authenticated Git remote must not rewrite to a different HTTPS origin"},
		{"other host", "ssh://git@gitlab.com/", "https://github.com/", "https://github.com/o/r.git", "",
			"cross-host", "Git remote must not rewrite to a different network host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useGitConfigList(t, configList("url."+tc.replacement+".insteadOf", tc.prefix))
			t.Setenv("GIT_HTTP_EXTRAHEADER", tc.header)

			err := ValidateURLRewriteSafety(tc.remote)

			var rejected *URLRewriteError
			if !errors.As(err, &rejected) {
				t.Fatalf("want *URLRewriteError, got %v", err)
			}
			if rejected.Reason != tc.wantReason {
				t.Errorf("reason = %q, want %q", rejected.Reason, tc.wantReason)
			}
			if want := tc.wantMessage + "; " + rewriteRecoveryHint; err.Error() != want {
				t.Errorf("message\n got %q\nwant %q", err.Error(), want)
			}
		})
	}
}

func TestValidateURLRewriteSafety_AllowedRewrites(t *testing.T) {
	tests := []struct{ name, replacement, prefix, remote string }{
		{"https to ssh on the same host", "ssh://git@github.com/", "https://github.com/", "https://github.com/o/r.git"},
		{"https to scp syntax on the same host", "git@github.com:", "https://github.com/", "https://github.com/o/r.git"},
		{"file URL", "file:///srv/mirror/", "https://github.com/", "https://github.com/o/r.git"},
		{"local path", "/srv/mirror/", "https://github.com/", "https://github.com/o/r.git"},
		{"rule for another URL", "http://evil.example/", "https://gitlab.com/", "https://github.com/o/r.git"},
		{"pushInsteadOf is not a fetch rewrite", "http://evil.example/", "", "https://github.com/o/r.git"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			list := configList("url."+tc.replacement+".insteadOf", tc.prefix)
			if tc.prefix == "" {
				list = configList("url."+tc.replacement+".pushInsteadOf", "https://github.com/")
			}
			useGitConfigList(t, list)
			t.Setenv("GIT_HTTP_EXTRAHEADER", "")

			if err := ValidateURLRewriteSafety(tc.remote); err != nil {
				t.Fatalf("want the URL allowed, got %v", err)
			}
		})
	}
}

func TestValidateURLRewriteSafety_LongestPrefixWins(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	unsafe, safe := "http://github.com/", "ssh://git@github.com/"

	t.Run("longer safe rule beats a shorter unsafe one", func(t *testing.T) {
		useGitConfigList(t, configList(
			"url."+unsafe+".insteadOf", "https://github.com/",
			"url."+safe+"o/.insteadOf", "https://github.com/o/",
		))
		if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
			t.Fatalf("want allowed, got %v", err)
		}
	})
	t.Run("longer unsafe rule beats a shorter safe one", func(t *testing.T) {
		useGitConfigList(t, configList(
			"url."+safe+".insteadOf", "https://github.com/",
			"url."+unsafe+"o/.insteadOf", "https://github.com/o/",
		))
		var rejected *URLRewriteError
		if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); !errors.As(err, &rejected) || rejected.Reason != "https-downgrade" {
			t.Fatalf("want https-downgrade, got %v", err)
		}
	})
	t.Run("first of two equally long prefixes wins", func(t *testing.T) {
		useGitConfigList(t, configList(
			"url."+safe+".insteadOf", "https://github.com/",
			"url."+unsafe+".insteadOf", "https://github.com/",
		))
		if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
			t.Fatalf("want allowed, got %v", err)
		}
	})
}

func TestValidateURLRewriteSafety_ConfigParsing(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	downgrade := configList("url.http://github.com/.insteadOf", "https://github.com/")
	tests := []struct {
		name         string
		list         []byte
		wantCategory string // "" = allowed
		wantReason   string
	}{
		{name: "empty output has no rules", list: nil},
		{name: "unrelated keys only", list: configList("user.name", "A", "core.editor", "vi")},
		{name: "key case is ignored", list: configList("URL.http://github.com/.INSTEADOF", "https://github.com/"), wantReason: "https-downgrade"},
		{name: "value may hold a newline", list: configList("core.x", "a\nb", "url.http://github.com/.insteadOf", "https://github.com/"), wantReason: "https-downgrade"},
		{name: "odd field count", list: append(append([]byte{}, downgrade...), "global\x00"...), wantCategory: "Git config returned malformed output"},
		{name: "payload without newline", list: []byte("global\x00url.http://github.com/.insteadOf\x00"), wantCategory: "Git config returned malformed output"},
		{name: "invalid UTF-8", list: []byte("global\x00core.x\n\xff\xfe\x00"), wantCategory: "Git config returned non-UTF-8 output"},
		{name: "rewrite without a prefix", list: []byte("global\x00url.http://github.com/.insteadOf\n\x00"), wantCategory: "Git config returned an incomplete rewrite"},
		{name: "rewrite without a replacement", list: configList("url..insteadOf", "https://github.com/"), wantCategory: "Git config returned an incomplete rewrite"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			useGitConfigList(t, tc.list)

			err := ValidateURLRewriteSafety("https://github.com/o/r.git")

			switch {
			case tc.wantCategory != "":
				if err == nil || err.Error() != probeFailureText(tc.wantCategory) {
					t.Fatalf("got %v\nwant %s", err, probeFailureText(tc.wantCategory))
				}
			case tc.wantReason != "":
				var rejected *URLRewriteError
				if !errors.As(err, &rejected) || rejected.Reason != tc.wantReason {
					t.Fatalf("want reason %s, got %v", tc.wantReason, err)
				}
			case err != nil:
				t.Fatalf("want allowed, got %v", err)
			}
		})
	}
}

// The key is matched case-insensitively, but the replacement keeps the case
// git printed: git's URL matching of a path is case-sensitive, so the URL
// handed to the URL-match probe must not be the lower-cased key.
func TestValidateURLRewriteSafety_ReplacementKeepsItsCase(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	f := useGitConfigList(t, configList(
		"URL.https://github.com/Org/Mirror/.INSTEADOF", "https://github.com/o/",
		"http.extraheader", "X-Trace: 1"))

	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatal(err)
	}
	if len(f.probes) != 2 {
		t.Fatalf("want the URL-match probe to run, got %d probes", len(f.probes))
	}
	args := f.probes[1].Args
	if got, want := args[len(args)-1], "https://github.com/Org/Mirror/r.git"; got != want {
		t.Errorf("effective URL = %q, want %q", got, want)
	}
}

func TestValidateURLRewriteSafety_ListProbe(t *testing.T) {
	f := useGitConfigList(t, nil)

	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatal(err)
	}

	if len(f.probes) != 1 {
		t.Fatalf("want 1 probe when no rule matches, got %d", len(f.probes))
	}
	if got, want := strings.Join(f.probes[0].Args, " "), "config --null --show-scope --list"; got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
	for _, want := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=", "GIT_ALLOW_PROTOCOL=https:ssh:git"} {
		if !contains(f.probes[0].Env, want) {
			t.Errorf("probe env lacks %s", want)
		}
	}
}

func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func TestValidateURLRewriteSafety_ListProbeFailures(t *testing.T) {
	tests := []struct {
		name         string
		code         int
		err          error
		wantCategory string
		wantProbes   int
	}{
		{"exit status", 1, nil, "Git config probe failed", 1},
		{"killed by a signal", -1, nil, "Git config probe failed", 1},
		{"cannot start", 0, errors.New("fork/exec git: permission denied"), "Git config probe could not start", 1},
		{"times out twice", 0, context.DeadlineExceeded, "Git config probe timed out", 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGitConfig{list: func(context.Context) ([]byte, int, error) { return nil, tc.code, tc.err }}
			useGitConfig(t, f)

			err := ValidateURLRewriteSafety("https://github.com/o/r.git")

			var probeErr *URLRewriteProbeError
			if !errors.As(err, &probeErr) || probeErr.Category != tc.wantCategory {
				t.Fatalf("want category %q, got %v", tc.wantCategory, err)
			}
			if err.Error() != probeFailureText(tc.wantCategory) {
				t.Errorf("message = %q", err.Error())
			}
			if len(f.probes) != tc.wantProbes {
				t.Errorf("probes = %d, want %d", len(f.probes), tc.wantProbes)
			}
		})
	}
}

func TestValidateURLRewriteSafety_RetriesOneTimeoutWithALongerDeadline(t *testing.T) {
	attempts := 0
	f := &fakeGitConfig{list: func(context.Context) ([]byte, int, error) {
		attempts++
		if attempts == 1 {
			return nil, 0, context.DeadlineExceeded
		}
		return nil, 0, nil
	}}
	useGitConfig(t, f)

	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatalf("want the retry to pass, got %v", err)
	}
	if len(f.timeouts) != 2 || f.timeouts[0] != 10*time.Second || f.timeouts[1] != 30*time.Second {
		t.Errorf("deadlines = %v, want [10s 30s]", f.timeouts)
	}
}

// A missing git is each call site's own report, never "unable to verify".
func TestValidateURLRewriteSafety_MissingGitPasses(t *testing.T) {
	f := &fakeGitConfig{list: func(context.Context) ([]byte, int, error) {
		return nil, 0, &exec.Error{Name: "git", Err: exec.ErrNotFound}
	}}
	useGitConfig(t, f)

	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatalf("want nil, got %v", err)
	}
}

// originChange is allowed without authorization and rejected with it, so the
// verdict shows which answer the authorization probe gave.
var originChange = []string{"url.https://github.com:8443/.insteadOf", "https://github.com/"}

func TestValidateURLRewriteSafety_AuthorizationFromEnvironment(t *testing.T) {
	tests := []struct {
		header         string
		wantAuthorized bool
	}{
		{"Authorization: Bearer abc", true},
		{"X-Api-Key: abc", true},
		{"X-Trace: abc", false},
		{"not a header line", false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.header, func(t *testing.T) {
			useGitConfigList(t, configList(originChange...))
			t.Setenv("GIT_HTTP_EXTRAHEADER", tc.header)

			err := ValidateURLRewriteSafety("https://github.com/o/r.git")

			if authorized := err != nil; authorized != tc.wantAuthorized {
				t.Fatalf("authorized = %v, want %v (err %v)", authorized, tc.wantAuthorized, err)
			}
		})
	}
}

func TestValidateURLRewriteSafety_AuthorizationFromConfig(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	const scoped = "http.https://github.com:8443/.extraHeader"
	tests := []struct {
		name           string
		headers        []string // key, value pairs
		selected       string   // urlmatch stdout; "" = exit 1, no key applies
		wantAuthorized bool
	}{
		{"selected key carries a credential", []string{"http.extraheader", "X-Trace: 1", scoped, "Authorization: Basic Zm9v"}, urlMatchProbeHeader + "1\x00", true},
		{"selected key carries none", []string{"http.extraheader", "X-Trace: 1", scoped, "Authorization: Basic Zm9v"}, urlMatchProbeHeader + "0\x00", false},
		{"no key applies", []string{scoped, "Authorization: Basic Zm9v"}, "", false},
		{"every value of the selected key counts", []string{scoped, "X-Trace: 1", "HTTP.https://github.com:8443/.EXTRAHEADER", "Authorization: Basic Zm9v"}, urlMatchProbeHeader + "0\x00", true},
		{"an empty value resets the earlier ones", []string{scoped, "Authorization: Basic Zm9v", scoped, "", scoped, "X-Trace: 1"}, urlMatchProbeHeader + "2\n", false},
		{"a credential after the reset counts", []string{scoped, "X-Trace: 1", scoped, "", scoped, "Authorization: Basic Zm9v"}, urlMatchProbeHeader + "0\x00", true},
		{"a value that is no header line is not collected", []string{scoped, "Authorization Basic"}, urlMatchProbeHeader + "0\x00", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGitConfig{
				list: func(context.Context) ([]byte, int, error) {
					return configList(append(append([]string{}, originChange...), tc.headers...)...), 0, nil
				},
				urlMatch: func(context.Context) ([]byte, int, error) {
					if tc.selected == "" {
						return nil, 1, nil
					}
					return []byte(tc.selected), 0, nil
				},
			}
			useGitConfig(t, f)

			err := ValidateURLRewriteSafety("https://github.com/o/r.git")

			if authorized := err != nil; authorized != tc.wantAuthorized {
				t.Fatalf("authorized = %v, want %v (err %v)", authorized, tc.wantAuthorized, err)
			}
			if tc.wantAuthorized {
				want := "Authenticated Git remote must not rewrite to a different HTTPS origin; " + rewriteRecoveryHint
				if err.Error() != want {
					t.Errorf("message = %q", err.Error())
				}
			}
		})
	}
}

// The URL-match probe replays only the collected header keys, each with its
// index as the value, against the effective URL, with every other
// configuration source shut off.
func TestValidateURLRewriteSafety_URLMatchProbe(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	t.Setenv("GIT_CONFIG_COUNT", "1")
	t.Setenv("GIT_CONFIG_KEY_0", "http.extraheader")
	t.Setenv("GIT_CONFIG_VALUE_0", "Authorization: Bearer ambient")
	t.Setenv("GIT_CONFIG_PARAMETERS", "'http.extraheader=Authorization: Bearer ambient'")
	f := useGitConfigList(t, configList(append(append([]string{}, originChange...),
		"http.extraheader", "X-Trace: 1",
		"http.https://github.com:8443/.extraHeader", "Authorization: Basic Zm9v")...))

	_ = ValidateURLRewriteSafety("https://github.com/o/r.git")

	if len(f.probes) != 2 {
		t.Fatalf("want 2 probes, got %d", len(f.probes))
	}
	probe := f.probes[1]
	if got, want := strings.Join(probe.Args, " "), "config --null --get-urlmatch http.extraHeader https://github.com:8443/o/r.git"; got != want {
		t.Errorf("args = %q, want %q", got, want)
	}
	if !probe.RepoNeutral {
		t.Error("the URL-match probe must not see a repository's local configuration")
	}
	// The tail of the environment is what git reads: a later entry wins.
	wantTail := []string{
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_SYSTEM=" + os.DevNull,
		"GIT_CONFIG_GLOBAL=" + os.DevNull,
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=http.extraheader",
		"GIT_CONFIG_VALUE_0=X-Apm-Config-Probe: 0",
		"GIT_CONFIG_KEY_1=http.https://github.com:8443/.extraHeader",
		"GIT_CONFIG_VALUE_1=X-Apm-Config-Probe: 1",
	}
	if len(probe.Env) < len(wantTail) {
		t.Fatalf("probe env too short: %q", probe.Env)
	}
	if tail := probe.Env[len(probe.Env)-len(wantTail):]; strings.Join(tail, "\n") != strings.Join(wantTail, "\n") {
		t.Errorf("env tail\n got %q\nwant %q", tail, wantTail)
	}
	for _, kv := range probe.Env[:len(probe.Env)-len(wantTail)] {
		name, _, _ := strings.Cut(kv, "=")
		if name == "GIT_CONFIG_PARAMETERS" || name == "GIT_CONFIG_COUNT" ||
			strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_") {
			t.Errorf("ambient %s reached the probe", kv)
		}
	}
	if !contains(probe.Env, "GIT_TERMINAL_PROMPT=0") {
		t.Error("the URL-match probe must run under the secure git environment")
	}
}

func TestValidateURLRewriteSafety_URLMatchProbeFailures(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	tests := []struct {
		name         string
		stdout       string
		code         int
		err          error
		wantCategory string
	}{
		{"times out twice", "", 0, context.DeadlineExceeded, "Git URL-match probe timed out"},
		{"cannot start", "", 0, errors.New("fork/exec git: permission denied"), "Git URL-match probe could not start"},
		{"exit status", "", 128, nil, "Git URL-match probe exited with status 128"},
		{"another header", "Authorization: Basic Zm9v\x00", 0, nil, "Git URL-match probe returned malformed output"},
		{"no index", urlMatchProbeHeader + "x\x00", 0, nil, "Git URL-match probe returned malformed output"},
		{"index past the end", urlMatchProbeHeader + "1\x00", 0, nil, "Git URL-match probe returned malformed output"},
		{"negative index", urlMatchProbeHeader + "-1\x00", 0, nil, "Git URL-match probe returned malformed output"},
		{"empty output", "", 0, nil, "Git URL-match probe returned malformed output"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeGitConfig{
				list: func(context.Context) ([]byte, int, error) {
					return configList(append(append([]string{}, originChange...), "http.extraheader", "Authorization: Basic Zm9v")...), 0, nil
				},
				urlMatch: func(context.Context) ([]byte, int, error) { return []byte(tc.stdout), tc.code, tc.err },
			}
			useGitConfig(t, f)

			err := ValidateURLRewriteSafety("https://github.com/o/r.git")

			if err == nil || err.Error() != probeFailureText(tc.wantCategory) {
				t.Fatalf("got %v\nwant %s", err, probeFailureText(tc.wantCategory))
			}
		})
	}
}

// A non-HTTP effective URL never sends an HTTP header, so git is not asked.
func TestValidateURLRewriteSafety_NoURLMatchProbeForSSH(t *testing.T) {
	t.Setenv("GIT_HTTP_EXTRAHEADER", "Authorization: Bearer abc")
	f := useGitConfigList(t, configList(
		"url.ssh://git@github.com/.insteadOf", "https://github.com/",
		"http.extraheader", "Authorization: Basic Zm9v"))

	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatalf("want allowed, got %v", err)
	}
	if len(f.probes) != 1 {
		t.Errorf("probes = %d, want only the list probe", len(f.probes))
	}
}

func TestPyURLSplit_MatchesOracle(t *testing.T) {
	for _, tc := range loadOracleTable(t).Split {
		t.Run(tc.URL, func(t *testing.T) {
			parsed, err := pyURLSplit(tc.URL)
			var origin pyOrigin
			if err == nil {
				origin, err = parsed.origin()
			}
			if tc.Error {
				if err == nil {
					t.Fatalf("want an error, got %+v", parsed)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantPort := 0
			if tc.Port != nil {
				wantPort = *tc.Port
				if wantPort == 0 {
					wantPort = -1
				}
			}
			hasUsername, hasPassword := parsed.userinfo()
			got := []any{parsed.scheme, parsed.netloc, parsed.hostname(), origin.port, hasUsername, hasPassword}
			want := []any{tc.Scheme, tc.Netloc, tc.Hostname, wantPort, tc.Username, tc.Password}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("field %d = %v, want %v", i, got[i], want[i])
				}
			}
		})
	}
}

func TestHTTPExtraHeader_MatchesOracle(t *testing.T) {
	for _, tc := range loadOracleTable(t).Headers {
		t.Run(tc.Value, func(t *testing.T) {
			if got := isValidHTTPExtraHeaderValue(tc.Value); got != tc.Valid {
				t.Errorf("valid = %v, want %v", got, tc.Valid)
			}
			if got := isCredentialBearingHTTPHeader(tc.Value); got != tc.Credential {
				t.Errorf("credential-bearing = %v, want %v", got, tc.Credential)
			}
		})
	}
}

// The production probe against a real git: the --list format, the URL-match
// replay, and that only the configuration files named here are read.
func TestValidateURLRewriteSafety_RealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
	t.Cleanup(SetConfigProbeForTest(execConfigProbe))
	global := filepath.Join(t.TempDir(), "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	t.Chdir(t.TempDir())

	write := func(content string) {
		t.Helper()
		if err := os.WriteFile(global, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("")
	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatalf("empty config: %v", err)
	}

	write("[url \"http://github.com/\"]\n\tinsteadOf = https://github.com/\n")
	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err == nil ||
		err.Error() != "HTTPS Git remote must not rewrite to insecure HTTP; "+rewriteRecoveryHint {
		t.Fatalf("downgrade rule: %v", err)
	}
	if err := ValidateURLRewriteSafety("https://gitlab.com/o/r.git"); err != nil {
		t.Fatalf("rule for another host: %v", err)
	}

	origin := "[url \"https://github.com:8443/\"]\n\tinsteadOf = https://github.com/\n"
	write(origin + "[http \"https://github.com:8443/\"]\n\textraHeader = Authorization: Basic Zm9v\n")
	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err == nil ||
		err.Error() != "Authenticated Git remote must not rewrite to a different HTTPS origin; "+rewriteRecoveryHint {
		t.Fatalf("header scoped to the effective URL: %v", err)
	}

	write(origin + "[http \"https://elsewhere.example/\"]\n\textraHeader = Authorization: Basic Zm9v\n")
	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err != nil {
		t.Fatalf("header scoped to another URL: %v", err)
	}

	write("[url \"http://github.com/\"\n")
	if err := ValidateURLRewriteSafety("https://github.com/o/r.git"); err == nil ||
		err.Error() != probeFailureText("Git config probe failed") {
		t.Fatalf("unreadable config: %v", err)
	}
}

// buildFakeGit compiles testdata/fakegit as "git" into a fresh directory.
func buildFakeGit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	name := "git"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, name), filepath.Join("testdata", "fakegit", "main.go"))
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build fakegit: %v\n%s", err, output)
	}
	return dir
}

// Each network git call site stops at a failed check: its error is the
// check's own, and git is never run.
func TestNetworkGit_NotRunWhenRewriteCheckFails(t *testing.T) {
	record := filepath.Join(t.TempDir(), "git-invocations")
	t.Setenv("PATH", buildFakeGit(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_RECORD_FILE", record)
	t.Cleanup(SetConfigProbeForTest(FailingGitConfigProbe))
	const remote = "https://example.invalid/owner/repo.git"
	loader := &RealPackageLoader{}

	calls := map[string]func() error{
		"ls-remote": func() error {
			_, err := (&RealTagLister{}).ListTags(remote)
			return err
		},
		"clone at a tag": func() error {
			return loader.cloneRepo(remote, filepath.Join(t.TempDir(), "dest"), "v1.0.0")
		},
		"clone at a commit": func() error {
			return loader.cloneRepo(remote, filepath.Join(t.TempDir(), "dest"), strings.Repeat("a", 40))
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()

			if err == nil || err.Error() != probeFailureText("Git config probe failed") {
				t.Fatalf("got %v\nwant %s", err, probeFailureText("Git config probe failed"))
			}
			if invoked, readErr := os.ReadFile(record); readErr == nil {
				t.Fatalf("git was run: %s", invoked)
			}
		})
	}
}

// The check judges the URL git would connect to, port included: a rule for
// h.io:8443 stops the clone and the tag listing of a dependency on that port.
func TestNetworkGit_RewriteCheckSeesDependencyPort(t *testing.T) {
	record := filepath.Join(t.TempDir(), "git-invocations")
	t.Setenv("PATH", buildFakeGit(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("FAKEGIT_RECORD_FILE", record)
	useGitConfigList(t, configList("url.https://evil.example:9999/.insteadof", "https://h.io:8443/"))
	ref, err := manifest.ParseDepString("https://h.io:8443/o/x.git")
	if err != nil {
		t.Fatal(err)
	}

	calls := map[string]func() error{
		"clone": func() error {
			_, err := (&RealPackageLoader{ModulesDir: t.TempDir()}).LoadPackage(ref, "")
			return err
		},
		"clone at a commit": func() error {
			_, err := (&RealPackageLoader{ModulesDir: t.TempDir()}).LoadPackage(ref, strings.Repeat("a", 40))
			return err
		},
		"subdirectory package": func() error {
			sub := parseOneDependency(t, "dependencies:\n  apm:\n    - git: https://h.io:8443/o/x.git\n      path: skills/foo\n")
			if sub.VirtualType != "subdirectory" {
				t.Fatalf("not a subdirectory package: %+v", sub)
			}
			_, err := (&RealPackageLoader{ModulesDir: t.TempDir()}).LoadPackage(sub, "")
			return err
		},
		"ls-remote": func() error {
			_, err := (&RealTagLister{}).ListTagsForRef(ref)
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			var rejected *URLRewriteError
			if err := call(); !errors.As(err, &rejected) {
				t.Fatalf("want *URLRewriteError, got %v", err)
			}
			if invoked, readErr := os.ReadFile(record); readErr == nil {
				t.Fatalf("git was run: %s", invoked)
			}
		})
	}
}

func parseOneDependency(t *testing.T, apmYML string) *manifest.DependencyReference {
	t.Helper()
	doc, err := yamlcore.SafeLoad([]byte("name: p\nversion: 1.0.0\n" + apmYML))
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := manifest.ParseManifest(doc)
	if err != nil || len(m.ParsedDeps) != 1 {
		t.Fatalf("ParseManifest: %v, %d dependencies", err, len(m.ParsedDeps))
	}
	return m.ParsedDeps[0]
}
