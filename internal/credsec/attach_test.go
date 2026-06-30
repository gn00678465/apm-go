package credsec

import "testing"

func TestShouldAttachCredential(t *testing.T) {
	tests := []struct {
		name, url string
		insecure  bool
		want      bool
	}{
		{"https always", "https://github.com/o/r.git", false, true},
		{"http refused", "http://example.com/o/r.git", false, false},
		{"http insecure ok", "http://registry.local/o/r.git", true, true},
		{"http loopback v4", "http://127.0.0.1:8080/o/r.git", false, true},
		{"http loopback v6", "http://[::1]/o/r.git", false, true},
		{"http localhost", "http://localhost/o/r.git", false, true},
		{"http private not loopback", "http://192.168.1.5/o/r.git", false, false},
		{"ssh scheme refused", "ssh://git@github.com/o/r.git", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ShouldAttachCredential(tt.url, tt.insecure)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("ShouldAttachCredential(%q, insecure=%v)=%v want %v", tt.url, tt.insecure, got, tt.want)
			}
		})
	}
}
