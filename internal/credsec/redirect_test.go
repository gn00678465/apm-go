package credsec

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAuthDropRedirect_SameHostKeepsAuth drives a real http.Client through a 3xx
// to the same host class and asserts the credential survives (the drop path is
// covered by the callback test, since httptest servers all run on 127.0.0.1).
func TestAuthDropRedirect_SameHostKeepsAuth(t *testing.T) {
	var gotAuth string
	dest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer dest.Close()

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, dest.URL, http.StatusFound)
	}))
	defer redirector.Close()

	client := &http.Client{CheckRedirect: NewAuthDropRedirect(nil)}
	req, _ := http.NewRequest("GET", redirector.URL, nil)
	req.Header.Set("Authorization", "Bearer secrettoken")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth == "" {
		t.Errorf("same-host-class redirect should keep Authorization, got dropped")
	}
}

// TestAuthDropRedirect_Callback exercises the policy directly with distinct host
// classes (the httptest servers all run on 127.0.0.1, so unit-test the callback).
func TestAuthDropRedirect_Callback(t *testing.T) {
	policy := NewAuthDropRedirect(map[string][]string{
		"registry.example.com": {"mirror.example.org"},
	})

	mk := func(host string) *http.Request {
		r, _ := http.NewRequest("GET", "https://"+host+"/x", nil)
		r.Header.Set("Authorization", "Bearer secrettoken")
		r.Header.Set("Cookie", "s=1")
		return r
	}

	// cross host class: github.com -> evil.example.net
	via := []*http.Request{mk("github.com")}
	next := mk("evil.example.net")
	if err := policy(next, via); err != nil {
		t.Fatal(err)
	}
	if next.Header.Get("Authorization") != "" || next.Header.Get("Cookie") != "" {
		t.Errorf("cross-class redirect must drop credentials, got auth=%q cookie=%q",
			next.Header.Get("Authorization"), next.Header.Get("Cookie"))
	}

	// same eTLD+1: api.github.com -> codeload.github.com — keep
	via2 := []*http.Request{mk("api.github.com")}
	keep := mk("codeload.github.com")
	_ = policy(keep, via2)
	if keep.Header.Get("Authorization") == "" {
		t.Errorf("same-class redirect must keep Authorization")
	}

	// alias group: registry.example.com -> mirror.example.org — keep
	via3 := []*http.Request{mk("registry.example.com")}
	keepAlias := mk("mirror.example.org")
	_ = policy(keepAlias, via3)
	if keepAlias.Header.Get("Authorization") == "" {
		t.Errorf("aliased redirect must keep Authorization")
	}
}
