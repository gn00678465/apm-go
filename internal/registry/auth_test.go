package registry

import "testing"

func TestResolveCredential_Bearer(t *testing.T) {
	t.Setenv("APM_REGISTRY_TOKEN_CORP_MAIN", "tok123")
	c := ResolveCredential("corp-main")
	if c.Scheme != "bearer" || c.Value != "tok123" {
		t.Fatalf("got %+v", c)
	}
	if c.Header() != "Bearer tok123" {
		t.Errorf("Header = %q", c.Header())
	}
}

func TestResolveCredential_BasicAndBearerWins(t *testing.T) {
	t.Setenv("APM_REGISTRY_USER_CORP", "alice")
	t.Setenv("APM_REGISTRY_PASS_CORP", "secret")
	c := ResolveCredential("corp")
	if c.Scheme != "basic" || c.Header() != "Basic YWxpY2U6c2VjcmV0" { // base64("alice:secret")
		t.Fatalf("basic got %+v header %q", c, c.Header())
	}
	// bearer wins when both present
	t.Setenv("APM_REGISTRY_TOKEN_CORP", "tok")
	if got := ResolveCredential("corp"); got.Scheme != "bearer" {
		t.Errorf("bearer should win, got %+v", got)
	}
}

func TestResolveCredential_NameSanitizationAndNone(t *testing.T) {
	// name with '.' maps to '_'
	t.Setenv("APM_REGISTRY_TOKEN_CORP_MAIN", "x")
	if ResolveCredential("corp.main").Scheme != "bearer" {
		t.Errorf("corp.main should map to CORP_MAIN")
	}
	if got := ResolveCredential("unset-registry"); got.Header() != "" {
		t.Errorf("unset registry should be anonymous, got %+v", got)
	}
}
