package yamlcore

import "testing"

func TestIsVendorExtKey(t *testing.T) {
	tests := []struct {
		key  string
		want bool
	}{
		{"x-acme-top", true},
		{"x-acme-region", true},
		{"x-a", true},
		{"x-a0", true},
		{"x-a-b-c", true},
		{"x-acme-pin", true},

		{"", false},
		{"x", false},
		{"x-", false},
		{"x-1", false},        // third char must be [a-z]
		{"x-A", false},        // uppercase not allowed
		{"x-acme_top", false}, // underscore not allowed
		{"name", false},
		{"x-Acme", false}, // uppercase not allowed
		{"xx-foo", false},
		{"X-foo", false}, // uppercase X
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := IsVendorExtKey(tt.key); got != tt.want {
				t.Errorf("IsVendorExtKey(%q) = %v, want %v", tt.key, got, tt.want)
			}
		})
	}
}
