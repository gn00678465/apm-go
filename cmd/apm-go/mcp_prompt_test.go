package main

import "testing"

func TestLooksSecret(t *testing.T) {
	for _, s := range []string{"token", "API_KEY", "Secret", "PASSWORD", "x-api", "GITHUB_TOKEN"} {
		if !looksSecret(s) {
			t.Errorf("%q should be treated as secret", s)
		}
	}
	for _, s := range []string{"Authorization", "X-Region", "Accept", "Content-Type"} {
		if looksSecret(s) {
			t.Errorf("%q should not be treated as secret", s)
		}
	}
}

func TestCollectHeaderValues(t *testing.T) {
	askedSecret := map[string]bool{}
	ask := func(label string, secret bool) string {
		askedSecret[label] = secret
		switch label {
		case "token":
			return "ghp_x"
		case "X-Custom":
			return "v"
		default: // X-Blank and anything else
			return ""
		}
	}

	got := collectHeaderValues([]string{"Authorization", "X-Custom", "X-Blank"}, ask)

	if got["Authorization"] != "Bearer ghp_x" {
		t.Errorf("Authorization = %q, want %q", got["Authorization"], "Bearer ghp_x")
	}
	if got["X-Custom"] != "v" {
		t.Errorf("X-Custom = %q, want v", got["X-Custom"])
	}
	if _, ok := got["X-Blank"]; ok {
		t.Errorf("blank input must skip the header, got %v", got)
	}
	if !askedSecret["token"] {
		t.Errorf("Authorization must be prompted as a secret token")
	}
}

func TestCollectHeaderValues_EmptyTokenSkipsAuthorization(t *testing.T) {
	ask := func(label string, secret bool) string { return "" }
	if got := collectHeaderValues([]string{"Authorization"}, ask); len(got) != 0 {
		t.Errorf("empty token must produce no Authorization header, got %v", got)
	}
}

func TestIsNonInteractiveEnv(t *testing.T) {
	for _, v := range []string{"APM_E2E_TESTS", "CI", "GITHUB_ACTIONS", "TRAVIS", "JENKINS_URL", "BUILDKITE"} {
		t.Setenv(v, "")
	}
	if isNonInteractiveEnv() {
		t.Fatalf("with all CI/E2E env cleared, isNonInteractiveEnv should be false")
	}
	t.Setenv("CI", "1")
	if !isNonInteractiveEnv() {
		t.Errorf("CI=1 should be detected as non-interactive")
	}
}
