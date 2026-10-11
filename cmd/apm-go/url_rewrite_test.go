package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/apm-go/apm/internal/gitops"
	"github.com/apm-go/apm/internal/marketplace"
)

const (
	rewriteInspectHint  = `inspect matching rules with 'git config --show-origin --get-regexp ^url\..*\.insteadOf$'`
	rewriteProbeFailure = "Unable to verify Git URL rewrite safety (Git config probe failed); check Git configuration and retry; " + rewriteInspectHint
	// The first 60 characters of rewriteProbeFailure: what the Oracle's
	// str(exc)[:60] leaves in a `check` or `outdated` row.
	rewriteProbeFailureCut60 = "Unable to verify Git URL rewrite safety (Git config probe fa"
)

func gitConfigWithRule(replacement, prefix string) gitops.ConfigProbeFunc {
	return func(context.Context, gitops.ConfigProbe) ([]byte, int, error) {
		return []byte("global\x00url." + replacement + ".insteadOf\n" + prefix + "\x00"), 0, nil
	}
}

func lsRemoteCalls(git *fakeGit) int {
	n := 0
	for _, call := range git.calls {
		if call[0] == "ls-remote" {
			n++
		}
	}
	return n
}

// Oracle v0.33.0 doctor.py:191-192: the rewrite check's message is the
// network detail in full (every other failure branch is cut to 60 or 80),
// and ls-remote does not run.
func TestDoctor_Network_RewriteCheckFailure_FullDetail_NoLsRemote(t *testing.T) {
	tests := []struct {
		name       string
		probe      gitops.ConfigProbeFunc
		wantDetail string
	}{
		{"configuration unreadable", gitops.FailingGitConfigProbe, rewriteProbeFailure},
		{"unsafe rule", gitConfigWithRule("http://github.com/", "https://github.com/"),
			"HTTPS Git remote must not rewrite to insecure HTTP; " + rewriteInspectHint + " and remove the unsafe rule"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Cleanup(gitops.SetConfigProbeForTest(tc.probe))
			t.Setenv("GIT_HTTP_EXTRAHEADER", "")
			git := healthyGit()

			check := checkNetwork(doctorDeps{runGit: git.run, getenv: func(string) string { return "" }})

			if check.passed || check.detail != tc.wantDetail {
				t.Errorf("check = %+v\nwant failed with detail %q", check, tc.wantDetail)
			}
			if len(tc.wantDetail) <= 80 {
				t.Fatalf("fixture detail is %d characters; it must exceed the 80 of the truncated branches", len(tc.wantDetail))
			}
			if n := lsRemoteCalls(git); n != 0 {
				t.Errorf("ls-remote ran %d time(s) after a failed check", n)
			}
		})
	}
}

func TestDoctor_Network_RewriteCheckFailure_Exit1(t *testing.T) {
	chdirTemp(t)
	t.Cleanup(gitops.SetConfigProbeForTest(gitops.FailingGitConfigProbe))
	git := healthyGit()

	out, err := runDoctorWith(t, git, nil)

	if exitCodeOf(err) != 1 {
		t.Fatalf("want exit 1, got %v", err)
	}
	// The table wraps a long cell, so compare with the whitespace and the
	// borders taken out.
	flat := strings.Join(strings.FieldsFunc(out, func(r rune) bool { return r == '│' || r == ' ' || r == '\n' }), "")
	if want := strings.ReplaceAll(rewriteProbeFailure, " ", ""); !strings.Contains(flat, want) {
		t.Errorf("network row lacks the whole message:\n%s", out)
	}
	if n := lsRemoteCalls(git); n != 0 {
		t.Errorf("ls-remote ran %d time(s) after a failed check", n)
	}
}

func TestDoctor_Network_SafeRewrite_RunsLsRemote(t *testing.T) {
	t.Cleanup(gitops.SetConfigProbeForTest(gitConfigWithRule("ssh://git@github.com/", "https://github.com/")))
	git := healthyGit()

	check := checkNetwork(doctorDeps{runGit: git.run, getenv: func(string) string { return "" }})

	if !check.passed || check.detail != "github.com reachable" {
		t.Errorf("check = %+v", check)
	}
	if n := lsRemoteCalls(git); n != 1 {
		t.Errorf("ls-remote ran %d time(s), want 1", n)
	}
}

// recordGit puts a recording fake git first on PATH and returns the file it
// writes when it is run.
func recordGit(t *testing.T) string {
	t.Helper()
	record := filepath.Join(t.TempDir(), "git-invocations.log")
	t.Setenv("PATH", buildRecordingFakeGit(t)+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("GIT_RECORD_FILE", record)
	return record
}

func assertGitNotRun(t *testing.T, record string) {
	t.Helper()
	if invoked, err := os.ReadFile(record); err == nil {
		t.Errorf("git was run: %s", invoked)
	}
}

const remotePackageApmYML = "name: demo\nversion: 1.0.0\nmarketplace:\n" +
	"  owner:\n    name: acme\n" +
	"  packages:\n" +
	"    - name: tool\n      source: owner/repo\n"

// Oracle v0.33.0 commands/marketplace/check.py:244-252: any exception is the
// row's error, cut to 60 characters.
func TestMarketplaceCheck_RewriteCheckFailure_RowCutTo60(t *testing.T) {
	chdirTemp(t)
	record := recordGit(t)
	t.Cleanup(gitops.SetConfigProbeForTest(gitops.FailingGitConfigProbe))
	if err := os.WriteFile("apm.yml", []byte(remotePackageApmYML+"      ref: v1.0.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := runMarketplaceCmd(t, "check")

	if got := exitCodeOf(err); got != 1 {
		t.Fatalf("exit = %d, want 1 (output: %s)", got, out)
	}
	if len(rewriteProbeFailureCut60) != 60 || !strings.HasPrefix(rewriteProbeFailure, rewriteProbeFailureCut60) {
		t.Fatal("fixture is not the first 60 characters of the message")
	}
	if !strings.Contains(out, rewriteProbeFailureCut60) || strings.Contains(out, rewriteProbeFailureCut60+"i") {
		t.Errorf("output = %q, want the detail cut to exactly 60 characters", out)
	}
	if !strings.Contains(out, "1 entries have issues") {
		t.Errorf("output = %q, want the failure summary", out)
	}
	assertGitNotRun(t, record)
}

// Oracle v0.33.0 commands/marketplace/outdated.py:73-84: the same cut, in the
// note of a [x] row.
func TestMarketplaceOutdated_RewriteCheckFailure_NoteCutTo60(t *testing.T) {
	chdirTemp(t)
	record := recordGit(t)
	t.Cleanup(gitops.SetConfigProbeForTest(gitops.FailingGitConfigProbe))
	if err := os.WriteFile("apm.yml", []byte(remotePackageApmYML+"      version: \"^1.0.0\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, _ := runMarketplaceCmd(t, "outdated")

	if !strings.Contains(out, rewriteProbeFailureCut60) || strings.Contains(out, rewriteProbeFailureCut60+"i") {
		t.Errorf("output = %q, want the note cut to exactly 60 characters", out)
	}
	assertGitNotRun(t, record)
}

// The marketplace fetch sentence (Oracle v0.33.0 marketplace/client.py:
// 579-603, marketplace/errors.py:60) reaches the user through the command's
// existing error path.
func TestMarketplaceUpdate_GitSource_RewriteCheckFailure(t *testing.T) {
	isolatedMarketplaceRegistry(t)
	record := recordGit(t)
	t.Cleanup(gitops.SetConfigProbeForTest(gitConfigWithRule("http://git.example.com/", "https://git.example.com/")))
	t.Setenv("GIT_HTTP_EXTRAHEADER", "")
	src := marketplace.MarketplaceSource{Name: "acme", URL: "https://git.example.com/team/plugins.git", Ref: "main", Path: "marketplace.json"}
	if src.Kind() != marketplace.KindGit {
		t.Fatalf("fixture is %s, want a generic git source", src.Kind())
	}
	if err := marketplace.AddSource(src); err != nil {
		t.Fatal(err)
	}

	_, err := runMarketplaceCmd(t, "update", "acme")

	want := "Failed to fetch marketplace 'acme': HTTPS Git remote was rejected because Git configuration rewrites it to insecure HTTP; " +
		rewriteInspectHint + " and remove the unsafe rule. " +
		"Correct the matching Git configuration, then run 'apm-go marketplace update acme' to retry."
	if err == nil || !strings.HasSuffix(err.Error(), want) {
		t.Fatalf("error\n got %v\nwant suffix %s", err, want)
	}
	assertGitNotRun(t, record)
}
