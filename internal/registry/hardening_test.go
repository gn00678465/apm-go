package registry

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// HIGH #2: a URL with embedded credentials must be refused before any request
// (Go would otherwise synthesize Basic auth from userinfo, bypassing the gate).
func TestClient_RefusesEmbeddedUserinfo(t *testing.T) {
	c, _ := NewClient("http://127.0.0.1:1", Credential{}, nil, true)
	_, _, err := c.FetchURL("http://user:pass@127.0.0.1:1/v1/packages/a/b/versions/1/download")
	if err == nil || !strings.Contains(err.Error(), "embedded credentials") {
		t.Fatalf("want embedded-credentials refusal, got %v", err)
	}
}

// MEDIUM #3: raw Basic user/pass echoed in an error body must be redacted, not
// just the base64 blob.
func TestClient_RedactsBasicRawSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "denied for alice / hunter2seekrit", 401)
	}))
	t.Cleanup(srv.Close)
	cred := Credential{Scheme: "basic", Value: "YWxpY2U6aHVudGVyMnNlZWtyaXQ=", redact: []string{"alice", "hunter2seekrit", "YWxpY2U6aHVudGVyMnNlZWtyaXQ="}}
	c, _ := NewClient(srv.URL, cred, nil, false)
	_, err := c.ListVersions("a", "b")
	if err == nil {
		t.Fatal("want error")
	}
	if strings.Contains(err.Error(), "hunter2seekrit") || strings.Contains(err.Error(), "alice") {
		t.Errorf("raw Basic secret leaked: %q", err.Error())
	}
}
