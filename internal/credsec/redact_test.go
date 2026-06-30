package credsec

import (
	"strings"
	"testing"
)

func TestRedactor(t *testing.T) {
	r := NewRedactor("ghp_supersecrettoken", "", "hunter2")
	in := "cloning with token ghp_supersecrettoken and pass hunter2"
	out := r.Redact(in)
	if strings.Contains(out, "ghp_supersecrettoken") || strings.Contains(out, "hunter2") {
		t.Errorf("credential leaked after redaction: %q", out)
	}
	if !strings.Contains(out, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker, got %q", out)
	}
}

func TestMatchesSecretPattern(t *testing.T) {
	match := []string{".env", ".env.local", "deploy/.env.production", "key.pem", "server.key", "id_rsa", "id_ed25519", "/home/u/.ssh/id_rsa"}
	for _, p := range match {
		if !MatchesSecretPattern(p) {
			t.Errorf("expected %q to match secret pattern", p)
		}
	}
	nomatch := []string{"README.md", "main.go", "env.go", "keyboard.txt", "apm.yml", "pemphigus.txt"}
	for _, p := range nomatch {
		if MatchesSecretPattern(p) {
			t.Errorf("expected %q NOT to match secret pattern", p)
		}
	}
}
