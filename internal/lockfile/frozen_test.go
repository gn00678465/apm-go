package lockfile

import "testing"

func TestIsTruthyCI(t *testing.T) {
	tests := []struct {
		val  string
		want bool
	}{
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"1", true},
		{"yes", true},
		{"anything", true},
		{"", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"False", false},
	}
	for _, tt := range tests {
		t.Run(tt.val, func(t *testing.T) {
			if got := IsTruthyCI(tt.val); got != tt.want {
				t.Errorf("IsTruthyCI(%q) = %v, want %v", tt.val, got, tt.want)
			}
		})
	}
}
